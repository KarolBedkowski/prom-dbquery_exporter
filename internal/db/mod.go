package db

//
// mod.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
)

// DBTask is query to perform.
type DBTask struct {
	Ctx context.Context

	DBName    string
	QueryName string

	Query  *conf.Query
	Params map[string]string

	Output chan *DBTaskResult
}

func (d *DBTask) newResult(err error, result []byte) *DBTaskResult {
	return &DBTaskResult{
		Error:     err,
		Result:    result,
		DBName:    d.DBName,
		QueryName: d.QueryName,
		Query:     d.Query,
	}
}

// DBTaskResult is query result.
type DBTaskResult struct {
	Error  error
	Result []byte

	DBName    string
	QueryName string
	Query     *conf.Query
}

type dbLoader struct {
	dbName string

	loader     Loader
	tasks      chan *DBTask
	stopCh     chan struct{}
	numWorkers int
	cfg        *conf.Database
	log        zerolog.Logger
}

func newDBLoader(name string, cfg *conf.Database) (*dbLoader, error) {
	loader, err := GetLoader(cfg)
	if err != nil {
		return nil, fmt.Errorf("get loader error: %w", err)
	}

	dbl := &dbLoader{
		dbName: name,
		loader: loader,

		tasks:  make(chan *DBTask, 20),
		stopCh: make(chan struct{}, 1),
		cfg:    cfg,
		log:    log.Logger.With().Str("dbname", name).Logger(),
	}

	return dbl, nil
}

func (d *dbLoader) start() error {
	d.numWorkers = 1
	if d.cfg.Pool != nil {
		d.numWorkers = d.cfg.Pool.MaxConnections
	}

	d.log.Debug().Msgf("start %d workers", d.numWorkers)

	for i := 0; i < d.numWorkers; i++ {
		go d.worker(i)
	}

	return nil
}

func (d *dbLoader) stop() error {
	// TODO
	return nil
}

func (d *dbLoader) worker(idx int) {
	wlog := d.log.With().Int("worker_idx", idx).Logger()

	for {
		select {
		case <-d.stopCh:
			wlog.Debug().Msg("stop worker")
			return

		case task := <-d.tasks:
			wlog.Debug().Interface("task", task).Msg("handle task")

			select {
			case <-task.Ctx.Done():
				wlog.Info().Msg("task cancelled")
				continue
			default:
				start := time.Now()

				result, err := d.loader.Query(task.Ctx, task.Query, task.Params)
				if err != nil {
					metrics.IncProcessErrorsCnt("query")
					wlog.Error().Err(err).Msg("query error")
					task.Output <- task.newResult(fmt.Errorf("query error: %w", err), nil)
					continue
				}

				wlog.Debug().Interface("task", task).Msg("result received")

				output, err := FormatResult(task.Ctx, result, task.Query, d.cfg)
				if err != nil {
					metrics.IncProcessErrorsCnt("format")
					task.Output <- task.newResult(fmt.Errorf("format error: %w", err), nil)
					continue
				}

				wlog.Debug().Interface("task", task).Msg("result formatted")
				metrics.ObserveQueryDuration(task.QueryName, task.DBName,
					float64(time.Now().Sub(start)))

				task.Output <- task.newResult(nil, output)
			}
		}
	}
}

// Databases is collection of all configured databases.
type Databases struct {
	sync.Mutex

	cfg *conf.Configuration
	dbs map[string]*dbLoader
}

// NewDatabases create new Databases object.
func NewDatabases(cfg *conf.Configuration) *Databases {
	return &Databases{
		dbs: make(map[string]*dbLoader),
		cfg: cfg,
	}
}

// PutTask schedule new task.
func (d *Databases) PutTask(task *DBTask) error {
	d.Lock()
	defer d.Unlock()

	dbName := task.DBName

	dl, ok := d.dbs[dbName]
	if !ok {
		dconf, ok := d.cfg.Database[dbName]
		if !ok {
			return fmt.Errorf("unknown database '%s'", dbName)
		}

		var err error
		dl, err = newDBLoader(task.DBName, dconf)
		if err != nil {
			return fmt.Errorf("create dbloader error: %w", err)
		}

		if err := dl.start(); err != nil {
			if errs := dl.stop(); errs != nil {
				err = errors.Join(err, errs)
			}
			return fmt.Errorf("start dbloader error: %w", err)
		}

		d.dbs[task.DBName] = dl
	}

	dl.tasks <- task

	return nil
}
