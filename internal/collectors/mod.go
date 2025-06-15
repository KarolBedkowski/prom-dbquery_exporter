package collectors

//
// mod.go
// Copyright (C) 2023-2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"sync"

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
	newCfgCh   chan *conf.Configuration

	mu sync.Mutex
}

// New create new Collectors object.
func New(cfg *conf.Configuration, cfgCh chan *conf.Configuration) *Collectors {
	colls := &Collectors{
		collectors: make(map[string]*collector),
		cfg:        cfg,
		log:        log.Logger.With().Str("module", "databases").Logger(),
		newCfgCh:   cfgCh,
		mu:         sync.Mutex{},
	}

	prometheus.MustRegister(colls)

	return colls
}

func (cs *Collectors) Run(ctx context.Context) error {
	for {
		cs.log.Debug().Msg("collectors: starting...")

		lctx, cancel := context.WithCancel(ctx)
		defer cancel()

		group, err := cs.startCollectors(lctx)
		if err != nil {
			return err
		}

		stop := func() {
			cs.log.Debug().Msg("collectors: stopping...")
			cs.collectors = nil

			cancel()

			if err := group.Wait(); err != nil {
				cs.log.Error().Err(err).Msg("stopping collectors error")
			}

			cs.log.Debug().Msg("collectors: stopped")
		}

	loop:
		for {
			select {
			case <-lctx.Done():
				stop()

				return nil

			case cfg := <-cs.newCfgCh:
				stop()

				cs.cfg = cfg
				cs.log.Debug().Msg("collectors: update configuration finished")

				break loop
			}
		}
	}
}

func (cs *Collectors) AddTask(ctx context.Context, task *Task) {
	dbloader := cs.getCollector(task.DBName)

	if dbloader != nil {
		dbloader.addTask(ctx, task)

		return
	}

	select {
	case task.Output <- task.newErrorResult(ErrUnknownDatabase, metrics.ErrCategoryInternalError):
	case <-ctx.Done():
		cs.log.Warn().Err(ctx.Err()).Msg("context cancelled")
	case <-task.Cancelled():
		cs.log.Warn().Msg("task cancelled")
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
	cs.mu.Lock()
	defer cs.mu.Unlock()

	collectors := make(map[string]*collector)

	for dbcfg := range cs.cfg.ValidDatabases {
		dbloader, err := newCollector(dbcfg)
		if err != nil {
			cs.log.Error().Err(err).Str("database", dbcfg.Name).Msg("create collector error")
		} else {
			collectors[dbcfg.Name] = dbloader
		}
	}

	if len(collectors) == 0 {
		return ErrNoDatabases
	}

	cs.collectors = collectors

	return nil
}

func (cs *Collectors) startCollectors(ctx context.Context) (*errgroup.Group, error) {
	err := cs.createCollectors()
	if err != nil {
		return nil, err
	}

	group, cctx := errgroup.WithContext(ctx)

	for _, dbloader := range cs.collectors {
		group.Go(func() error { return dbloader.run(cctx) })
	}

	return group, nil
}

func (cs *Collectors) getCollector(name string) *collector {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.collectors == nil {
		// this may happen when configuration is reloaded; we can't do anything
		return nil
	}

	return cs.collectors[name]
}
