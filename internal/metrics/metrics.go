package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	// Register the metrics to prometheus.
	prometheus.MustRegister(
		NodeCount,
		NodeLogEntries,
		PuppetDBReportCacheEntries,
		PuppetDBReportCacheAccess,
		PuppetDBQueries,
	)
}

const (
	LabelEnvironment = "environment"
	LabelNode        = "node"
	LabelLevel       = "level"
	LabelType        = "type"
	LabelEndpoint    = "endpoint"
)

var (
	NodeCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "puppet",
			Subsystem: "report_exporter",
			Name:      "node_count",
			Help:      "Number of nodes per environment",
		},
		[]string{
			LabelEnvironment,
		},
	)

	NodeLogEntries = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "puppet",
			Subsystem: "report_exporter",
			Name:      "node_log_entries",
			Help:      "status of nodes",
		},
		[]string{
			LabelEnvironment,
			LabelLevel,
			LabelNode,
		},
	)

	PuppetDBReportCacheEntries = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "puppet",
			Subsystem: "report_exporter",
			Name:      "puppetdb_report_cache_entries",
			Help:      "Number of entries in the report log cache",
		},
	)

	PuppetDBReportCacheAccess = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "puppet",
			Subsystem: "report_exporter",
			Name:      "puppetdb_report_cache_access",
			Help:      "Number of accesses in the report log cache",
		},
		[]string{
			LabelType,
		},
	)

	PuppetDBQueries = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "puppet",
			Subsystem: "report_exporter",
			Name:      "puppetdb_queries",
			Help:      "Number of queries to the PuppetDB API",
		},
		[]string{
			LabelEndpoint,
		},
	)
)
