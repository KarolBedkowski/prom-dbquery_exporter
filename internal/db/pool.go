package db

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

func init() {
	prometheus.MustRegister(
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: metrics.MetricsNamespace,
				Name:      "loaders_in_pool",
				Help:      "Number of active loaders in pool",
			},
			DatabasesPool.loadersInPool,
		))

	prometheus.MustRegister(loggersPoolCollector{})
}

// loggersPoolCollector collect metric from active loggers in loggersPool.
type loggersPoolCollector struct{}

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
		"dbquery_exporter_dbpool_connections_waitcount",
		"Total number of connections waited for per loader",
		[]string{"loader"}, nil,
	)
	dbpoolConnWaitTimeDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_wait_second",
		"The total time blocked waiting for a new connection per loader",
		[]string{"loader"}, nil,
	)
	dbpoolConnIdleClosedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_idleclosed",
		"The total number of connections closed due to idle connection limit.",
		[]string{"loader"}, nil,
	)
	dbpoolConnIdleTimeClosedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_idletimeclosed",
		"The total number of connections closed due to max idle time limit.",
		[]string{"loader"}, nil,
	)
	dbpoolConnLifeTimeClosedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_lifetimeclosed",
		"The total number of connections closed due to max life time limit.",
		[]string{"loader"}, nil,
	)
	dbpoolConnTotalConnectedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_connected",
		"Total number of connections created per loader",
		[]string{"loader"}, nil,
	)
	dbpoolConnTotalFailedDesc = prometheus.NewDesc(
		"dbquery_exporter_dbpool_connections_failed",
		"Total number of failed connections per loader",
		[]string{"loader"}, nil,
	)
)

func (l loggersPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(l, ch)
}

func (l loggersPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := DatabasesPool.loadersStats()

	for _, s := range stats {
		ch <- prometheus.MustNewConstMetric(
			dbpoolOpenConnsDesc,
			prometheus.GaugeValue,
			float64(s.DBStats.OpenConnections),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolActConnsDesc,
			prometheus.GaugeValue,
			float64(s.DBStats.InUse),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolIdleConnsDesc,
			prometheus.GaugeValue,
			float64(s.DBStats.Idle),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolconfMaxConnsDesc,
			prometheus.GaugeValue,
			float64(s.DBStats.MaxOpenConnections),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnWaitCntDesc,
			prometheus.CounterValue,
			float64(s.DBStats.WaitCount),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnIdleClosedDesc,
			prometheus.CounterValue,
			float64(s.DBStats.MaxIdleClosed),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnIdleTimeClosedDesc,
			prometheus.CounterValue,
			float64(s.DBStats.MaxIdleTimeClosed),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnLifeTimeClosedDesc,
			prometheus.CounterValue,
			float64(s.DBStats.MaxLifetimeClosed),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnWaitTimeDesc,
			prometheus.CounterValue,
			float64(s.DBStats.WaitDuration.Seconds()),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnTotalConnectedDesc,
			prometheus.CounterValue,
			float64(s.TotalOpenedConnections),
			s.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnTotalFailedDesc,
			prometheus.CounterValue,
			float64(s.TotalFailedConnections),
			s.Name,
		)
	}
}
