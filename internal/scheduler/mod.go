package scheduler

//
// mod.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
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

type scheduledJob struct {
	nextRun time.Time
	job     conf.Job
}

var scheduledTasksCnt = prometheus.NewCounter(
	prometheus.CounterOpts{
		Namespace: metrics.MetricsNamespace,
		Name:      "task_scheduled_total",
		Help:      "Total number of scheduled tasks",
	},
)

// Scheduler is background process that load configured data into cache in some intervals.
type Scheduler struct {
	log      zerolog.Logger
	cfg      *conf.Configuration
	cache    *support.Cache[[]byte]
	stop     chan struct{}
	jobs     []*scheduledJob
	newCfgCh chan *conf.Configuration
}

// NewScheduler create new scheduler that use `cache` and initial `cfg` configuration.
func NewScheduler(cache *support.Cache[[]byte], cfg *conf.Configuration) *Scheduler {
	prometheus.MustRegister(scheduledTasksCnt)

	scheduler := &Scheduler{
		cfg:      cfg,
		cache:    cache,
		stop:     make(chan struct{}),
		log:      log.Logger.With().Str("module", "scheduler").Logger(),
		newCfgCh: make(chan *conf.Configuration, 1),
	}
	scheduler.ReloadConf(cfg)

	return scheduler
}

func (s *Scheduler) handleJob(ctx context.Context, j *scheduledJob) bool {
	queryName := j.job.Query
	dbName := j.job.Database

	if support.TraceMaxEvents > 0 {
		tr := trace.New("dbquery_exporter.scheduler", "scheduler: "+dbName+"/"+queryName)
		tr.SetMaxEvents(support.TraceMaxEvents)
		defer tr.Finish()

		ctx = trace.NewContext(ctx, tr)
	}

	logger := s.log.With().Str("dbname", dbName).Str("query", queryName).Logger()
	logger.Debug().Msg("job processing start")

	query := (s.cfg.Query)[queryName]

	output := make(chan *collectors.TaskResult, 1)
	defer close(output)

	task := &collectors.Task{
		Ctx:          logger.WithContext(ctx),
		DBName:       dbName,
		QueryName:    queryName,
		Params:       nil,
		Output:       output,
		Query:        query,
		RequestStart: time.Now(),

		IsScheduledJob: true,
	}

	logger.Debug().Object("task", task).Msg("schedule task")

	if err := collectors.CollectorsPool.ScheduleTask(task); err != nil {
		support.TraceErrorf(ctx, "scheduled %q to %q error: %v", queryName, dbName, err)
		logger.Error().Err(err).Str("dbname", dbName).Str("query", queryName).
			Msg("start task error")

		return false
	}

	support.TracePrintf(ctx, "scheduled %q to %q", queryName, dbName)

	select {
	case <-ctx.Done():
		err := ctx.Err()
		logger.Error().Err(err).Object("task", task).Msg("processing query error")

		return false

	case res := <-output:
		if res.Error != nil {
			logger.Error().Err(res.Error).Object("task", task).Msg("processing query error")

			return false
		}

		queryKey := query.Name + "@" + dbName
		s.cache.Put(queryKey, query.CachingTime, res.Result)
	}

	return true
}

func (s *Scheduler) runJobBg(ctx context.Context, jobIdx int, job scheduledJob) {
	llog := s.log.With().Int("job", jobIdx).Logger()
	llog.Debug().Interface("job", job.job).Msg("starting bg job")

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	interval := job.job.Interval

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
			llog.Debug().Msg("bg job stopped")

			return

		case <-timer.C:
			scheduledTasksCnt.Inc()

			startTS := time.Now()
			success := s.handleJob(ctx, &job)
			duration := time.Since(startTS)

			if job.job.Interval < duration {
				s.log.Warn().Dur("duration", duration).Interface("task", job.job).
					Msgf("scheduled task run longer than defined interval (%s)", job.job.Interval)
			}

			if !success {
				interval = 2 * job.job.Interval
			}
		}
	}
}

func (s *Scheduler) runJob(job *scheduledJob) {
	startTS := time.Now()
	success := s.handleJob(context.Background(), job)
	duration := time.Since(startTS)

	interval := job.job.Interval
	if interval < duration {
		s.log.Warn().Dur("duration", duration).Interface("task", job.job).
			Msgf("scheduled task run longer than defined interval (%s)", interval)
	}

	if !success {
		interval *= 2
	}

	job.nextRun = time.Now().Add(interval)
}

// Run scheduler process.
func (s *Scheduler) Run() error {
	s.log.Debug().Msgf("starting serial scheduler")

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
			s.log.Debug().Msg("scheduler stopped")

			return nil

		case cfg := <-s.newCfgCh:
			s.updateConfig(cfg)

		case <-timer.C:
			for _, job := range s.jobs {
				if time.Now().After(job.nextRun) {
					scheduledTasksCnt.Inc()

					s.runJob(job)
				}
			}
		}
	}
}

// RunParallel scheduler process.
func (s *Scheduler) RunParallel() error {
	s.log.Debug().Msgf("starting parallel scheduler")

	group := sync.WaitGroup{}

	for {
		ctx, cancel := context.WithCancel(context.Background())

		// spawn background workers
		for jobIdx, job := range s.jobs {
			group.Add(1)

			go func() {
				s.runJobBg(ctx, jobIdx, *job)
				group.Done()
			}()
		}

		select {
		case <-s.stop:
			s.log.Debug().Msg("scheduler stopping")
			cancel()
			group.Wait()
			s.log.Debug().Msg("scheduler stopped")

			return nil

		case cfg := <-s.newCfgCh:
			s.log.Debug().Msg("stopping background jobs for config reload")
			cancel()
			group.Wait()
			s.updateConfig(cfg)

			continue
		}
	}
}

// Close scheduler.
func (s *Scheduler) Close(err error) {
	_ = err

	log.Logger.Debug().Msg("stopping scheduler")
	close(s.stop)
	close(s.newCfgCh)
}

// ReloadConf load new configuration.
func (s *Scheduler) ReloadConf(cfg *conf.Configuration) {
	s.newCfgCh <- cfg
}

func (s *Scheduler) updateConfig(cfg *conf.Configuration) {
	jobs := make([]*scheduledJob, 0, len(cfg.Jobs))

	for idx, job := range cfg.Jobs {
		if job.Interval.Seconds() <= 1 {
			continue
		}

		if _, ok := (s.cfg.Query)[job.Query]; !ok {
			s.log.Error().Msgf("reload cfg error: unknown query: %s", job.Query)

			continue
		}

		if _, ok := s.cfg.Database[job.Database]; !ok {
			s.log.Error().Msgf("reload cfg error: unknown database: %s", job.Database)

			continue
		}

		// add some offset to prevent all tasks start in the same time
		nextRun := time.Now().Add(time.Duration(idx*7) * time.Second) //nolint:mnd

		jobs = append(jobs, &scheduledJob{job: job, nextRun: nextRun})
	}

	s.jobs = jobs
	s.cfg = cfg

	s.log.Debug().Msg("configuration update")
}
