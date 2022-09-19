package puppet

import (
	"time"
)

type Node struct {
	Deactivated                  interface{} `json:"deactivated"`
	LatestReportHash             string      `json:"latest_report_hash"`
	FactsEnvironment             string      `json:"facts_environment"`
	CachedCatalogStatus          string      `json:"cached_catalog_status"`
	ReportEnvironment            string      `json:"report_environment"`
	LatestReportCorrectiveChange interface{} `json:"latest_report_corrective_change"`
	CatalogEnvironment           string      `json:"catalog_environment"`
	FactsTimestamp               time.Time   `json:"facts_timestamp"`
	LatestReportNoop             bool        `json:"latest_report_noop"`
	Expired                      interface{} `json:"expired"`
	LatestReportNoopPending      bool        `json:"latest_report_noop_pending"`
	ReportTimestamp              time.Time   `json:"report_timestamp"`
	Certname                     string      `json:"certname"`
	CatalogTimestamp             time.Time   `json:"catalog_timestamp"`
	LatestReportJobId            interface{} `json:"latest_report_job_id"`
	LatestReportStatus           string      `json:"latest_report_status"`
}

// ReportLogEntry - representation of https://puppet.com/docs/puppet/7/format_report.html#format_report-puppet-util-log
type ReportLogEntry struct {
	File    interface{} `json:"file"`
	Line    interface{} `json:"line"`
	Tags    []string    `json:"tags"`
	Time    time.Time   `json:"time"`
	Level   string      `json:"level"`
	Source  string      `json:"source"`
	Message string      `json:"message"`
}
