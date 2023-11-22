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

func initMetrics() {
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
	runningWorkersDesc = prometheus.NewDesc(
		"dbquery_exporter_active_workers",
		"Number of active workes",
		[]string{"loader"}, nil,
	)
)

func (l loggersPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(l, ch)
}

func (l loggersPoolCollector) Collect(ch chan<- prometheus.Metric) {
	stats := DatabasesPool.loadersStats()

	for _, stat := range stats {
		ch <- prometheus.MustNewConstMetric(
			dbpoolOpenConnsDesc,
			prometheus.GaugeValue,
			float64(stat.DBStats.OpenConnections),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolActConnsDesc,
			prometheus.GaugeValue,
			float64(stat.DBStats.InUse),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolIdleConnsDesc,
			prometheus.GaugeValue,
			float64(stat.DBStats.Idle),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolconfMaxConnsDesc,
			prometheus.GaugeValue,
			float64(stat.DBStats.MaxOpenConnections),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnWaitCntDesc,
			prometheus.CounterValue,
			float64(stat.DBStats.WaitCount),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnIdleClosedDesc,
			prometheus.CounterValue,
			float64(stat.DBStats.MaxIdleClosed),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnIdleTimeClosedDesc,
			prometheus.CounterValue,
			float64(stat.DBStats.MaxIdleTimeClosed),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnLifeTimeClosedDesc,
			prometheus.CounterValue,
			float64(stat.DBStats.MaxLifetimeClosed),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnWaitTimeDesc,
			prometheus.CounterValue,
			stat.DBStats.WaitDuration.Seconds(),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnTotalConnectedDesc,
			prometheus.CounterValue,
			float64(stat.TotalOpenedConnections),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			dbpoolConnTotalFailedDesc,
			prometheus.CounterValue,
			float64(stat.TotalFailedConnections),
			stat.Name,
		)
		ch <- prometheus.MustNewConstMetric(
			runningWorkersDesc,
			prometheus.GaugeValue,
			float64(stat.RunningWorkers),
			stat.Name,
		)
	}
}
