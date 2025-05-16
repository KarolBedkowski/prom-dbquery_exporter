package collectors

//
// mod.go
// Copyright (C) 2023-2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
)

// Collectors is collection of all configured databases.
type Collectors struct {
	log        zerolog.Logger
	cfg        *conf.Configuration
	collectors map[string]*collector
	newConfCh  chan *conf.Configuration
}

// NewCollectors create new Databases object.
func NewCollectors(cfg *conf.Configuration) *Collectors {
	colls := &Collectors{
		collectors: make(map[string]*collector),
		cfg:        cfg,
		log:        log.Logger.With().Str("module", "databases").Logger(),
		newConfCh:  make(chan *conf.Configuration, 1),
	}

	prometheus.MustRegister(
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: metrics.MetricsNamespace,
				Name:      "loaders_in_pool",
				Help:      "Number of active loaders in pool",
			},
			colls.collectorsLen,
		))

	prometheus.MustRegister(colls)

	return colls
}

func (cs *Collectors) Run(ctx context.Context) error {
	for {
		cs.log.Debug().Msg("collectors: starting...")

		group, cancel, err := cs.startLoaders()
		if err != nil {
			return err
		}

	loop:
		for {
			select {
			case <-ctx.Done():
				cs.log.Debug().Msg("collectors: stopping...")
				cs.collectors = nil
				cancel()

				if err := group.Wait(); err != nil {
					cs.log.Error().Err(err).Msg("stopping collectors for new conf error")
				}

				cs.log.Debug().Msg("collectors: stopped")

				return nil

			case cfg := <-cs.newConfCh:
				cs.log.Debug().Msg("collectors: stopping collectors for update configuration")
				cancel()

				if err := group.Wait(); err != nil {
					cs.log.Error().Err(err).Msg("stopping collectors for new conf error")
				}

				cs.cfg = cfg
				cs.log.Debug().Msg("collectors: update configuration finished")

				break loop
			}
		}
	}
}

func (cs *Collectors) AddTask(ctx context.Context, task *Task) {
	if cs.collectors == nil {
		return
	}

	if dbloader, ok := cs.collectors[task.DBName]; ok {
		dbloader.addTask(ctx, task)
	} else {
		task.Output <- task.newResult(ErrAppNotConfigured, nil)
	}
}

// UpdateConf update configuration for existing loaders:
// close not existing any more loaders and close loaders with changed
// configuration so they can be create with new conf on next use.
func (cs *Collectors) UpdateConf(cfg *conf.Configuration) {
	if cfg != nil {
		cs.newConfCh <- cfg
	}
}

func (cs *Collectors) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(cs, ch)
}

func (cs *Collectors) Collect(resCh chan<- prometheus.Metric) { //nolint:funlen
	if cs.collectors == nil {
		return
	}

	cstats := cs.stats()

	for _, cstat := range cstats {
		stat := cstat.dbstats

		if stat == nil {
			continue
		}

		resCh <- prometheus.MustNewConstMetric(
			dbpoolOpenConnsDesc,
			prometheus.GaugeValue,
			float64(stat.DBStats.OpenConnections),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolActConnsDesc,
			prometheus.GaugeValue,
			float64(stat.DBStats.InUse),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolIdleConnsDesc,
			prometheus.GaugeValue,
			float64(stat.DBStats.Idle),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolconfMaxConnsDesc,
			prometheus.GaugeValue,
			float64(stat.DBStats.MaxOpenConnections),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolConnWaitCntDesc,
			prometheus.CounterValue,
			float64(stat.DBStats.WaitCount),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolConnIdleClosedDesc,
			prometheus.CounterValue,
			float64(stat.DBStats.MaxIdleClosed),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolConnIdleTimeClosedDesc,
			prometheus.CounterValue,
			float64(stat.DBStats.MaxIdleTimeClosed),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolConnLifeTimeClosedDesc,
			prometheus.CounterValue,
			float64(stat.DBStats.MaxLifetimeClosed),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolConnWaitTimeDesc,
			prometheus.CounterValue,
			stat.DBStats.WaitDuration.Seconds(),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolConnTotalConnectedDesc,
			prometheus.CounterValue,
			float64(stat.TotalOpenedConnections),
			stat.Name,
		)
		resCh <- prometheus.MustNewConstMetric(
			dbpoolConnTotalFailedDesc,
			prometheus.CounterValue,
			float64(stat.TotalFailedConnections),
			stat.Name,
		)

		resCh <- prometheus.MustNewConstMetric(
			collectorQueueLengthDesc,
			prometheus.GaugeValue,
			float64(cstat.queueLength),
			stat.Name,
			"main",
		)
		resCh <- prometheus.MustNewConstMetric(
			collectorQueueLengthDesc,
			prometheus.GaugeValue,
			float64(cstat.queueBgLength),
			stat.Name,
			"bg",
		)
	}
}

// collectorsLen return number or loaders in pool.
func (cs *Collectors) collectorsLen() float64 {
	if cs == nil {
		return 0
	}

	return float64(len(cs.collectors))
}

// stats return stats for each loaders.
func (cs *Collectors) stats() []collectorStats {
	stats := make([]collectorStats, 0, len(cs.collectors))

	for _, l := range cs.collectors {
		stats = append(stats, l.stats())
	}

	return stats
}

func (cs *Collectors) createCollectors() error {
	collectors := make(map[string]*collector)

	for dbName, dbConf := range cs.cfg.Database {
		if !dbConf.Valid {
			continue
		}

		dbloader, err := newCollector(dbName, dbConf)
		if err != nil {
			cs.log.Error().Err(err).Str("dbname", dbName).Msg("create collector error")

			continue
		}

		collectors[dbName] = dbloader
	}

	if len(collectors) == 0 {
		return InvalidConfigurationError("no databases available")
	}

	cs.collectors = collectors

	return nil
}

func (cs *Collectors) startLoaders() (*errgroup.Group, context.CancelFunc, error) {
	if err := cs.createCollectors(); err != nil {
		return nil, nil, err
	}

	cctx, cancel := context.WithCancel(context.Background())
	group, cctx := errgroup.WithContext(cctx)

	for _, dbloader := range cs.collectors {
		group.Go(func() error { return dbloader.run(cctx) })
	}

	return group, cancel, nil
}
