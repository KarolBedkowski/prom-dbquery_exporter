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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"golang.org/x/net/trace"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

type scheduledTask struct {
	// nextRun is time when task should be run when scheduler run in serial mode
	nextRun time.Time
	job     conf.Job
}

// MarshalZerologObject implements LogObjectMarshaler.
func (s *scheduledTask) MarshalZerologObject(event *zerolog.Event) {
	event.Object("job", &s.job).
		Time("next_run", s.nextRun)
}

// Scheduler is background process that load configured data into cache in some intervals.
type Scheduler struct {
	log      zerolog.Logger
	cfg      *conf.Configuration
	cache    *support.Cache[[]byte]
	stop     chan struct{}
	tasks    []*scheduledTask
	newCfgCh chan *conf.Configuration

	scheduledTasksCnt     prometheus.Counter
	scheduledTasksSuccess prometheus.Counter
	scheduledTasksFailed  prometheus.Counter
}

// NewScheduler create new scheduler that use `cache` and initial `cfg` configuration.
func NewScheduler(cache *support.Cache[[]byte], cfg *conf.Configuration) *Scheduler {
	scheduler := &Scheduler{
		cfg:      cfg,
		cache:    cache,
		stop:     make(chan struct{}),
		log:      log.Logger.With().Str("module", "scheduler").Logger(),
		newCfgCh: make(chan *conf.Configuration, 1),

		scheduledTasksCnt: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: metrics.MetricsNamespace,
				Name:      "task_scheduled_total",
				Help:      "Total number of scheduled tasks",
			},
		),
		scheduledTasksSuccess: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: metrics.MetricsNamespace,
				Name:      "task_success_total",
				Help:      "Total number of tasks finished with success",
			},
		),
		scheduledTasksFailed: prometheus.NewCounter(
			prometheus.CounterOpts{
				Namespace: metrics.MetricsNamespace,
				Name:      "task_failed_total",
				Help:      "Total number of tasks finished with error",
			},
		),
	}

	prometheus.MustRegister(scheduler.scheduledTasksCnt)
	prometheus.MustRegister(scheduler.scheduledTasksSuccess)
	prometheus.MustRegister(scheduler.scheduledTasksFailed)

	scheduler.ReloadConf(cfg)

	return scheduler
}

func (s *Scheduler) handleJobWithMetrics(ctx context.Context, job conf.Job) {
	startTS := time.Now()

	s.scheduledTasksCnt.Inc()

	llog := s.log.With().Str("dbname", job.Database).Str("query", job.Query).Int("job_idx", job.Idx).Logger()
	log.Debug().Msg("scheduler: job processing start")

	ctx = llog.WithContext(ctx)

	if support.TraceMaxEvents > 0 {
		tr := trace.New("dbquery_exporter.scheduler", "scheduler: "+job.Database+"/"+job.Query)
		tr.SetMaxEvents(support.TraceMaxEvents)
		defer tr.Finish()

		ctx = trace.NewContext(ctx, tr)
	}

	if err := s.handleJob(ctx, job); err != nil {
		s.scheduledTasksFailed.Inc()

		support.TraceErrorf(ctx, "scheduled %q to %q error: %v", job.Query, job.Database, err)
		llog.Error().Err(err).Msg("scheduler: error execute job")
	} else {
		s.scheduledTasksSuccess.Inc()
		llog.Debug().Msg("scheduler: execute job finished")
	}

	duration := time.Since(startTS)
	if job.Interval < duration {
		llog.Warn().Dur("duration", duration).
			Msgf("scheduler: task run longer than defined interval (%s)", job.Interval)
	}
}

