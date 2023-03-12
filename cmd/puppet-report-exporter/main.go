package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/bonsai-oss/jsonstatus"
	"github.com/bonsai-oss/workering/v2"
	"github.com/getsentry/sentry-go"
	"github.com/gorilla/mux"
	"github.com/jellydator/ttlcache/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"

	"github.com/bonsai-oss/puppet-report-exporter/internal/metrics"
	"github.com/bonsai-oss/puppet-report-exporter/internal/middleware"
	"github.com/bonsai-oss/puppet-report-exporter/pkg/puppet"
)

type application struct {
	settings settings

	nodeCache     []puppet.Node
	nodeCacheLock sync.Mutex

	reportLogCache *ttlcache.Cache[string, []puppet.ReportLogEntry]

	metricServer *http.Server
	puppetDb     *puppet.ApiClient
}
type settings struct {
	webListenAddress     string
	puppetdbApiAddress   string
	puppetdbInitialFetch bool
	mode                 string
	reportListenAddress  string
}
type worker func(ctx context.Context, done chan any)

// metricsListener listens for metrics requests and serves them
func (app *application) metricsListener(ctx context.Context, done chan<- any) {
	metricsRouter := mux.NewRouter()
	metricsRouter.Path("/").Methods(http.MethodGet).Handler(http.RedirectHandler("/metrics", http.StatusTemporaryRedirect))
	metricsRouter.Path("/metrics").Methods(http.MethodGet).Handler(promhttp.Handler())

	app.metricServer = &http.Server{Addr: applicationInstance.settings.webListenAddress, Handler: metricsRouter}

	log.Println("Starting metric endpoint on", applicationInstance.settings.webListenAddress)
	if err := app.metricServer.ListenAndServe(); err != nil {
		log.Println(err)
		done <- nil
	}
}

// reportListenerBuilder creates a worker listening for reports
func (app *application) reportListenerBuilder(messageChan chan puppet.ReportData) workering.WorkerFunction {
	return func(ctx context.Context, done chan<- any) {
		reportRouter := mux.NewRouter()
		reportRouter.Use(middleware.Prometheus)
		reportRouter.Path("/").Methods(http.MethodPost).HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			var report puppet.ReportData
			decodeError := yaml.NewDecoder(request.Body).Decode(&report)
			if decodeError != nil {
				jsonstatus.Status{Code: http.StatusNotAcceptable, Message: fmt.Sprintf("Invalid report format %+q", decodeError)}.Encode(response)
				sentry.CaptureException(decodeError)
				return
			}
			messageChan <- report
			jsonstatus.Status{Code: http.StatusAccepted, Message: "Report accepted"}.Encode(response)
			return
		})

		reportServer := &http.Server{Addr: applicationInstance.settings.reportListenAddress, Handler: reportRouter}

		log.Println("Starting report endpoint on", applicationInstance.settings.reportListenAddress)
		go func() {
			if err := reportServer.ListenAndServe(); err != nil {
				log.Println(err)
			}
		}()
		<-ctx.Done()
		reportServer.Shutdown(context.Background())
		done <- nil
	}
}

// puppetdbReportLogCacheManager creates a worker that processes cache metrics
func (app *application) puppetdbReportLogCacheManager(ctx context.Context, done chan<- any) {
	metricUpdateTicker := time.NewTicker(2 * time.Second)
	for {
		select {
		case <-ctx.Done():
			metricUpdateTicker.Stop()
			done <- nil
			return
		case <-metricUpdateTicker.C:
			metrics.PuppetDBReportCacheEntries.Set(float64(app.reportLogCache.Len()))
		}
	}
}

