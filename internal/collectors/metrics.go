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
			Namespace: metrics.MetricsNamespace,
			Name:      "workers_created_total",
			Help:      "Total number of created workers",
		},
		[]string{"database", "kind"})

	tasksQueueWaitTime = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metrics.MetricsNamespace,
			Name:      "tasks_queue_wait_time_seconds",
			Help:      "A histogram of time what log task waiting for handle.",
			Buckets:   []float64{0.05, 0.1, 0.2, 0.5, 1, 5, 10, 30, 60, 120, 300},
		},
		[]string{"database", "kind"})

	// queryDuration is duration of query.
	queryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metrics.MetricsNamespace,
			Name:      "query_duration_seconds",
			Help:      "Duration of query by the DBQuery exporter",
			Buckets:   []float64{0.05, 0.1, 0.2, 0.5, 1, 5, 10, 30, 60, 120, 300},
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
	dbpoolActConnsDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_activeconnections",
		"Number of active connections by database",
		[]string{"database"}, nil,
	)
	dbpoolIdleConnsDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_idleconnections",
		"Number of idle connections by database",
		[]string{"database"}, nil,
	)
	dbpoolOpenConnsDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_openconnections",
		"Number of open connections by database",
		[]string{"database"}, nil,
	)
	dbpoolconfMaxConnsDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_conf_maxopenconnections",
		"Maximal number of open connections by database",
		[]string{"database"}, nil,
	)
	dbpoolConnWaitCntDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_wait_total",
		"Total number of connections waited for per database",
		[]string{"database"}, nil,
	)
	dbpoolConnWaitTimeDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_wait_second_total",
		"The total time blocked waiting for a new connection per database",
		[]string{"database"}, nil,
	)
	dbpoolConnIdleClosedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_idleclosed_total",
		"The total number of connections closed due to idle connection limit.",
		[]string{"database"}, nil,
	)
	dbpoolConnIdleTimeClosedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_idletimeclosed_total",
		"The total number of connections closed due to max idle time limit.",
		[]string{"database"}, nil,
	)
	dbpoolConnLifeTimeClosedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_lifetimeclosed_total",
		"The total number of connections closed due to max life time limit.",
		[]string{"database"}, nil,
	)
	dbpoolConnTotalConnectedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_connected_total",
		"Total number of connections created per database",
		[]string{"database"}, nil,
	)
	dbpoolConnTotalFailedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_failed_total",
		"Total number of failed connections per database",
		[]string{"database"}, nil,
	)

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
