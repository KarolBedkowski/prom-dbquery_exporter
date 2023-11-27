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
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

// Task is query to perform.
type Task struct {
	// Ctx is context used for cancellation.
	Ctx context.Context //nolint:containedctx

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

// MarshalZerologObject implements LogObjectMarshaler.
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

// MarshalZerologObject implements LogObjectMarshaler.
func (t TaskResult) MarshalZerologObject(e *zerolog.Event) {
	e.Str("db", t.DBName).
		Str("query", t.QueryName).
		Err(t.Error).
		Int("result_size", len(t.Result))
}

// Database handle task for one database (loader).
type database struct {
	sync.Mutex

	dbName string
	loader Loader
	cfg    *conf.Database
	log    zerolog.Logger
	active bool
	// tasks is queue task to schedule
	tasks chan *Task
	// workQueue is chan that distribute task to workers
	workQueue chan *Task
	// newConfCh bring new configuration
	newConfCh chan *conf.Database
	// stopCh stopping main worker.
	stopCh chan struct{}
}

const tasksQueueSize = 10

func newDatabase(name string, cfg *conf.Database) (*database, error) {
	loader, err := newLoader(cfg)
	if err != nil {
		return nil, fmt.Errorf("get loader error: %w", err)
	}

	dbl := &database{
		dbName: name,
		loader: loader,

		workQueue: make(chan *Task, tasksQueueSize),
		newConfCh: make(chan *conf.Database, 1),
		stopCh:    make(chan struct{}, 1),
		cfg:       cfg,
		log:       log.Logger.With().Str("dbname", name).Logger(),
	}

	return dbl, nil
}

func (d *database) stop() error {
	d.Lock()
	defer d.Unlock()

	if !d.active {
		return ErrLoaderStopped
	}

	d.log.Debug().Msgf("stopping...")
	d.stopCh <- struct{}{}

	return nil
}

func (d *database) addTask(task *Task) {
	d.Lock()
	defer d.Unlock()

	if !d.active {
		d.log.Debug().Msg("starting")

		d.active = true

		if d.tasks == nil {
			d.tasks = make(chan *Task, 1)
		}

		go d.mainWorker()
	}

	d.log.Debug().Msgf("add new task; queue size: %d", len(d.tasks))

	if d.tasks != nil {
		d.tasks <- task
	} else {
		d.log.Warn().Msg("try add new task to closed queue")
	}
}

func (d *database) updateConf(cfg *conf.Database) bool {
	if reflect.DeepEqual(d.cfg, cfg) {
		return false
	}

	d.newConfCh <- cfg

	return true
}

func (d *database) recreateLoader(cfg *conf.Database) {
	newLoader, err := newLoader(cfg)
	if err != nil || newLoader == nil {
		d.log.Error().Err(err).Msg("create loader with new configuration error")
		d.log.Error().Msg("configuration not updated!")
	} else {
		d.cfg = cfg
		d.loader = newLoader

		d.log.Debug().Msg("conf updated")
	}
}

func (d *database) mainWorker() {
	group := sync.WaitGroup{}
	// rwCh receive worker-stopped events.
	rwCh := make(chan struct{})
	runningWorkers := 0

	defer close(rwCh)

	support.SetGoroutineLabels(context.Background(), "main_worker", d.dbName)

loop:
	for d.active && d.tasks != nil {
		select {
		case <-rwCh:
			runningWorkers--

		case cfg := <-d.newConfCh:
			d.Lock()
			d.log.Debug().Msg("wait for workers finish its current task...")
			group.Wait()
			d.log.Debug().Msg("stopped workers")

			d.loader.Close(d.log.WithContext(context.Background()))
			d.recreateLoader(cfg)
			d.Unlock()

		case <-d.stopCh:
			d.log.Debug().Msg("stopping workers...")
			d.active = false
			close(d.tasks)
			d.tasks = nil
			group.Wait()
			d.log.Debug().Msg("stopped")

			break loop

		case task := <-d.tasks:
			d.workQueue <- task

			if runningWorkers < d.cfg.Pool.MaxConnections && len(d.workQueue) > 0 {
				group.Add(1)
				runningWorkers++

				go func(rw int) {
					d.worker(rw)
					group.Done()

					rwCh <- struct{}{}
				}(runningWorkers)
			}
		}
	}

	d.log.Debug().Msg("main worker exit")
}

func (d *database) worker(idx int) {
	wlog := d.log.With().Int("worker_idx", idx).Logger()

	workersCreatedCnt.WithLabelValues(d.dbName).Inc()
	support.SetGoroutineLabels(context.Background(), "worker", strconv.Itoa(idx), "db", d.dbName)

	wlog.Debug().Msgf("start worker %d", idx)

	// stop worker after 1 second of inactivty
	shutdownTimer := time.NewTimer(time.Second)

loop:
	for d.active {
		// reset worker shutdown timer after each iteration.
		if !shutdownTimer.Stop() {
			select {
			case <-shutdownTimer.C:
			default:
			}
		}
		shutdownTimer.Reset(time.Second)

		select {
		case task := <-d.workQueue:
			wlog.Debug().Interface("task", task).
				Int("queue_len", len(d.workQueue)).
				Msg("handle task")

			if task.Ctx == nil {
				wlog.Error().Interface("task", task).Msg("missing context")

				continue
			}

			support.SetGoroutineLabels(task.Ctx, "query", task.QueryName, "worker", strconv.Itoa(idx), "db", d.dbName)

			select {
			case <-task.Ctx.Done():
				wlog.Warn().Msg("task cancelled before processing")

			default:
				d.handleTask(wlog, task)
			}

			continue

		case <-shutdownTimer.C:
			break loop
		}
	}

	wlog.Debug().Msg("worker stopped")
}

func (d *database) handleTask(wlog zerolog.Logger, task *Task) {
	ctx := task.Ctx
	llog := wlog.With().Object("task", task).Logger()

	support.TracePrintf(ctx, "start query %q in %q", task.QueryName, task.DBName)

	result, err := d.loader.Query(ctx, task.Query, task.Params)
	if err != nil {
		metrics.IncProcessErrorsCnt("query")
		task.Output <- task.newResult(fmt.Errorf("query error: %w", err), nil)

		return
	}

	llog.Debug().Msg("result received")
	metrics.ObserveQueryDuration(task.QueryName, task.DBName, result.Duration)

	output, err := FormatResult(ctx, result, task.Query, d.cfg)
	if err != nil {
		metrics.IncProcessErrorsCnt("format")
		task.Output <- task.newResult(fmt.Errorf("format error: %w", err), nil)

		return
	}

	llog.Debug().Msg("result formatted")
	support.TracePrintf(ctx, "finished  query and formatting %q in %q", task.QueryName, task.DBName)

	select {
	case task.Output <- task.newResult(nil, output):
	default:
		llog.Warn().Msg("can't send response")
	}
}

func (d *database) stats() *LoaderStats {
	return d.loader.Stats()
}

// Databases is collection of all configured databases.
type Databases struct {
	sync.Mutex

	cfg *conf.Configuration
	dbs map[string]*database

	log zerolog.Logger
}

// newDatabases create new Databases object.
func newDatabases() *Databases {
	return &Databases{
		dbs: make(map[string]*database),
		cfg: nil,
		log: log.Logger.With().Str("module", "databases").Logger(),
	}
}

func (d *Databases) createDatabase(dbName string) (*database, error) {
	if d.cfg == nil {
		return nil, ErrAppNotConfigured
	}

	dconf, ok := d.cfg.Database[dbName]
	if !ok {
		return nil, ErrUnknownDatabase
	}

	dl, err := newDatabase(dbName, dconf)
	if err != nil {
		return nil, fmt.Errorf("create dbloader error: %w", err)
	}

	d.dbs[dbName] = dl

	return dl, nil
}

// PutTask schedule new task to process in this database.
func (d *Databases) PutTask(task *Task) error {
	d.Lock()
	defer d.Unlock()

	dbName := task.DBName

	dbloader, ok := d.dbs[dbName]
	if !ok {
		var err error

		dbloader, err = d.createDatabase(dbName)
		if err != nil {
			return err
		}

		if dbloader == nil {
			return ErrAppNotConfigured
		}
	}

	dbloader.addTask(task)

	return nil
}

// UpdateConf update configuration for existing loaders:
// close not existing any more loaders and close loaders with changed
// configuration so they can be create with new conf on next use.
func (d *Databases) UpdateConf(cfg *conf.Configuration) {
	d.Lock()
	defer d.Unlock()

	if cfg == nil {
		return
	}

	d.log.Debug().Msg("update configuration begin")

	// update existing
	for k, dbConf := range cfg.Database {
		if db, ok := d.dbs[k]; ok {
			if db.updateConf(dbConf) {
				d.log.Info().Str("db", k).Msg("configuration changed")
			}
		}
	}

	var toDel []string

	// stop not existing anymore
	for k, db := range d.dbs {
		if _, ok := cfg.Database[k]; !ok {
			d.log.Info().Str("db", k).Msgf("db %s not found in new conf; removing", k)

			_ = db.stop()

			toDel = append(toDel, k)
		}
	}

	for _, k := range toDel {
		delete(d.dbs, k)
	}

	d.cfg = cfg
}

// Close database.
func (d *Databases) Close() {
	d.Lock()
	defer d.Unlock()

	d.log.Debug().Msg("closing databases")

	for k, db := range d.dbs {
		d.log.Info().Str("db", k).Msg("stopping db")

		_ = db.stop()
	}
}

// loadersInPool return number or loaders in pool.
func (d *Databases) loadersInPool() float64 {
	if d == nil {
		return 0
	}

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
		if s := l.stats(); s != nil {
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
