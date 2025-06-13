package scheduler

//
// mod.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/trace"
	"golang.org/x/sync/errgroup"
	"prom-dbquery_exporter.app/internal/cache"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/debug"
	"prom-dbquery_exporter.app/internal/metrics"
)

var (
	scheduledTasksCnt = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "",
			Name:        "task_scheduled_total",
			Help:        "Total number of scheduled tasks",
			ConstLabels: nil,
		},
	)
	scheduledTasksSuccess = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "",
			Name:        "task_success_total",
			Help:        "Total number of tasks finished with success",
			ConstLabels: nil,
		},
	)
	scheduledTasksFailed = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "",
			Name:        "task_failed_total",
			Help:        "Total number of tasks finished with error",
			ConstLabels: nil,
		},
	)
)

func init() {
	prometheus.MustRegister(scheduledTasksCnt)
	prometheus.MustRegister(scheduledTasksSuccess)
	prometheus.MustRegister(scheduledTasksFailed)
}

// -----------------------------------------------------------------------------

type scheduledTask struct {
	// nextRun is time when task should be run when scheduler run in serial mode
	nextRun time.Time
	job     conf.Job
}

// MarshalZerologObject implements LogObjectMarshaler.
func (s *scheduledTask) MarshalZerologObject(event *zerolog.Event) {
	event.Object("job", &s.job).Time("next_run", s.nextRun)
}

// -----------------------------------------------------------------------------

type TaskQueue interface {
	AddTask(ctx context.Context, task *collectors.Task)
}

// -----------------------------------------------------------------------------
// Scheduler is background process that load configured data into cache in some intervals.
type Scheduler struct {
	log zerolog.Logger

	tasksQueue TaskQueue

	cfg      *conf.Configuration
	cache    *cache.Cache[[]byte]
	newCfgCh chan *conf.Configuration
	tasks    []*scheduledTask
}

// New create new scheduler that use `cache` and initial `cfg` configuration.
func New(cfg *conf.Configuration, cache *cache.Cache[[]byte],
	tasksQueue TaskQueue, newCfgCh chan *conf.Configuration,
) *Scheduler {
	scheduler := &Scheduler{
		cfg:        cfg,
		cache:      cache,
		log:        log.Logger.With().Str("module", "scheduler").Logger(),
		newCfgCh:   newCfgCh,
		tasksQueue: tasksQueue,
		tasks:      nil,
	}

	scheduler.updateConfig(cfg)

	return scheduler
}

// Run scheduler process that get data for all defined jobs sequential.
func (s *Scheduler) Run(ctx context.Context, parallel bool) error {
	if parallel {
		return s.runParallel(ctx)
	}

	return s.runSerial(ctx)
}

// Close scheduler.
func (s *Scheduler) Close(err error) {
	_ = err

	s.log.Debug().Msg("scheduler: stopping")
}

// RunSerial scheduler process that get data for all defined jobs sequential.
func (s *Scheduler) runSerial(ctx context.Context) error {
	s.log.Debug().Msgf("scheduler: starting serial scheduler")
	s.rescheduleTask()

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	for {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}

		timer.Reset(time.Second)

		select {
		case <-ctx.Done():
			s.log.Debug().Msg("scheduler: stopped")

			return nil

		case cfg, ok := <-s.newCfgCh:
			if ok {
				s.updateConfig(cfg)
				s.rescheduleTask()
			}

		case <-timer.C:
			s.runOverdueJobs(ctx)
		}
	}
}

// RunParallel run scheduler in parallel mode that spawn goroutine for each defined job.
func (s *Scheduler) runParallel(ctx context.Context) error {
	s.log.Debug().Msg("scheduler: starting parallel scheduler")

	for {
		lctx, cancel := context.WithCancel(ctx)
		group, lctx := errgroup.WithContext(lctx)

		// spawn background workers
		for _, job := range s.tasks {
			group.Go(func() error {
				return s.runJobInLoop(lctx, job.job)
			})
		}

		s.log.Debug().Msgf("scheduler: all job started")

		select {
		case <-ctx.Done():
			s.log.Debug().Msg("scheduler: stopping")
			cancel()

			if err := group.Wait(); err != nil {
				s.log.Error().Err(err).Msg("scheduler: wait for errors finished error")
			}

			s.log.Debug().Msg("scheduler: stopped")

			return nil

		case cfg, ok := <-s.newCfgCh:
			if !ok {
				cancel()

				return nil
			}

			s.log.Debug().Msg("scheduler: stopping workers for config reload")
			cancel()

			if err := group.Wait(); err != nil {
				s.log.Error().Err(err).Msg("scheduler: wait for finish error")
			}

			s.log.Debug().Msg("scheduler: all workers stopped")
			s.updateConfig(cfg)

			continue
		}
	}
}

