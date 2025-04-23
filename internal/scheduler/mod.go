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
	nextRun   time.Time
	job       conf.Job
	isRunning bool
	runID     int
}

type scheduledJobResult struct {
	jobIdx   int
	runID    int
	success  bool
	duration time.Duration
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
	parallel bool
}

// NewScheduler create new scheduler that use `cache` and initial `cfg` configuration.
func NewScheduler(cache *support.Cache[[]byte], cfg *conf.Configuration, parallel bool) *Scheduler {
	prometheus.MustRegister(scheduledTasksCnt)

	scheduler := &Scheduler{
		cfg:      cfg,
		cache:    cache,
		stop:     make(chan struct{}),
		log:      log.Logger.With().Str("module", "scheduler").Logger(),
		parallel: parallel,
	}
	scheduler.ReloadConf(cfg)

	return scheduler
}

func (s *Scheduler) handleJob(j *scheduledJob) bool {
	queryName := j.job.Query
	dbName := j.job.Database

	ctx := context.Background()

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

func (s *Scheduler) runJobBg(jobIdx int, runID int, job *scheduledJob, resultsCh chan scheduledJobResult) {
	res := scheduledJobResult{
		jobIdx: jobIdx,
		runID:  runID,
	}
	startTS := time.Now()
	res.success = s.handleJob(job)
	res.duration = time.Since(startTS)
	resultsCh <- res
}

func (s *Scheduler) runJob(jobIdx int, runID int, job *scheduledJob) scheduledJobResult {
	res := scheduledJobResult{
		jobIdx: jobIdx,
		runID:  runID,
	}
	startTS := time.Now()
	res.success = s.handleJob(job)
	res.duration = time.Since(startTS)
	return res
}

func (s *Scheduler) handleResult(res scheduledJobResult) {
	if res.jobIdx >= len(s.jobs) {
		s.log.Warn().Msgf("invalid result for job id %d", res.jobIdx)

		return
	}

	job := s.jobs[res.jobIdx]
	if job.runID != res.runID {
		s.log.Warn().Msgf("invalid runID for job id %d; expected %d, got %d", res.jobIdx, job.runID, res.runID)

		return
	}

	interval := job.job.Interval
	if interval < res.duration {
		s.log.Warn().Dur("duration", res.duration).Interface("task", job.job).
			Msgf("scheduled task run longer than defined interval (%s)", interval)
	}

	if !res.success {
		interval *= 2
	}

	job.nextRun = time.Now().Add(interval)
	job.isRunning = false
}

// Run scheduler process.
func (s *Scheduler) Run() error {
	s.log.Debug().Msgf("starting scheduler; parallel=%v", s.parallel)

	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	group := sync.WaitGroup{}

	results := make(chan scheduledJobResult)
	defer close(results)

	lastRunID := 0

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
			s.log.Debug().Msg("scheduler stopping")
			group.Wait()
			s.log.Debug().Msg("scheduler stopped")

			return nil

		case res := <-results:
			s.handleResult(res)
			group.Done()

		case <-timer.C:
			for jobIdx, job := range s.jobs {
				if !job.isRunning && time.Now().After(job.nextRun) {
					scheduledTasksCnt.Inc()

					lastRunID++
					job.isRunning = true
					job.runID = lastRunID

					if s.parallel {
						group.Add(1)

						go s.runJobBg(jobIdx, lastRunID, job, results)
					} else {
						s.handleResult(s.runJob(jobIdx, lastRunID, job))
					}
				}
			}
		}
	}
}

// Close scheduler.
func (s *Scheduler) Close(err error) {
	_ = err

	log.Logger.Debug().Msg("stopping scheduler")
	close(s.stop)
}

// ReloadConf load new configuration.
func (s *Scheduler) ReloadConf(cfg *conf.Configuration) {
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
		nextRun := time.Now().Add(time.Duration(idx*7) * time.Second) //noqa:mnd

		jobs = append(jobs, &scheduledJob{job: job, nextRun: nextRun, isRunning: false})
	}

	s.jobs = jobs
	s.cfg = cfg
}
