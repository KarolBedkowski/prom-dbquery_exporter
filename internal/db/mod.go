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
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/trace"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
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

	loader Loader
	// tasks is queue task to schedule
	tasks chan *Task
	// workQueue is chan that distribute task to workers
	workQueue chan *Task
	// newConfCh bring new configuration
	newConfCh      chan *conf.Database
	stopCh         chan struct{}
	cfg            *conf.Database
	log            zerolog.Logger
	active         bool
	runningWorkers int
	createdWorkers uint64
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

		tasks:          make(chan *Task, 1),
		workQueue:      make(chan *Task, tasksQueueSize),
		newConfCh:      make(chan *conf.Database, 1),
		stopCh:         make(chan struct{}, 1),
		cfg:            cfg,
		log:            log.Logger.With().Str("dbname", name).Logger(),
		runningWorkers: 0,
	}

	return dbl, nil
}

func (d *dbLoader) stop() error {
	if !d.active {
		return ErrLoaderStopped
	}

	d.log.Debug().Msgf("stopping...")
	d.stopCh <- struct{}{}

	return nil
}

func (d *dbLoader) addTask(task *Task) {
	if !d.active {
		d.log.Debug().Msg("starting")

		d.active = true

		go d.mainWorker()
	}

	log.Logger.Debug().Msgf("add new task; queue size: %d", len(d.tasks))

	d.tasks <- task
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

	wlog := d.log.With().Str("db_loader", d.dbName).Logger()
	rwCh := make(chan struct{})

	defer close(rwCh)

	support.SetGoroutineLabels(context.Background(), "main_worker", d.dbName)

loop:
	for d.active {
		select {
		case <-rwCh:
			d.runningWorkers--
		case conf := <-d.newConfCh:
			wlog.Debug().Msg("wait for workers finish its current task...")
			group.Wait()
			wlog.Debug().Msg("stopped workers")

			ctx := wlog.WithContext(context.Background())
			d.loader.Close(ctx)
			d.loader.UpdateConf(conf)

			d.cfg = conf

			wlog.Debug().Msg("conf updated")

		case <-d.stopCh:
			d.log.Debug().Msgf("stopping workers...")
			d.active = false
			close(d.tasks)
			group.Wait()
			d.log.Debug().Msgf("stoppped")

			break loop

		case task := <-d.tasks:
			d.workQueue <- task

			if d.runningWorkers < d.cfg.Pool.MaxConnections && len(d.workQueue) > 0 {
				group.Add(1)
				d.runningWorkers++

				go func() {
					d.worker(d.runningWorkers)
					group.Done()

					rwCh <- struct{}{}
				}()
			}
		}
	}

	wlog.Debug().Msg("main worker exit")
}

func (d *dbLoader) worker(idx int) {
	wlog := d.log.With().Int("worker_idx", idx).Logger()

	atomic.AddUint64(&d.createdWorkers, 1)
	support.SetGoroutineLabels(context.Background(), "worker", strconv.Itoa(idx), "db", d.dbName)

	wlog.Debug().Msgf("start worker %d", idx)

	// stop worker after 1 second of inactivty
	shutdownTimer := time.NewTimer(time.Second)

loop:
	for d.active {
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

func (d *dbLoader) handleTask(wlog zerolog.Logger, task *Task) {
	ctx := task.Ctx
	tr, _ := trace.FromContext(ctx)

	tr.LazyPrintf("start query %q in %q", task.QueryName, task.DBName)

	result, err := d.loader.Query(ctx, task.Query, task.Params)
	if err != nil {
		metrics.IncProcessErrorsCnt("query")
		task.Output <- task.newResult(fmt.Errorf("query error: %w", err), nil)

		return
	}

	wlog.Debug().Interface("task", task).Msg("result received")

	output, err := FormatResult(ctx, result, task.Query, d.cfg)
	if err != nil {
		metrics.IncProcessErrorsCnt("format")
		task.Output <- task.newResult(fmt.Errorf("format error: %w", err), nil)

		return
	}

	wlog.Debug().Interface("task", task).Msg("result formatted")
	metrics.ObserveQueryDuration(task.QueryName, task.DBName, result.Duration)
	task.Output <- task.newResult(nil, output)
	tr.LazyPrintf("finished query %q in %q", task.QueryName, task.DBName)
}

func (d *dbLoader) stats() *LoaderStats {
	s := d.loader.Stats()
	if s != nil {
		s.RunningWorkers = uint32(d.runningWorkers)
		s.CreatedWorkers = d.createdWorkers
	}

	return s
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
	if d.cfg == nil {
		return nil, ErrAppNotConfigured
	}

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

	d.log.Debug().Msg("update configuration")

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
	d.Lock()
	defer d.Unlock()

	if d == nil {
		return 0
	}

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
