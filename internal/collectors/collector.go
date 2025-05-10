package collectors

//
// collector.go
// Copyright (C) 2023-2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/db"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

// collector handle task for one loader.
type collector struct {
	log    zerolog.Logger
	loader db.Database
	cfg    *conf.Database
	// tasks is queue task to schedule.
	tasks chan *Task
	// workQueue is chan that distribute task to workers.
	workQueue chan *Task
	// workBgQueue is chan that distribute task created by scheduler to dedicated pool of workers.
	workBgQueue chan *Task
	dbName      string

	// normalWorkersGroup is pool of workers that handle normal tasks and scheduled task when
	// background workers are disabled.
	normalWorkersGroup errgroup.Group
	// bgWorkersGroup is pool of workers that handle only tasks created by scheduler.
	bgWorkersGroup errgroup.Group

	// workerID is last unique worker id
	workerID atomic.Uint64
}

const tasksQueueSize = 10

func newCollector(name string, cfg *conf.Database) (*collector, error) {
	loader, err := db.GlobalRegistry.GetInstance(cfg)
	if err != nil {
		return nil, fmt.Errorf("get loader error: %w", err)
	}

	col := &collector{
		dbName:      name,
		loader:      loader,
		workQueue:   make(chan *Task, tasksQueueSize),
		workBgQueue: make(chan *Task, tasksQueueSize),
		cfg:         cfg,
		log:         log.Logger.With().Str("dbname", name).Logger(),
	}

	col.normalWorkersGroup.SetLimit(cfg.MaxWorkers)
	col.bgWorkersGroup.SetLimit(cfg.BackgroundWorkers)

	return col, nil
}

func (c *collector) addTask(task *Task) {
	c.log.Debug().Msgf("collector: add new task; queue size: %d", len(c.tasks))

	if c.tasks != nil {
		c.tasks <- task
	} else {
		c.log.Warn().Msg("collector: try add new task to closed queue")
	}
}

// run collector and process task in loop.
func (c *collector) run(ctx context.Context) error {
	c.log.Debug().Msg("collector: starting")

	c.tasks = make(chan *Task, 1)
	defer close(c.tasks)

	support.SetGoroutineLabels(context.Background(), "main_worker", c.dbName)

loop:
	for {
		select {
		case <-ctx.Done():
			c.log.Debug().Msg("collector: stopping workers...")

			break loop

		case task := <-c.tasks:
			if task.IsScheduledJob && c.cfg.BackgroundWorkers > 0 {
				c.workBgQueue <- task
				c.spawnBgWorker(ctx)
			} else {
				c.workQueue <- task
				c.spawnWorker(ctx)
			}
		}
	}

	_ = c.bgWorkersGroup.Wait()
	_ = c.normalWorkersGroup.Wait()

	c.log.Debug().Msg("collector: main worker exit")

	return nil
}

// spawnWorker start worker for normal queue.
func (c *collector) spawnWorker(ctx context.Context) {
	if len(c.workQueue) == 0 {
		return
	}

	c.normalWorkersGroup.TryGo(func() error {
		c.worker(ctx, c.workQueue, false)

		return nil
	})
}

// spawnBgWorker start worker for background queue.
func (c *collector) spawnBgWorker(ctx context.Context) {
	if len(c.workBgQueue) == 0 {
		return
	}

	c.bgWorkersGroup.TryGo(func() error {
		c.worker(ctx, c.workBgQueue, true)

		return nil
	})
}

