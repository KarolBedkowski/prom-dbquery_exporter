package scheduler

//
// mod.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/support"
)

type scheduledJob struct {
	nextRun time.Time
	job     conf.Job
}

// Scheduler is background process that load configured data into cache in some intervals.
type Scheduler struct {
	log   zerolog.Logger
	cfg   *conf.Configuration
	cache *support.Cache[[]byte]
	stop  chan struct{}
	jobs  []*scheduledJob
}

// NewScheduler create new scheduler that use `cache` and initial `cfg` configuration.
func NewScheduler(cache *support.Cache[[]byte], cfg *conf.Configuration) *Scheduler {
	scheduler := &Scheduler{
		cfg:   cfg,
		cache: cache,
		stop:  make(chan struct{}),
		log:   log.Logger.With().Str("module", "scheduler").Logger(),
	}
	scheduler.ReloadConf(cfg)

	return scheduler
}

func (s *Scheduler) handleJob(j *scheduledJob) bool {
	queryName := j.job.Query
	dbName := j.job.Database
	timeout := j.job.Interval

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logger := s.log.With().Str("dbname", dbName).Str("query", queryName).Logger()
	logger.Debug().Msg("job processing start")

	query := (s.cfg.Query)[queryName]

	output := make(chan *collectors.TaskResult, 1)
	defer close(output)

	task := collectors.Task{
		Ctx:          logger.WithContext(ctx),
		DBName:       dbName,
		QueryName:    queryName,
		Params:       nil,
		Output:       output,
		Query:        query,
		RequestStart: time.Now(),
	}

	logger.Debug().Object("task", &task).Msg("schedule task")

	if err := collectors.CollectorsPool.ScheduleTask(&task); err != nil {
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

// Run scheduler process.
func (s *Scheduler) Run() error {
	log.Logger.Debug().Msg("starting scheduler")

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
			log.Logger.Debug().Msg("scheduler stopped")

			return nil

		case <-timer.C:
			for _, job := range s.jobs {
				if time.Now().Before(job.nextRun) {
					continue
				}

				interval := job.job.Interval
				if !s.handleJob(job) {
					interval *= 2
				}

				job.nextRun = time.Now().Add(interval)
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

	for _, job := range cfg.Jobs {
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

		jobs = append(jobs, &scheduledJob{
			job:     job,
			nextRun: time.Now(),
		})
	}

	s.jobs = jobs
	s.cfg = cfg
}
