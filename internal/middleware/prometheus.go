package middleware

import (
	"log"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/bonsai-oss/puppet-report-exporter/internal/metrics"
)

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w, http.StatusOK}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func Prometheus(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		timer := prometheus.NewTimer(metrics.RequestDuration)
		rw := newResponseWriter(w)
		next.ServeHTTP(rw, r)

		statusCode := rw.statusCode

		metrics.RequestResponseStatus.WithLabelValues(strconv.Itoa(statusCode)).Inc()

		log.Printf("%s %s %d %s", r.Method, r.URL.Path, statusCode, timer.ObserveDuration())
	})
}
