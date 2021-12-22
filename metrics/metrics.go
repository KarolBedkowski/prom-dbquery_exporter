package metrics

//
// metrics.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"prom-dbquery_exporter.app/support"
)

var (
	// QueryDuration is duration of query
	QueryDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: support.MetricsNamespace,
			Name:      "query_duration_seconds",
			Help:      "Duration of query by the DBQuery exporter",
		},
		[]string{"query", "database"},
	)
	// QueryTotalCnt is total number of query executions
	QueryTotalCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: support.MetricsNamespace,
			Name:      "query_total",
			Help:      "Total numbers queries per database and query",
		},
		[]string{"query", "database"},
	)
	// QueryErrorCnt is total number of execution errors
	QueryErrorCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: support.MetricsNamespace,
			Name:      "query_database_errors_total",
			Help:      "Errors in requests to the DBQuery exporter",
		},
		[]string{"database"},
	)
	// QueryCacheHits is number of result served from cache
	QueryCacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: support.MetricsNamespace,
			Name:      "query_cache_hit",
			Help:      "Number of result loaded from cache",
		},
	)
	// ProcessErrorsCnt is total number of internal errors by category
	ProcessErrorsCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: support.MetricsNamespace,
			Name:      "process_errors_total",
			Help:      "Number of internal processing errors",
		},
		[]string{"error"},
	)

	configReloadTime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: support.MetricsNamespace,
			Name:      "configuration_load_time",
			Help:      "Current configuration load time",
		},
	)

	// ReqDuration measure http request duration
	ReqDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: support.MetricsNamespace,
			Name:      "request_duration_seconds",
			Help:      "A histogram of latencies for requests.",
			Buckets:   []float64{0.5, 1, 5, 10, 60, 120},
		},
		[]string{"handler"},
	)
)

func init() {
	prometheus.MustRegister(QueryDuration)
	prometheus.MustRegister(QueryTotalCnt)
	prometheus.MustRegister(QueryErrorCnt)
	prometheus.MustRegister(QueryCacheHits)
	prometheus.MustRegister(ProcessErrorsCnt)
	prometheus.MustRegister(configReloadTime)
	prometheus.MustRegister(ReqDuration)
}

// UpdateConfLoadTime set current time for configuration_load_time metric
func UpdateConfLoadTime() {
	configReloadTime.Set(float64(time.Now().UTC().Unix()))
}
