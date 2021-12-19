package main

//
// metrics.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import "github.com/prometheus/client_golang/prometheus"

var (
	// Metrics about the exporter itself.
	queryDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: MetricsNamespace,
			Name:      "query_duration_seconds",
			Help:      "Duration of query by the DBQuery exporter",
		},
		[]string{"query", "database"},
	)
	queryTotalCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "query_total",
			Help:      "Total numbers queries per database and query",
		},
		[]string{"query", "database"},
	)
	queryErrorCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "query_database_errors_total",
			Help:      "Errors in requests to the DBQuery exporter",
		},
		[]string{"database"},
	)
	queryCacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "query_cache_hit",
			Help:      "Number of result loaded from cache",
		},
	)

	processErrorsCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: MetricsNamespace,
			Name:      "process_errors_total",
			Help:      "Number of internal processing errors",
		},
		[]string{"error"},
	)
)

func init() {
	prometheus.MustRegister(queryDuration)
	prometheus.MustRegister(queryTotalCnt)
	prometheus.MustRegister(queryErrorCnt)
	prometheus.MustRegister(queryCacheHits)
	prometheus.MustRegister(processErrorsCnt)
}
