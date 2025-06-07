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
)

// Collectors is collection of all configured databases.
type Collectors struct {
	log        zerolog.Logger
	cfg        *conf.Configuration
	collectors map[string]*collector
	newCfgCh   chan *conf.Configuration
}

// New create new Collectors object.
func New(cfg *conf.Configuration, cfgCh chan *conf.Configuration) *Collectors {
	colls := &Collectors{
		collectors: make(map[string]*collector),
		cfg:        cfg,
		log:        log.Logger.With().Str("module", "databases").Logger(),
		newCfgCh:   cfgCh,
	}

	prometheus.MustRegister(colls)

	return colls
}

func (cs *Collectors) Run(ctx context.Context) error {
	for {
		cs.log.Debug().Msg("collectors: starting...")

		ctx, cancel := context.WithCancel(ctx)

		group, err := cs.startCollectors(ctx)
		if err != nil {
			cancel()

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

			case cfg := <-cs.newCfgCh:
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
		select {
		case task.Output <- task.newResult(ErrUnknownDatabase, nil):
		case <-ctx.Done():
			cs.log.Warn().Err(ctx.Err()).Msg("context cancelled")
		case <-task.Cancelled():
			cs.log.Warn().Msg("task cancelled")
		}
	}
}

func (cs *Collectors) Describe(ch chan<- *prometheus.Desc) {
	prometheus.DescribeByCollect(cs, ch)
}

func (cs *Collectors) Collect(resCh chan<- prometheus.Metric) {
	resCh <- prometheus.MustNewConstMetric(
		collectorCountDesc,
		prometheus.GaugeValue,
		float64(len(cs.collectors)),
	)

	for _, c := range cs.collectors {
		c.collectMetrics(resCh)
	}
}

func (cs *Collectors) createCollectors() error {
	collectors := make(map[string]*collector)

	for dbName, dbCfg := range cs.cfg.Database {
		if !dbCfg.Valid {
			continue
		}

		dbloader, err := newCollector(dbName, dbCfg)
		if err != nil {
			cs.log.Error().Err(err).Str("dbname", dbName).Msg("create collector error")

			continue
		}

		collectors[dbName] = dbloader
	}

	if len(collectors) == 0 {
		return ErrNoDatabases
	}

	cs.collectors = collectors

	return nil
}

func (cs *Collectors) startCollectors(ctx context.Context) (*errgroup.Group, error) {
	if err := cs.createCollectors(); err != nil {
		return nil, err
	}

	group, cctx := errgroup.WithContext(ctx)

	for _, dbloader := range cs.collectors {
		group.Go(func() error { return dbloader.run(cctx) })
	}

	return group, nil
}
