package db

//
// mod.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
)

// DBTask is query to perform.
type DBTask struct {
	Ctx context.Context

	DBName    string
	QueryName string

	Query  *conf.Query
	Params map[string]string

	Output chan *DBTaskResult
}

func (d *DBTask) newResult(err error, result []byte) *DBTaskResult {
	return &DBTaskResult{
		Error:     err,
		Result:    result,
		DBName:    d.DBName,
		QueryName: d.QueryName,
		Query:     d.Query,
	}
}

// DBTaskResult is query result.
type DBTaskResult struct {
	Error  error
	Result []byte

	DBName    string
	QueryName string
	Query     *conf.Query
}

type dbLoader struct {
	dbName string

	loader     Loader
	tasks      chan *DBTask
	stopCh     chan struct{}
	newConfCh  chan *conf.Database
	numWorkers int32
	cfg        *conf.Database
	log        zerolog.Logger
	active     bool
}

func newDBLoader(name string, cfg *conf.Database) (*dbLoader, error) {
	loader, err := newLoader(cfg)
	if err != nil {
		return nil, fmt.Errorf("get loader error: %w", err)
	}

	dbl := &dbLoader{
		dbName: name,
		loader: loader,

		tasks:      make(chan *DBTask, 20),
		stopCh:     make(chan struct{}, 1),
		numWorkers: 0,
		cfg:        cfg,
		log:        log.Logger.With().Str("dbname", name).Logger(),
	}

	return dbl, nil
}

func (d *dbLoader) start() error {
	d.log.Debug().Msg("starting")

	numWorkers := 1
	if d.cfg.Pool != nil {
		numWorkers = d.cfg.Pool.MaxConnections
	}

	d.log.Debug().Msgf("start %d workers", numWorkers)
	d.stopCh = make(chan struct{}, 1)
	d.active = true

	for i := 0; i < numWorkers; i++ {
		go d.worker(i)
	}

	return nil
}

func (d *dbLoader) stop() error {
	numWorkers := int(d.numWorkers)
	d.active = false

	d.log.Debug().Msgf("stopping %d workers", numWorkers)
	close(d.stopCh)

	return nil
}

func (d *dbLoader) worker(idx int) {
	wlog := d.log.With().Int("worker_idx", idx).Logger()
	atomic.AddInt32(&d.numWorkers, 1)
	for {
		select {
		case <-d.stopCh:
			wlog.Debug().Msg("stop worker")
			numWorkers := int(atomic.AddInt32(&d.numWorkers, -1))
			switch {
			case numWorkers < 0:
				wlog.Error().Msg("num workers <0")
			case numWorkers == 0:
				if err := d.loader.Close(context.Background()); err != nil {
					wlog.Error().Err(err).Msg("close loader error")
				}
			}
			return

		case task := <-d.tasks:
			wlog.Debug().Interface("task", task).Msg("handle task")

			select {
			case <-task.Ctx.Done():
				wlog.Info().Msg("task cancelled")
				continue
			default:
				start := time.Now()

				result, err := d.loader.Query(task.Ctx, task.Query, task.Params)
				if err != nil {
					metrics.IncProcessErrorsCnt("query")
					wlog.Error().Err(err).Msg("query error")
					task.Output <- task.newResult(fmt.Errorf("query error: %w", err), nil)
					continue
				}

				wlog.Debug().Interface("task", task).Msg("result received")

				output, err := FormatResult(task.Ctx, result, task.Query, d.cfg)
				if err != nil {
					metrics.IncProcessErrorsCnt("format")
					task.Output <- task.newResult(fmt.Errorf("format error: %w", err), nil)
					continue
				}

				wlog.Debug().Interface("task", task).Msg("result formatted")
				metrics.ObserveQueryDuration(task.QueryName, task.DBName,
					float64(time.Now().Sub(start)))

				task.Output <- task.newResult(nil, output)
			}
		}
	}
}

// Databases is collection of all configured databases.
type Databases struct {
	sync.Mutex

	cfg *conf.Configuration
	dbs map[string]*dbLoader

	log zerolog.Logger
}

// NewDatabases create new Databases object.
func NewDatabases() *Databases {
	return &Databases{
		dbs: make(map[string]*dbLoader),
		cfg: nil,
		log: log.Logger.With().Str("module", "databases").Logger(),
	}
}

func (d *Databases) createLoader(dbName string) (*dbLoader, error) {
	dconf, ok := d.cfg.Database[dbName]
	if !ok {
		return nil, fmt.Errorf("unknown database '%s'", dbName)
	}

	var err error
	dl, err := newDBLoader(dbName, dconf)
	if err != nil {
		return nil, fmt.Errorf("create dbloader error: %w", err)
	}

	d.dbs[dbName] = dl
	return dl, nil
}

// PutTask schedule new task.
func (d *Databases) PutTask(task *DBTask) error {
	d.Lock()
	defer d.Unlock()

	dbName := task.DBName

	dl, ok := d.dbs[dbName]
	if !ok {
		var err error
		dl, err = d.createLoader(dbName)
		if err != nil {
			return err
		}
	}

	if !dl.active {
		if err := dl.start(); err != nil {
			if errs := dl.stop(); errs != nil {
				err = errors.Join(err, errs)
			}
			return fmt.Errorf("start dbloader error: %w", err)
		}
	}

	dl.tasks <- task

	return nil
}

// UpdateConf update configuration for existing loaders:
// close not existing any more loaders and close loaders with changed
// configuration so they can be create with new conf on next use.
func (d *Databases) UpdateConf(cfg *conf.Configuration) {
	d.Lock()
	defer d.Unlock()

	d.log.Debug().Msg("update configuration")

	// update existing
	for k, dbConf := range cfg.Database {
		if db, ok := d.dbs[k]; ok {
			if db.loader.ConfChanged(dbConf) {
				d.log.Info().Str("db", k).Msg("configuration changed")
				db.cfg = dbConf
				db.stop()
			}
		}
	}

	// stop not existing anymore
	for k, db := range d.dbs {
		if _, ok := cfg.Database[k]; !ok {
			d.log.Info().Str("db", k).Msg("stopping db")
			db.stop()
		}
	}

	d.cfg = cfg
}

func (d *Databases) Close() {
	d.Lock()
	defer d.Unlock()

	d.log.Debug().Msg("update configuration")

	for k, db := range d.dbs {
		d.log.Info().Str("db", k).Msg("stopping db")
		db.stop()
	}
}

// loadersInPool return number or loaders in pool
func (d *Databases) loadersInPool() float64 {
	d.Lock()
	defer d.Unlock()

	return float64(len(d.dbs))
}

// loadersStats return stats for each loaders
func (d *Databases) loadersStats() []*LoaderStats {
	d.Lock()
	defer d.Unlock()

	var stats []*LoaderStats

	for _, l := range d.dbs {
		if s := l.loader.Stats(); s != nil {
			stats = append(stats, s)
		}
	}

	return stats
}

var DatabasesPool = NewDatabases()
