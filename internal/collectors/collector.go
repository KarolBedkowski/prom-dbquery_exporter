package collectors

//
// collector.go
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
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/db"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

// collector handle task for one collector (loader).
type collector struct {
	log    zerolog.Logger
	loader db.Database
	cfg    *conf.Database
	// tasks is queue task to schedule
	tasks chan *Task
	// workQueue is chan that distribute task to workers
	workQueue chan *Task
	// newConfCh bring new configuration
	newConfCh chan *conf.Database
	// stopCh stopping main worker.
	stopCh chan struct{}
	dbName string
	sync.Mutex
	active bool
}

const tasksQueueSize = 10

func newCollector(name string, cfg *conf.Database) (*collector, error) {
	loader, err := db.CreateLoader(cfg)
	if err != nil {
		return nil, fmt.Errorf("get loader error: %w", err)
	}

	c := &collector{
		dbName: name,
		loader: loader,

		workQueue: make(chan *Task, tasksQueueSize),
		newConfCh: make(chan *conf.Database, 1),
		stopCh:    make(chan struct{}, 1),
		cfg:       cfg,
		log:       log.Logger.With().Str("dbname", name).Logger(),
	}

	return c, nil
}

func (c *collector) stop() error {
	c.Lock()
	defer c.Unlock()

	if !c.active {
		return ErrLoaderStopped
	}

	c.log.Debug().Msgf("stopping...")
	c.stopCh <- struct{}{}

	return nil
}

func (c *collector) addTask(task *Task) {
	c.Lock()
	defer c.Unlock()

	if !c.active {
		c.log.Debug().Msg("starting")

		c.active = true

		if c.tasks == nil {
			c.tasks = make(chan *Task, 1)
		}

		go c.mainWorker()
	}

	c.log.Debug().Msgf("add new task; queue size: %d", len(c.tasks))

	if c.tasks != nil {
		c.tasks <- task
	} else {
		c.log.Warn().Msg("try add new task to closed queue")
	}
}

func (c *collector) updateConf(cfg *conf.Database) bool {
	if reflect.DeepEqual(c.cfg, cfg) {
		return false
	}

	c.newConfCh <- cfg

	return true
}

func (c *collector) recreateLoader(cfg *conf.Database) {
	newLoader, err := db.CreateLoader(cfg)
	if err != nil || newLoader == nil {
		c.log.Error().Err(err).Msg("create loader with new configuration error")
		c.log.Error().Msg("configuration not updated!")
	} else {
		c.cfg = cfg
		c.loader = newLoader

		c.log.Debug().Msg("conf updated")
	}
}

func (c *collector) mainWorker() {
	group := sync.WaitGroup{}
	// rwCh receive worker-stopped events.
	rwCh := make(chan struct{})
	runningWorkers := 0

	defer close(rwCh)

	support.SetGoroutineLabels(context.Background(), "main_worker", c.dbName)

loop:
	for c.active && c.tasks != nil {
		select {
		case <-rwCh:
			runningWorkers--

		case cfg := <-c.newConfCh:
			c.Lock()
			c.log.Debug().Msg("wait for workers finish its current task...")
			group.Wait()
			c.log.Debug().Msg("stopped workers")

			c.loader.Close(c.log.WithContext(context.Background()))
			c.recreateLoader(cfg)
			c.Unlock()

		case <-c.stopCh:
			c.log.Debug().Msg("stopping workers...")
			c.active = false
			close(c.tasks)
			c.tasks = nil
			group.Wait()
			c.log.Debug().Msg("stopped")

			break loop

		case task := <-c.tasks:
			c.workQueue <- task

			if runningWorkers < c.cfg.Pool.MaxConnections && len(c.workQueue) > 0 {
				group.Add(1)
				runningWorkers++

				go func(rw int) {
					c.worker(rw)
					group.Done()

					rwCh <- struct{}{}
				}(runningWorkers)
			}
		}
	}

	c.log.Debug().Msg("main worker exit")
}

func (c *collector) worker(idx int) {
	wlog := c.log.With().Int("worker_idx", idx).Logger()

	workersCreatedCnt.WithLabelValues(c.dbName).Inc()
	support.SetGoroutineLabels(context.Background(), "worker", strconv.Itoa(idx), "db", c.dbName)

	wlog.Debug().Msgf("start worker %d", idx)

	// stop worker after 1 second of inactivty
	shutdownTimer := time.NewTimer(time.Second)
	defer shutdownTimer.Stop()

loop:
	for c.active {
		// reset worker shutdown timer after each iteration.
		if !shutdownTimer.Stop() {
			select {
			case <-shutdownTimer.C:
			default:
			}
		}

		shutdownTimer.Reset(time.Second)

		select {
		case task := <-c.workQueue:
			wlog.Debug().Object("task", task).Int("queue_len", len(c.workQueue)).Msg("handle task")
			support.SetGoroutineLabels(task.Ctx, "query", task.QueryName, "worker", strconv.Itoa(idx), "db", c.dbName)
			tasksQueueWaitTime.WithLabelValues(c.dbName).Observe(time.Since(task.RequestStart).Seconds())

			c.handleTask(wlog, task)

			continue
		case <-shutdownTimer.C:
			break loop
		}
	}

	wlog.Debug().Msg("worker stopped")
}

func (c *collector) handleTask(wlog zerolog.Logger, task *Task) {
	llog := wlog.With().Object("task", task).Logger()

	select {
	case <-task.Ctx.Done():
		llog.Warn().Msg("task cancelled before processing")

		return
	default:
	}

	if task.Ctx.Err() != nil {
		return
	}

	res := c.doQuery(wlog, task)

	select {
	case <-task.Ctx.Done():
		llog.Warn().Msg("task cancelled after processing")

		return
	default:
	}

	if task.Ctx.Err() != nil {
		llog.Warn().Msg("can't send output")

		return
	}

	task.Output <- res
}

func (c *collector) doQuery(llog zerolog.Logger, task *Task) *TaskResult {
	ctx := task.Ctx

	support.TracePrintf(ctx, "start query %q in %q", task.QueryName, task.DBName)

	result, err := c.loader.Query(ctx, task.Query, task.Params)
	if err != nil {
		metrics.IncProcessErrorsCnt("query")

		// When OnError is defined - generate metrics according to this template
		if task.Query.OnErrorTpl != nil {
			llog.Warn().Err(err).Msg("query error handled by on error template")

			if output, err := formatError(ctx, err, task.Query, c.cfg); err != nil {
				llog.Error().Err(err).Msg("format error result error")
			} else {
				llog.Debug().Bytes("output", output).Msg("result")

				return task.newResult(nil, output)
			}
		}

		return task.newResult(fmt.Errorf("query error: %w", err), nil)
	}

	llog.Debug().Msg("result received")
	metrics.ObserveQueryDuration(task.QueryName, task.DBName, result.Duration)

	output, err := formatResult(ctx, result, task.Query, c.cfg)
	if err != nil {
		metrics.IncProcessErrorsCnt("format")

		return task.newResult(fmt.Errorf("format error: %w", err), nil)
	}

	llog.Debug().Msg("result formatted")
	support.TracePrintf(ctx, "finished  query and formatting %q in %q", task.QueryName, task.DBName)

	return task.newResult(nil, output)
}

func (c *collector) stats() *db.DatabaseStats {
	return c.loader.Stats()
}