// handleJobWithMetrics wrap handleJob to gather some metrics and log errors.
func (s *Scheduler) handleJobWithMetrics(ctx context.Context, job conf.Job) {
	startTS := time.Now()

	scheduledTasksCnt.Inc()

	llog := s.log.With().Str("database", job.Database).Str("query", job.Query).Int("job_idx", job.Idx).Logger()
	llog.Debug().Msg("scheduler: job processing start")

	ctx = llog.WithContext(ctx)

	if debug.TraceMaxEvents > 0 {
		tr := trace.New("dbquery_exporter.scheduler", "scheduler: "+job.Database+"/"+job.Query)
		tr.SetMaxEvents(debug.TraceMaxEvents)
		defer tr.Finish()

		ctx = trace.NewContext(ctx, tr)
	}

	if err := s.handleJob(ctx, job); err != nil {
		scheduledTasksFailed.Inc()

		debug.TraceErrorf(ctx, "scheduled %q to %q error: %v", job.Query, job.Database, err)
		llog.Error().Err(err).Msg("scheduler: error execute job")
	} else {
		scheduledTasksSuccess.Inc()
		llog.Debug().Msg("scheduler: execute job finished")
	}

	if duration := time.Since(startTS); job.Interval < duration {
		llog.Warn().Msgf("scheduler: task run longer than defined interval (%s): %s", job.Interval, duration)
	}
}

// handleJob request data and wait for response. On success put result into cache.
func (s *Scheduler) handleJob(ctx context.Context, j conf.Job) error {
	queryName := j.Query
	dbname := j.Database
	query := (s.cfg.Query)[queryName]

	if query == nil {
		return fmt.Errorf("fatal, empty query %s", queryName) //nolint:err113
	}

	output := make(chan *collectors.TaskResult, 1)
	defer close(output)

	task, cancel := collectors.NewTask(dbname, query, output).WithNewReqID().MarkScheduled().WithCancel()
	defer cancel()

	s.tasksQueue.AddTask(ctx, task)
	debug.TracePrintf(ctx, "scheduled %q to %q", queryName, dbname)

	select {
	case <-ctx.Done():
		return fmt.Errorf("processing error: %w", ctx.Err())

	case res, ok := <-output:
		if ok {
			if res.Error != nil {
				return fmt.Errorf("processing error: %w", res.Error)
			}

			s.cache.Put(query.Name+"@"+dbname, query.CachingTime, res.Result)
		}
	}

	return nil
}

func (s *Scheduler) runOverdueJobs(ctx context.Context) {
	for _, job := range s.tasks {
		if time.Now().After(job.nextRun) {
			s.handleJobWithMetrics(ctx, job.job)
			job.nextRun = time.Now().Add(job.job.Interval)
		}
	}
}

// / runJobInLoop run in goroutine and get data from data for one job in defined intervals.
func (s *Scheduler) runJobInLoop(ctx context.Context, job conf.Job) error {
	llog := s.log.With().Int("job", job.Idx).Logger()
	llog.Debug().Object("job", &job).Msg("scheduler: starting worker")

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	// add some offset to prevent all tasks start in the same time
	interval := job.Interval + time.Duration(job.Idx*7)*time.Second //nolint:mnd

	for {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}

		timer.Reset(interval)

		select {
		case <-ctx.Done():
			llog.Debug().Msg("scheduler: worker stopped")

			return nil

		case <-timer.C:
			s.handleJobWithMetrics(ctx, job)
			interval = job.Interval
		}
	}
}

// updateConfig load new configuration (only valid entries).
func (s *Scheduler) updateConfig(cfg *conf.Configuration) {
	tasks := make([]*scheduledTask, 0, len(cfg.Jobs))

	for _, job := range cfg.Jobs {
		if job.IsValid {
			tasks = append(tasks, &scheduledTask{job: *job, nextRun: time.Time{}})
		}
	}

	s.tasks = tasks
	s.cfg = cfg

	s.log.Debug().Msgf("scheduler: configuration updated; tasks: %d", len(s.tasks))
}

// rescheduleTask set next run time for all tasks.
func (s *Scheduler) rescheduleTask() {
	// add some offset to prevent all tasks start in the same time
	for _, task := range s.tasks {
		task.nextRun = time.Now().Add(time.Duration(task.job.Idx*7) * time.Second) //nolint:mnd
	}
}