// puppetdbNodesCrawlerBuilder creates a worker that fetches puppet.Node from PuppetDB
func (app *application) puppetdbNodesCrawlerBuilder(refreshNotify chan any) workering.WorkerFunction {
	return func(ctx context.Context, done chan<- any) {
		nodeUpdateTicker := time.NewTicker(10 * time.Second)
		for {
			select {
			case <-ctx.Done():
				nodeUpdateTicker.Stop()
				done <- nil
				return
			case <-nodeUpdateTicker.C:
				nodes, getNodesError := app.puppetDb.GetNodes()
				if getNodesError != nil {
					sentry.CaptureException(getNodesError)
					log.Println(getNodesError)
					continue
				}
				app.nodeCacheLock.Lock()
				app.nodeCache = nodes
				metrics.NodeCount.Reset()
				for _, node := range nodes {
					metrics.NodeCount.With(prometheus.Labels{metrics.LabelEnvironment: node.CatalogEnvironment}).Add(1)
				}
				app.nodeCacheLock.Unlock()
				refreshNotify <- nil
			}
		}
	}
}

// httpReportMetricCollectorBuilder creates a worker that collects metrics from reports
func (app *application) httpReportMetricCollectorBuilder(messageChan chan puppet.ReportData) workering.WorkerFunction {
	return func(ctx context.Context, done chan<- any) {
		for {
			select {
			case <-ctx.Done():
				done <- nil
				return
			case report := <-messageChan:
				app.reportLogCache.Set(report.Host, report.Logs, 1*time.Hour)
				for _, l := range puppet.Levels {
					metrics.NodeLogEntries.With(prometheus.Labels{
						metrics.LabelEnvironment: report.Environment,
						metrics.LabelNode:        report.Host,
						metrics.LabelLevel:       string(l),
					}).Set(0)
					for _, logEntry := range report.Logs {
						if logEntry.Level == string(l) {
							metrics.NodeLogEntries.With(prometheus.Labels{
								metrics.LabelEnvironment: report.Environment,
								metrics.LabelNode:        report.Host,
								metrics.LabelLevel:       string(l),
							}).Add(1)
						}
					}
				}
			}
		}
	}
}

// puppetdbLogMetricCollectorBuilder creates a worker that collects metrics from puppetdb
func (app *application) puppetdbLogMetricCollectorBuilder(refreshNotify chan any) workering.WorkerFunction {
	return func(ctx context.Context, done chan<- any) {
		firstRun := app.settings.puppetdbInitialFetch
		for {
			select {
			case <-ctx.Done():
				done <- nil
				return
			case <-refreshNotify:
				applicationInstance.nodeCacheLock.Lock()
				for _, node := range applicationInstance.nodeCache {
					if node.LatestReportHash == "" {
						sentry.CaptureMessage(fmt.Sprintf("Node %s has no latest report hash", node.Certname))
						log.Println("node", node.Certname, "has no latest report hash")
						continue
					}

					if !firstRun {
						if node.ReportTimestamp.Before(time.Now().Add(-1 * time.Minute)) {
							continue
						}
					}

					var report []puppet.ReportLogEntry

					if item := applicationInstance.reportLogCache.Get(node.LatestReportHash); item != nil {
						go metrics.PuppetDBReportCacheAccess.With(prometheus.Labels{metrics.LabelType: "hit"}).Add(1)
						report = item.Value()
					} else {
						var reportFetchError error
						go metrics.PuppetDBReportCacheAccess.With(prometheus.Labels{metrics.LabelType: "miss"}).Add(1)
						report, reportFetchError = app.puppetDb.GetReportHashInfo(node.LatestReportHash)
						if reportFetchError != nil {
							sentry.CaptureException(reportFetchError)
							log.Println(reportFetchError)
							continue
						}
						applicationInstance.reportLogCache.Set(node.LatestReportHash, report, ttlcache.DefaultTTL)
					}

					for _, l := range puppet.Levels {
						metrics.NodeLogEntries.With(prometheus.Labels{
							metrics.LabelEnvironment: node.ReportEnvironment,
							metrics.LabelNode:        node.Certname,
							metrics.LabelLevel:       string(l),
						}).Set(0)
						for _, logEntry := range report {
							if logEntry.Level == string(l) {
								metrics.NodeLogEntries.With(prometheus.Labels{
									metrics.LabelNode:        node.Certname,
									metrics.LabelEnvironment: node.ReportEnvironment,
									metrics.LabelLevel:       string(l),
								}).Add(1)
							}
						}
					}
				}
				firstRun = false
				applicationInstance.nodeCacheLock.Unlock()
			}
		}

	}
}