func (s *Scheduler) handleJob(ctx context.Context, j conf.Job) error {
	queryName := j.Query
	dbName := j.Database
	query := (s.cfg.Query)[queryName]

	output := make(chan *collectors.TaskResult, 1)
	defer close(output)

	task := &collectors.Task{
		Ctx:          ctx,
		DBName:       dbName,
		QueryName:    queryName,
		Params:       nil,
		Output:       output,
		Query:        query,
		RequestStart: time.Now(),

		IsScheduledJob: true,
	}

	if err := collectors.CollectorsPool.ScheduleTask(task); err != nil {
		return fmt.Errorf("start task error: %w", err)
	}

	support.TracePrintf(ctx, "scheduled %q to %q", queryName, dbName)

	select {
	case <-ctx.Done():
		err := ctx.Err()

		return fmt.Errorf("processing error: %w", err)

	case res := <-output:
		if res.Error != nil {
			return fmt.Errorf("processing error: %w", res.Error)
		}

		s.cache.Put(query.Name+"@"+dbName, query.CachingTime, res.Result)
	}

	return nil
}

func (s *Scheduler) runJobInLoop(ctx context.Context, job conf.Job) {
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

			return

		case <-timer.C:
			s.handleJobWithMetrics(ctx, job)
			interval = job.Interval
		}
	}
}

// Run scheduler process.
func (s *Scheduler) Run() error {
	s.log.Debug().Msgf("scheduler: starting serial scheduler")

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
		case <-s.stop:
			s.log.Debug().Msg("scheduler: stopped")

			return nil

		case cfg := <-s.newCfgCh:
			s.updateConfig(cfg)

		case <-timer.C:
			for _, job := range s.tasks {
				if time.Now().After(job.nextRun) {
					s.handleJobWithMetrics(context.Background(), job.job)
					job.nextRun = time.Now().Add(job.job.Interval)
				}
			}
		}
	}
}

// RunParallel scheduler process.
func (s *Scheduler) RunParallel() error {
	s.log.Debug().Msgf("scheduler: starting parallel scheduler")

	group := sync.WaitGroup{}

	for {
		ctx, cancel := context.WithCancel(context.Background())

		// spawn background workers
		for _, job := range s.tasks {
			group.Add(1)

			go func() {
				s.runJobInLoop(ctx, job.job)
				group.Done()
			}()
		}

		select {
		case <-s.stop:
			s.log.Debug().Msg("scheduler: stopping")
			cancel()
			group.Wait()
			s.log.Debug().Msg("scheduler: stopped")

			return nil

		case cfg := <-s.newCfgCh:
			s.log.Debug().Msg("scheduler: stopping workers for config reload")
			cancel()
			group.Wait()
			s.log.Debug().Msg("scheduler: all workers stopped")
			s.updateConfig(cfg)

			continue
		}
	}
}

// Close scheduler.
func (s *Scheduler) Close(err error) {
	_ = err

	log.Logger.Debug().Msg("scheduler: stopping")
	close(s.stop)
	close(s.newCfgCh)
}

// ReloadConf load new configuration.
func (s *Scheduler) ReloadConf(cfg *conf.Configuration) {
	s.newCfgCh <- cfg
}

func (s *Scheduler) updateConfig(cfg *conf.Configuration) {
	tasks := make([]*scheduledTask, 0, len(cfg.Jobs))

	for _, job := range cfg.Jobs {
		if job.Interval.Seconds() <= 1 {
			continue
		}

		if _, ok := (s.cfg.Query)[job.Query]; !ok {
			s.log.Error().Msgf("scheduler: reload cfg error: unknown query: %s", job.Query)

			continue
		}

		if _, ok := s.cfg.Database[job.Database]; !ok {
			s.log.Error().Msgf("scheduler: reload cfg error: unknown database: %s", job.Database)

			continue
		}

		// add some offset to prevent all tasks start in the same time
		nextRun := time.Now().Add(time.Duration(job.Idx*7) * time.Second) //nolint:mnd

		tasks = append(tasks, &scheduledTask{job: job, nextRun: nextRun})
	}

	s.tasks = tasks
	s.cfg = cfg

	s.log.Info().Msgf("scheduler: configuration updated; tasks: %d", len(s.tasks))
}
