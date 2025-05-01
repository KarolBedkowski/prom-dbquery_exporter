package collectors

//
// pool.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
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
		[]string{"loader"})
	tasksQueueWaitTime = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metrics.MetricsNamespace,
			Name:      "tasks_queue_wait_time_seconds",
			Help:      "A histogram of time what log task waiting for handle.",
			Buckets:   []float64{0.05, 0.1, 0.2, 0.5, 1, 5, 10, 30, 60, 120, 300},
		},
		[]string{"loader"})
)

func initMetrics() {
	prometheus.MustRegister(workersCreatedCnt)
	prometheus.MustRegister(tasksQueueWaitTime)
}

var (
	dbpoolActConnsDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_activeconnections",
		"Number of active connections by loader",
		[]string{"loader"}, nil,
	)
	dbpoolIdleConnsDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_idleconnections",
		"Number of idle connections by loader",
		[]string{"loader"}, nil,
	)
	dbpoolOpenConnsDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_openconnections",
		"Number of open connections by loader",
		[]string{"loader"}, nil,
	)
	dbpoolconfMaxConnsDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_conf_maxopenconnections",
		"Maximal number of open connections by loader",
		[]string{"loader"}, nil,
	)
	dbpoolConnWaitCntDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_wait_total",
		"Total number of connections waited for per loader",
		[]string{"loader"}, nil,
	)
	dbpoolConnWaitTimeDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_wait_second_total",
		"The total time blocked waiting for a new connection per loader",
		[]string{"loader"}, nil,
	)
	dbpoolConnIdleClosedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_idleclosed_total",
		"The total number of connections closed due to idle connection limit.",
		[]string{"loader"}, nil,
	)
	dbpoolConnIdleTimeClosedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_idletimeclosed_total",
		"The total number of connections closed due to max idle time limit.",
		[]string{"loader"}, nil,
	)
	dbpoolConnLifeTimeClosedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_lifetimeclosed_total",
		"The total number of connections closed due to max life time limit.",
		[]string{"loader"}, nil,
	)
	dbpoolConnTotalConnectedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_connected_total",
		"Total number of connections created per loader",
		[]string{"loader"}, nil,
	)
	dbpoolConnTotalFailedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_failed_total",
		"Total number of failed connections per loader",
		[]string{"loader"}, nil,
	)
	collectorQueueLengthDesc = prometheus.NewDesc(
		"dbquery_exporter_collector_queue_len",
		"Number of task in queue",
		[]string{"loader", "queue"}, nil,
	)
)
