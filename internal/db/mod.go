package db

//
// mod.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
)

// Task is query to perform.
type Task struct {
	Ctx context.Context

	DBName    string
	QueryName string

	Query  *conf.Query
	Params map[string]string

	Output chan *TaskResult
}

func (d *Task) newResult(err error, result []byte) *TaskResult {
	return &TaskResult{
		Error:     err,
		Result:    result,
		DBName:    d.DBName,
		QueryName: d.QueryName,
		Query:     d.Query,
	}
}

func (d Task) MarshalZerologObject(e *zerolog.Event) {
	e.Str("db", d.DBName).
		Str("query", d.QueryName).
		Interface("params", d.Params)

	if rid, ok := hlog.IDFromCtx(d.Ctx); ok {
		e.Str("req_id", rid.String())
	}
}

// TaskResult is query result.
type TaskResult struct {
	Error  error
	Result []byte

	DBName    string
	QueryName string
	Query     *conf.Query
}

type dbLoader struct {
	dbName string

	loader     Loader
	tasks      chan *Task
	stopCh     chan struct{}
	newConfCh  chan *conf.Database
	numWorkers int32
	cfg        *conf.Database
	log        zerolog.Logger
	active     bool
}

const tasksQueueSize = 10

func newDBLoader(name string, cfg *conf.Database) (*dbLoader, error) {
	loader, err := newLoader(cfg)
	if err != nil {
		return nil, fmt.Errorf("get loader error: %w", err)
	}

	dbl := &dbLoader{
		dbName: name,
		loader: loader,

		tasks:      make(chan *Task, tasksQueueSize),
		stopCh:     make(chan struct{}, 1),
		numWorkers: 0,
		cfg:        cfg,
		log:        log.Logger.With().Str("dbname", name).Logger(),
	}

	return dbl, nil
}

func (d *dbLoader) start() {
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
}

func (d *dbLoader) stop() error {
	if !d.active {
		return ErrLoaderStopped
	}

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
				d.handleTask(wlog, task)
			}
		}
	}
}

func (d *dbLoader) handleTask(wlog zerolog.Logger, task *Task) {
	result, err := d.loader.Query(task.Ctx, task.Query, task.Params)
	if err != nil {
		metrics.IncProcessErrorsCnt("query")
		task.Output <- task.newResult(fmt.Errorf("query error: %w", err), nil)

		return
	}

	wlog.Debug().Interface("task", task).Msg("result received")

	output, err := FormatResult(task.Ctx, result, task.Query, d.cfg)
	if err != nil {
		metrics.IncProcessErrorsCnt("format")
		task.Output <- task.newResult(fmt.Errorf("format error: %w", err), nil)

		return
	}

	wlog.Debug().Interface("task", task).Msg("result formatted")
	metrics.ObserveQueryDuration(task.QueryName, task.DBName, result.Duration)
	task.Output <- task.newResult(nil, output)
}

// Databases is collection of all configured databases.
type Databases struct {
	sync.Mutex

	cfg *conf.Configuration
	dbs map[string]*dbLoader

	log zerolog.Logger
}

// newDatabases create new Databases object.
func newDatabases() *Databases {
	return &Databases{
		dbs: make(map[string]*dbLoader),
		cfg: nil,
		log: log.Logger.With().Str("module", "databases").Logger(),
	}
}

func (d *Databases) createLoader(dbName string) (*dbLoader, error) {
	dconf, ok := d.cfg.Database[dbName]
	if !ok {
		return nil, ErrUnknownDatabase
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
func (d *Databases) PutTask(task *Task) error {
	d.Lock()
	defer d.Unlock()

	dbName := task.DBName

	dbloader, ok := d.dbs[dbName]
	if !ok {
		var err error

		dbloader, err = d.createLoader(dbName)

		if err != nil {
			return err
		}
	}

	if !dbloader.active {
		dbloader.start()
	}

	dbloader.tasks <- task

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
			if db.loader.UpdateConf(dbConf) {
				d.log.Info().Str("db", k).Msg("configuration changed")

				db.cfg = dbConf

				_ = db.stop()
			}
		}
	}

	// stop not existing anymore
	for k, db := range d.dbs {
		if _, ok := cfg.Database[k]; !ok {
			d.log.Info().Str("db", k).Msg("stopping db")

			_ = db.stop()
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

		_ = db.stop()
	}
}

// loadersInPool return number or loaders in pool.
func (d *Databases) loadersInPool() float64 {
	d.Lock()
	defer d.Unlock()

	return float64(len(d.dbs))
}

// loadersStats return stats for each loaders.
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

// DatabasesPool is global handler for all db queries.
var DatabasesPool *Databases

// Init db subsystem.
func Init() {
	DatabasesPool = newDatabases()

	initMetrics()
}