// worker get process task from `workQueue` in loop. Exit when there is no new task for 1 sec.
func (c *collector) worker(ctx context.Context, workQueue chan *Task, isBg bool) {
	idx := c.workerID.Add(1)
	idxstr := strconv.FormatUint(idx, 10)
	wlog := c.log.With().Bool("bg", isBg).Uint64("worker_id", idx).Logger()

	workersCreatedCnt.WithLabelValues(c.dbName).Inc()
	support.SetGoroutineLabels(context.Background(), "worker", idxstr, "db", c.dbName, "bg", strconv.FormatBool(isBg))

	wlog.Debug().Msgf("collector: start worker %d  bg=%v", idx, isBg)

	// stop worker after 1 second of inactivity
	shutdownTimer := time.NewTimer(time.Second)
	defer shutdownTimer.Stop()

loop:
	for {
		// reset worker shutdown timer after each iteration.
		if !shutdownTimer.Stop() {
			select {
			case <-shutdownTimer.C:
			default:
			}
		}

		shutdownTimer.Reset(time.Second)

		select {
		case <-ctx.Done():
			break loop

		case task, ok := <-workQueue:
			if ok {
				wlog.Debug().Object("task", task).Int("queue_len", len(workQueue)).Msg("collector: handle task")
				support.SetGoroutineLabels(ctx, "query", task.QueryName, "worker", idxstr, "db", c.dbName, "req_id", task.ReqID)
				tasksQueueWaitTime.WithLabelValues(c.dbName).Observe(time.Since(task.RequestStart).Seconds())

				c.handleTask(ctx, wlog, task)
			}

		case <-shutdownTimer.C:
			break loop
		}
	}

	wlog.Debug().Msg("collector: worker stopped")
}

func (c *collector) handleTask(ctx context.Context, wlog zerolog.Logger, task *Task) {
	llog := wlog.With().Object("task", task).Logger()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// is task already cancelled?
	select {
	case <-task.Cancelled():
		llog.Warn().Msg("collector: task cancelled before processing")

		return
	case <-ctx.Done():
		return
	default:
	}

	// get result
	res := make(chan *TaskResult, 1)
	go c.queryDatabase(ctx, wlog, task, res)

	// check is task cancelled in meantime
	select {
	case <-ctx.Done():
		llog.Warn().Err(ctx.Err()).Msg("collector: worker stopped when processing")

	case <-task.Cancelled():
		llog.Warn().Msg("collector: task cancelled when processing")

	case r := <-res:
		// send result
		task.Output <- r
	}
}

// queryDatabase get result from loader.
func (c *collector) queryDatabase(ctx context.Context, llog zerolog.Logger, task *Task, res chan *TaskResult) {
	support.TracePrintf(ctx, "start query %q in %q", task.QueryName, task.DBName)

	result, err := c.loader.Query(ctx, task.Query, task.Params)
	if err != nil {
		metrics.IncProcessErrorsCnt("query")

		res <- c.handleQueryError(ctx, llog, task, err)

		return
	}

	llog.Debug().Msg("collector: result received")
	metrics.ObserveQueryDuration(task.QueryName, task.DBName, result.Duration)

	output, err := formatResult(ctx, result, task.Query, c.cfg)
	if err != nil {
		metrics.IncProcessErrorsCnt("format")
		res <- task.newResult(fmt.Errorf("format error: %w", err), nil)

		return
	}

	llog.Debug().Msg("collector: result formatted")
	support.TracePrintf(ctx, "finished  query and formatting %q in %q", task.QueryName, task.DBName)

	res <- task.newResult(nil, output)
}

func (c *collector) handleQueryError(ctx context.Context, llog zerolog.Logger, task *Task, err error) *TaskResult {
	// When OnError is defined - generate metrics according to this template
	if task.Query.OnErrorTpl != nil {
		llog.Warn().Err(err).Msg("collector: query error handled by on error template")

		if output, err := formatError(ctx, err, task.Query, c.cfg); err != nil {
			llog.Error().Err(err).Msg("collector: format error result error")
		} else {
			llog.Debug().Bytes("output", output).Msg("result")

			return task.newResult(nil, output)
		}
	}

	return task.newResult(fmt.Errorf("query error: %w", err), nil)
}

type collectorStats struct {
	dbstats *db.DatabaseStats

	queueLength   int
	queueBgLength int
}

func (c *collector) stats() collectorStats {
	return collectorStats{
		dbstats:       c.loader.Stats(),
		queueLength:   len(c.workQueue),
		queueBgLength: len(c.workBgQueue),
	}
}