var applicationInstance application

const (
	operationModePuppetDB   string = "puppetdb"
	operationModeHTTPReport string = "http-report"
)

func init() {
	app := kingpin.New(os.Args[0], "puppet-report-exporter")
	app.Flag("web.listen-address", "Address to listen on for web interface and telemetry").Envar("WEB_LISTEN_ADDRESS").Default(":9115").StringVar(&applicationInstance.settings.webListenAddress)
	app.Flag("report.listen-address", "Address to listen on for report submission").Envar("REPORT_LISTEN_ADDRESS").Default(":9116").StringVar(&applicationInstance.settings.reportListenAddress)
	app.Flag("mode", "Mode of operation.").Default("puppetdb").Envar("MODE").EnumVar(&applicationInstance.settings.mode, operationModePuppetDB, operationModeHTTPReport)
	app.Flag("puppetdb.api-address", "Address of the PuppetDB API").Default("http://puppetdb:8081").Envar("PUPPETDB_URI").StringVar(&applicationInstance.settings.puppetdbApiAddress)
	app.Flag("puppetdb.initial-fetch", "Fetch all nodes on startup").Default("false").BoolVar(&applicationInstance.settings.puppetdbInitialFetch)
	kingpin.MustParse(app.Parse(os.Args[1:]))
}

func main() {
	log.SetFlags(log.LstdFlags | log.Llongfile)
	log.SetOutput(os.Stdout)

	// initialize sentry instrumentation
	sentry.Init(sentry.ClientOptions{TracesSampleRate: 1.0, Transport: sentry.NewHTTPSyncTransport()})
	defer sentry.Flush(2 * time.Second)

	// start metrics server and cache instrumentation
	workering.Register(
		workering.RegisterSet{
			Name:   "metricsListener",
			Worker: applicationInstance.metricsListener},
		workering.RegisterSet{
			Name:   "reportLogCacheManager",
			Worker: applicationInstance.puppetdbReportLogCacheManager},
	)

	// initialize cache
	applicationInstance.reportLogCache = ttlcache.New[string, []puppet.ReportLogEntry](
		ttlcache.WithTTL[string, []puppet.ReportLogEntry](10 * time.Minute),
	)
	go applicationInstance.reportLogCache.Start()
	defer applicationInstance.reportLogCache.Stop()

	/*
		Prepare dependencies and define workers depending on mode parameter.
		Channels for worker communication must be given via builder functions.
		Each worker is a separate goroutine which will be terminated when the context is cancelled.
	*/
	switch applicationInstance.settings.mode {
	case operationModePuppetDB:
		nodeRefreshChan := make(chan any)
		applicationInstance.puppetDb = puppet.NewApiClient(puppet.WithUrl(applicationInstance.settings.puppetdbApiAddress))

		workering.Register(
			workering.RegisterSet{
				Name:   "puppetdbNodesCrawler",
				Worker: applicationInstance.puppetdbNodesCrawlerBuilder(nodeRefreshChan)},
			workering.RegisterSet{
				Name:   "puppetdbLogMetricCollector",
				Worker: applicationInstance.puppetdbLogMetricCollectorBuilder(nodeRefreshChan)},
		)
	case operationModeHTTPReport:
		messageChan := make(chan puppet.ReportData)

		workering.Register(
			workering.RegisterSet{
				Name:   "reportListener",
				Worker: applicationInstance.reportListenerBuilder(messageChan)},
			workering.RegisterSet{
				Name:   "httpReportMetricCollector",
				Worker: applicationInstance.httpReportMetricCollectorBuilder(messageChan)},
		)
	}

	// capture input signals
	workerSignal := make(chan os.Signal, 1)
	signal.Notify(workerSignal, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	if workerStartError := workering.StartAll(); workerStartError != nil {
		log.Fatal(workerStartError)
	}

	<-workerSignal
	log.Println("Shutting down...")

	applicationInstance.metricServer.Shutdown(context.Background())

	if workerStopError := workering.StopAll(); workerStopError != nil {
		log.Fatal(workerStopError)
	}
	log.Println("Done.")
}
