package metrics

//
// metrics.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MetricsNamespace is namespace for prometheus metrics.
const MetricsNamespace = "dbquery_exporter"

var (
	// queryDuration is duration of query.
	queryDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: MetricsNamespace,
			Name:      "query_duration_seconds",
			Help:      "Duration of query by the DBQuery exporter",
		},
		[]string{"query", "database"},
	)
	// queryTotalCnt is total number of query executions.
	queryTotalCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "query_total",
			Help:      "Total numbers queries per database and query",
		},
		[]string{"query", "database"},
	)
	// queryErrorCnt is total number of execution errors.
	queryErrorCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "query_database_errors_total",
			Help:      "Errors in requests to the DBQuery exporter",
		},
		[]string{"database"},
	)
	// queryCacheHits is number of result served from cache.
	queryCacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "query_cache_hit_total",
			Help:      "Number of result loaded from cache",
		},
	)
	// processErrorsCnt is total number of internal errors by category.
	processErrorsCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "process_errors_total",
			Help:      "Number of internal processing errors",
		},
		[]string{"error"},
	)

	configReloadTime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Name:      "configuration_load_time",
			Help:      "Current configuration load time",
		},
	)

	// reqDuration measure http request duration.
	reqDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Name:      "request_duration_seconds",
			Help:      "A histogram of latencies for requests.",
			Buckets:   []float64{0.5, 1, 5, 10, 60, 120},
		},
		[]string{"handler"},
	)

	uptime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: MetricsNamespace,
			Name:      "start_time",
			Help:      "dbquery_exporter start time",
		},
	)
)

func init() {
	prometheus.MustRegister(queryDuration)
	prometheus.MustRegister(queryTotalCnt)
	prometheus.MustRegister(queryErrorCnt)
	prometheus.MustRegister(queryCacheHits)
	prometheus.MustRegister(processErrorsCnt)
	prometheus.MustRegister(configReloadTime)
	prometheus.MustRegister(reqDuration)
	prometheus.MustRegister(uptime)
	uptime.SetToCurrentTime()
}

// UpdateConfLoadTime set current time for configuration_load_time metric.
func UpdateConfLoadTime() {
	configReloadTime.SetToCurrentTime()
}

// IncProcessErrorsCnt increment process errors count in category.
func IncProcessErrorsCnt(category string) {
	processErrorsCnt.WithLabelValues(category).Inc()
}

// IncQueryTotalCnt increment query_total metric.
func IncQueryTotalCnt(queryName, dbName string) {
	queryTotalCnt.WithLabelValues(queryName, dbName).Inc()
}

// IncQueryTotalErrCnt increment query_database_errors_total metric.
func IncQueryTotalErrCnt(dbName string) {
	queryErrorCnt.WithLabelValues(dbName).Inc()
}

// ObserveQueryDuration update query_duration_seconds metric.
func ObserveQueryDuration(queryName, dbName string, duration float64) {
	queryDuration.WithLabelValues(queryName, dbName).Observe(duration)
}

// IncQueryCacheHits increment query_cache_hit metric.
func IncQueryCacheHits() {
	queryCacheHits.Inc()
}

// NewReqDurationWraper create new ObserverVec for InstrumentHandlerDuration.
func NewReqDurationWraper(handler string) prometheus.ObserverVec {
	return reqDuration.MustCurryWith(prometheus.Labels{"handler": handler})
}
