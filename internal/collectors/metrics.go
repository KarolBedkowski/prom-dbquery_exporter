package collectors

//
// pool.go
// Copyright (C) 2021-2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"github.com/prometheus/client_golang/prometheus"
	"prom-dbquery_exporter.app/internal/metrics"
)

var (
	// workersCreatedCnt is total number of created workers.
	workersCreatedCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "",
			Name:        "workers_created_total",
			Help:        "Total number of created workers",
			ConstLabels: nil,
		},
		[]string{"database", "kind"})

	tasksQueueWaitTime = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{ //nolint:exhaustruct
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "",
			Name:        "tasks_queue_wait_time_seconds",
			Help:        "A histogram of time what log task waiting for handle.",
			Buckets:     []float64{0.05, 0.1, 0.2, 0.5, 1, 5, 10, 30, 60, 120, 300},
			ConstLabels: nil,
		},
		[]string{"database", "kind"})

	// queryDuration is duration of query.
	queryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{ //nolint:exhaustruct
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "",
			Name:        "query_duration_seconds",
			Help:        "Duration of query by the DBQuery exporter",
			Buckets:     []float64{0.05, 0.1, 0.2, 0.5, 1, 5, 10, 30, 60, 120, 300},
			ConstLabels: nil,
		},
		[]string{"query", "database"},
	)
)

func init() {
	prometheus.MustRegister(workersCreatedCnt)
	prometheus.MustRegister(tasksQueueWaitTime)
	prometheus.MustRegister(queryDuration)
}

var (
	collectorCountDesc = prometheus.NewDesc(
		"dbquery_exporter_collectors_count",
		"Number of active loaders in pool",
		nil, nil,
	)
	collectorQueueLengthDesc = prometheus.NewDesc(
		"dbquery_exporter_collector_queue_len",
		"Number of task in queue",
		[]string{"database", "queue"}, nil,
	)
	collectorActiveDesc = prometheus.NewDesc(
		"dbquery_exporter_collector_active",
		"Collector status",
		[]string{"database"}, nil,
	)
)
