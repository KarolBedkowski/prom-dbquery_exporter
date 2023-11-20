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
	"reflect"
	"sync"

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

func (t TaskResult) MarshalZerologObject(e *zerolog.Event) {
	e.Str("db", t.DBName).
		Str("query", t.QueryName).
		Err(t.Error).
		Int("result_size", len(t.Result))
}

type dbLoader struct {
	dbName string

	loader    Loader
	tasks     chan *Task
	workQueue chan *Task
	stopCh    chan struct{}
	newConfCh chan *conf.Database
	cfg       *conf.Database
	log       zerolog.Logger
	active    bool
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

		// tasks is queue task to schedule
		tasks: make(chan *Task, 1),
		// workQueue is chan that distribute task to workers
		workQueue: make(chan *Task, tasksQueueSize),
		// newConfCh bring new configuration
		newConfCh: make(chan *conf.Database, 1),
		stopCh:    nil,
		cfg:       cfg,
		log:       log.Logger.With().Str("dbname", name).Logger(),
	}

	return dbl, nil
}

func (d *dbLoader) start() {
	d.log.Debug().Msg("starting")
	go d.mainWorker()
}

func (d *dbLoader) stop() error {
	if !d.active {
		return ErrLoaderStopped
	}

	d.active = false

	d.log.Debug().Msgf("stopping workers")
	close(d.stopCh)

	return nil
}

func (d *dbLoader) updateConf(cfg *conf.Database) bool {
	if reflect.DeepEqual(d.cfg, cfg) {
		return false
	}

	d.newConfCh <- cfg

	return true
}

func (d *dbLoader) mainWorker() {
	var group sync.WaitGroup

	d.active = true
	wlog := d.log.With().Str("db_loader", d.dbName).Logger()

	for d.active {
		select {
		case conf := <-d.newConfCh:
			if d.stopCh != nil {
				wlog.Debug().Msg("stopping...")
				close(d.stopCh)
				group.Wait()
				wlog.Debug().Msg("stopped workers")

				d.stopCh = nil
			}

			ctx := wlog.WithContext(context.Background())
			d.loader.Close(ctx)
			d.loader.UpdateConf(conf)

			d.cfg = conf

			wlog.Debug().Msg("conf updated")

		case task := <-d.tasks:
			if d.stopCh == nil {
				d.spinWorkers(&group)
			}

			d.workQueue <- task
		}
	}

	wlog.Debug().Msg("main worker exit")
}

func (d *dbLoader) spinWorkers(group *sync.WaitGroup) {
	d.stopCh = make(chan struct{}, 1)

	numWorkers := 1
	if d.cfg.Pool != nil {
		numWorkers = d.cfg.Pool.MaxConnections
	}

	d.log.Debug().Msgf("start %d workers", numWorkers)
	d.stopCh = make(chan struct{}, 1)
	d.active = true

	for i := 0; i < numWorkers; i++ {
		i := i

		group.Add(1)

		go func() {
			d.worker(i)
			group.Done()
		}()
	}
}

func (d *dbLoader) worker(idx int) {
	wlog := d.log.With().Int("worker_idx", idx).Logger()

	for {
		select {
		case <-d.stopCh:
			wlog.Debug().Msg("stop worker")

			return

		case task := <-d.workQueue:
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
			if db.updateConf(dbConf) {
				d.log.Info().Str("db", k).Msg("configuration changed")
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
