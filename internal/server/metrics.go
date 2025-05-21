package server

import (
	"github.com/prometheus/client_golang/prometheus"
	"prom-dbquery_exporter.app/internal/metrics"
)

// metrics.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
var (
	// queryTotalCnt is total number of query executions.
	queryTotalCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metrics.MetricsNamespace,
			Name:      "query_total",
			Help:      "Total numbers queries per database and query",
		},
		[]string{"query", "database"},
	)
	// queryErrorCnt is total number of execution errors.
	queryErrorCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metrics.MetricsNamespace,
			Name:      "query_database_errors_total",
			Help:      "Errors in requests to the DBQuery exporter",
		},
		[]string{"database"},
	)
	// reqDuration measure http request duration.
	reqDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metrics.MetricsNamespace,
			Name:      "request_duration_seconds",
			Help:      "A histogram of latencies for requests.",
			Buckets:   []float64{0.1, 0.2, 0.5, 1, 5, 10, 30, 60, 120, 300},
		},
		[]string{"handler"},
	)
	reqInFlightCnt = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metrics.MetricsNamespace,
			Name:      "query_database_requests_in_flight",
			Help:      "Number of concurrent request to the DBQuery exporter",
		},
		[]string{"handler"},
	)
)

func init() {
	prometheus.MustRegister(queryTotalCnt)
	prometheus.MustRegister(queryErrorCnt)
	prometheus.MustRegister(reqDuration)
}

// NewReqDurationWrapper create new ObserverVec for InstrumentHandlerDuration.
func newReqDurationWrapper(handler string) prometheus.ObserverVec {
	return reqDuration.MustCurryWith(prometheus.Labels{"handler": handler})
}

// NewReqDurationWrapper create new ObserverVec for InstrumentHandlerDuration.
func newReqInflightWrapper(handler string) prometheus.Gauge { //nolint:ireturn
	return reqInFlightCnt.With(prometheus.Labels{"handler": handler})
}

// IncQueryTotalErrCnt increment query_database_errors_total metric.
func IncQueryTotalErrCnt(dbName string) {
	queryErrorCnt.WithLabelValues(dbName).Inc()
}
