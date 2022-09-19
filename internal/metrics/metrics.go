package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

func init() {
	// Register the metrics to prometheus.
	prometheus.MustRegister(
		NodeCount,
		NodeErrors,
		PuppetDBReportCacheEntries,
	)
}

const (
	LabelEnvironment = "environment"
	LabelNode        = "node"
)

var NodeCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Namespace: "puppet",
	Subsystem: "report_exporter",
	Name:      "node_count",
	Help:      "Number of nodes per environment",
},
	[]string{
		LabelEnvironment,
	},
)

var NodeErrors = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Namespace: "puppet",
		Subsystem: "report_exporter",
		Name:      "node_errors",
		Help:      "status of nodes",
	},
	[]string{
		LabelEnvironment,
		LabelNode,
	},
)

var PuppetDBReportCacheEntries = prometheus.NewGauge(prometheus.GaugeOpts{
	Namespace: "puppet",
	Subsystem: "report_exporter",
	Name:      "puppetdb_report_cache_entries",
	Help:      "Number of entries in the report log cache",
})
