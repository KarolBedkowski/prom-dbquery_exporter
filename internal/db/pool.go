package db

//
// pool.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
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
			lp.loadersInPool,
		))

	prometheus.MustRegister(loggersPoolCollector{})
}

// loadersPool keep database loaders
type loadersPool struct {
	// map of loader instances
	loaders map[string]Loader
	lock    sync.Mutex
}

var lp loadersPool = loadersPool{
	loaders: make(map[string]Loader),
}

func (l *loadersPool) loadersInPool() float64 {
	lp.lock.Lock()
	defer lp.lock.Unlock()

	return float64(len(l.loaders))
}

func (l *loadersPool) loadersStats() (stats []*LoaderStats) {
	lp.lock.Lock()
	defer lp.lock.Unlock()

	for _, l := range l.loaders {
		if s := l.Stats(); s != nil {
			stats = append(stats, s)
		}
	}

	return stats
}

// GetLoader create or return existing loader according to configuration
func GetLoader(d *conf.Database) (Loader, error) {
	lp.lock.Lock()
	defer lp.lock.Unlock()

	if loader, ok := lp.loaders[d.Name]; ok {
		return loader, nil
	}

	log.Logger.Debug().Str("name", d.Name).Msg("creating new loader")

	loader, err := newLoader(d)
	if err == nil {
		lp.loaders[d.Name] = loader
	}

	return loader, err
}

// UpdateConfiguration update configuration for existing loaders:
// close not existing any more loaders and close loaders with changed
// configuration so they can be create with new conf on next use.
func UpdateConfiguration(c *conf.Configuration) {
	lp.lock.Lock()
	defer lp.lock.Unlock()

	logger := log.Logger
	ctx := logger.WithContext(context.Background())

	var dbToClose []string

	for k, l := range lp.loaders {
		if newConf, ok := c.Database[k]; !ok {
			dbToClose = append(dbToClose, k)
		} else if l.ConfChanged(newConf) {
			logger.Info().Str("db", k).Msg("configuration changed")
			dbToClose = append(dbToClose, k)
		}
	}

	for _, name := range dbToClose {
		l := lp.loaders[name]
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)

		if err := l.Close(cctx); err != nil {
			logger.Error().Err(err).Msg("close loader error")
		}

		cancel()
		delete(lp.loaders, name)
	}
}

// CloseLoaders close all active loaders in pool
func CloseLoaders() {
	lp.lock.Lock()
	defer lp.lock.Unlock()

	log.Logger.Debug().Interface("loaders", lp.loaders).Msg("")

	ctx := log.Logger.WithContext(context.Background())

	for _, l := range lp.loaders {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := l.Close(cctx); err != nil {
			log.Logger.Error().Err(err).Msg("close loader error")
		}

		cancel()
	}
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
	stats := lp.loadersStats()

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
