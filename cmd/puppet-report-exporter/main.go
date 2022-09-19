package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/bonsai-oss/puppet-report-exporter/internal/metrics"
	"github.com/bonsai-oss/puppet-report-exporter/pkg/puppet"
)

type application struct {
	settings settings

	nodeCache     []puppet.Node
	nodeCacheLock sync.Mutex

	reportLogCache     map[string][]puppet.ReportLogEntry
	reportLogCacheLock sync.Mutex

	metricServer *http.Server
	puppetDb     *puppet.ApiClient
}
type settings struct {
	webListenAddress     string
	puppetdbApiAddress   string
	puppetdbInitialFetch bool
	mode                 string
}
type worker func(ctx context.Context, done chan any)

func (app *application) metricsListener(ctx context.Context, done chan any) {
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

func (app *application) puppetdbReportLogCacheManager(ctx context.Context, done chan any) {
	flushTicker := time.NewTicker(2 * time.Minute)
	metricUpdateTicker := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-ctx.Done():
			flushTicker.Stop()
			metricUpdateTicker.Stop()
			done <- nil
			return
		case <-metricUpdateTicker.C:
			metrics.PuppetDBReportCacheEntries.Set(float64(len(applicationInstance.reportLogCache)))
		case <-flushTicker.C:
			applicationInstance.reportLogCacheLock.Lock()
			log.Printf("Cleaning report cache: %d entries", len(applicationInstance.reportLogCache))
			applicationInstance.reportLogCache = make(map[string][]puppet.ReportLogEntry)
			applicationInstance.reportLogCacheLock.Unlock()
		}
	}
}

func (app *application) puppetdbNodesCrawlerBuilder(refreshNotify chan any) worker {
	return func(ctx context.Context, done chan any) {
		for {
			select {
			case <-ctx.Done():
				done <- nil
				return
			case <-time.After(10 * time.Second):
				nodes, getNodesError := app.puppetDb.GetNodes()
				if getNodesError != nil {
					log.Println(getNodesError)
					continue
				}
				applicationInstance.nodeCacheLock.Lock()
				applicationInstance.nodeCache = nodes
				metrics.NodeCount.Reset()
				for _, node := range nodes {
					metrics.NodeCount.With(prometheus.Labels{metrics.LabelEnvironment: node.CatalogEnvironment}).Add(1)
				}
				applicationInstance.nodeCacheLock.Unlock()
				refreshNotify <- nil
			}
		}
	}
}

func (app *application) puppetdbLogMetricCollectorBuilder(refreshNotify chan any) worker {
	return func(ctx context.Context, done chan any) {
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
						continue
					}

					if !firstRun {
						if node.ReportTimestamp.Before(time.Now().Add(-1 * time.Minute)) {
							continue
						}
					}

					app.reportLogCacheLock.Lock()
					var report []puppet.ReportLogEntry
					if _, foundInCache := applicationInstance.reportLogCache[node.LatestReportHash]; foundInCache {
						report = applicationInstance.reportLogCache[node.LatestReportHash]
					} else {
						var reportFetchError error
						report, reportFetchError = app.puppetDb.GetReportHashInfo(node.LatestReportHash)
						if reportFetchError != nil {
							panic(reportFetchError)
						}
						applicationInstance.reportLogCache[node.LatestReportHash] = report
					}
					app.reportLogCacheLock.Unlock()
					metrics.NodeErrors.With(prometheus.Labels{metrics.LabelNode: node.Certname, metrics.LabelEnvironment: node.ReportEnvironment}).Set(0)
					for _, logEntry := range report {
						if logEntry.Level == "err" {
							metrics.NodeErrors.With(prometheus.Labels{metrics.LabelNode: node.Certname, metrics.LabelEnvironment: node.ReportEnvironment}).Add(1)
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

func init() {
	app := kingpin.New(os.Args[0], "puppet-report-exporter")
	app.Flag("web.listen-address", "Address to listen on for web interface and telemetry").Envar("WEB_LISTEN_ADDRESS").Default(":9115").StringVar(&applicationInstance.settings.webListenAddress)
	app.Flag("mode", "Mode of operation.").Default("puppetdb").Envar("MODE").Hidden().EnumVar(&applicationInstance.settings.mode, "puppetdb", "http-report")
	app.Flag("puppetdb.api-address", "Address of the PuppetDB API").Default("http://puppetdb:8081").Envar("PUPPETDB_URI").StringVar(&applicationInstance.settings.puppetdbApiAddress)
	app.Flag("puppetdb.initial-fetch", "Fetch all nodes on startup").Default("false").BoolVar(&applicationInstance.settings.puppetdbInitialFetch)
	kingpin.MustParse(app.Parse(os.Args[1:]))
}

func main() {
	workers := []worker{
		applicationInstance.metricsListener,
	}

	if applicationInstance.settings.mode == "puppetdb" {
		nodeRefreshChan := make(chan any)
		applicationInstance.puppetDb = puppet.NewApiClient(puppet.WithUrl(applicationInstance.settings.puppetdbApiAddress))
		// initialize puppetdb report log cache
		applicationInstance.reportLogCache = make(map[string][]puppet.ReportLogEntry)
		workers = append(workers, []worker{
			applicationInstance.puppetdbNodesCrawlerBuilder(nodeRefreshChan),
			applicationInstance.puppetdbLogMetricCollectorBuilder(nodeRefreshChan),
			applicationInstance.puppetdbReportLogCacheManager,
		}...)

		log.Printf("Workers: %d", len(workers))
	}

	// capture input signals
	workerContext, workerStop := context.WithCancel(context.Background())
	workerDone := make(chan any)
	workerSignal := make(chan os.Signal, 1)
	signal.Notify(workerSignal, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	workersRunning := 0

	for _, worker := range workers {
		go worker(workerContext, workerDone)
		workersRunning++
	}

	<-workerSignal
	log.Println("Shutting down...")
	applicationInstance.metricServer.Shutdown(workerContext)

	workerStop()
	for range workerDone {
		workersRunning--
		if workersRunning == 0 {
			break
		}
	}
	log.Println("Done.")
}
