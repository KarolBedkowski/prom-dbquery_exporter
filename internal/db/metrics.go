package db

//
// metrics.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"github.com/prometheus/client_golang/prometheus"
	"prom-dbquery_exporter.app/internal/metrics"
)

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
)

var (
	dbpoolConnOpenedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "dbpool",
			Name:        "connections_connected_total",
			Help:        "Total number of connections created per database",
			ConstLabels: nil,
		},
		[]string{"database"})

	dbpoolConnFailedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "dbpool",
			Name:        "connections_failed_total",
			Help:        "Total number of failed connections per database",
			ConstLabels: nil,
		},
		[]string{"database"})
)

func init() {
	prometheus.MustRegister(dbpoolConnOpenedTotal)
	prometheus.MustRegister(dbpoolConnFailedTotal)
}
