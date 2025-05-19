package server

//
// query_handler.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/expfmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

const maxLockTime = time.Duration(20) * time.Minute

type lockInfo struct {
	ts  time.Time
	key string
}

func (l lockInfo) String() string {
	return fmt.Sprintf("%s on %s (%s ago)", l.key, l.ts, time.Since(l.ts))
}

type locker struct {
	runningQuery map[string]lockInfo
	sync.Mutex
}

func newLocker() locker {
	return locker{runningQuery: make(map[string]lockInfo)}
}

func (l *locker) tryLock(queryKey, reqID string) (string, bool) {
	l.Lock()
	defer l.Unlock()

	if li, ok := l.runningQuery[queryKey]; ok {
		if time.Since(li.ts) < maxLockTime {
			return li.String(), false
		}

		log.Logger.Warn().Str("reqID", li.key).Msgf("queryhandler: lock after maxLockTime, since %s", li.ts)
	}

	l.runningQuery[queryKey] = lockInfo{key: reqID, ts: time.Now()}

	return "", true
}

func (l *locker) unlock(queryKey string) {
	l.Lock()
	defer l.Unlock()

	delete(l.runningQuery, queryKey)
}

// queryHandler handle all request for metrics.
type queryHandler struct {
	cfg         *conf.Configuration
	resultCache *support.Cache[[]byte]
	// locker protect from running the same request twice
	locker    locker
	taskQueue TaskQueue
}

func newQueryHandler(c *conf.Configuration, cache *support.Cache[[]byte], taskQueue TaskQueue) *queryHandler {
	return &queryHandler{
		cfg:         c,
		locker:      newLocker(),
		resultCache: cache,
		taskQueue:   taskQueue,
	}
}

func (q *queryHandler) Handler() http.Handler {
	h := newLogMiddleware(promhttp.InstrumentHandlerDuration(metrics.NewReqDurationWrapper("query"), q), "query", false)
	h = support.NewTraceMiddleware("dbquery_exporter")(h)
	h = hlog.RequestIDHandler("req_id", "X-Request-Id")(h)
	h = hlog.NewHandler(log.Logger)(h)

	return h
}

// SetConfiguration update handler configuration.
func (q *queryHandler) UpdateConf(c *conf.Configuration) {
	q.cfg = c

	q.resultCache.Clear()
}

func (q *queryHandler) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), q.cfg.Global.RequestTimeout)
	defer cancel()

	logger := log.Ctx(ctx)
	requestID, _ := hlog.IDFromCtx(ctx)

	support.SetGoroutineLabels(ctx, "req_id", requestID.String(), "req", req.URL.String())

	// prevent to run the same request twice
	if locker, ok := q.locker.tryLock(req.URL.RawQuery, requestID.String()); !ok {
		logger.Warn().Msgf("query already in progress, started by %s", locker)
		http.Error(writer, "query in progress", http.StatusInternalServerError)
		support.TraceErrorf(ctx, "query locked by %s", locker)

		return
	}

	defer q.locker.unlock(req.URL.RawQuery)

	parameters, err := newRequestParams(req, q.cfg)
	if err != nil {
		logger.Info().Err(err).Msg("parse request parameters error")
		http.Error(writer, "invalid parameters", http.StatusBadRequest)
		support.TraceErrorf(ctx, "bad request")

		return
	}

	logger.Debug().Object("parameters", parameters).Msg("parsed parameters")

	dWriter := dataWriter{writer: writer}
	dWriter.writeHeaders()

	// cancelCh is used to cancel background / running queries
	cancelCh := make(chan struct{}, 1)
	defer close(cancelCh)

	support.TracePrintf(ctx, "start querying db")

	output := q.queryDatabases(ctx, parameters, &dWriter, cancelCh)
	defer close(output)

	support.TracePrintf(ctx, "start reading data, scheduled: %d", dWriter.scheduled)
	q.writeResult(ctx, &dWriter, output)

	logger.Debug().Int("written", dWriter.written).
		Err(ctx.Err()).
		Msg("queryhandler: all database queries finished")

	if dWriter.written == 0 {
		metrics.IncProcessErrorsCnt("bad_requests")
		http.Error(writer, "error", http.StatusBadRequest)
	}
}

func (q *queryHandler) getFromCache(query *conf.Query, dbName string, params map[string]any) ([]byte, bool) {
	if conf.Args.DisableCache || len(params) > 0 || query.CachingTime == 0 {
		return nil, false
	}

	queryKey := query.Name + "@" + dbName

	return q.resultCache.Get(queryKey)
}

func (q *queryHandler) putIntoCache(task *collectors.Task, data []byte) {
	query := task.Query

	// do not cache query with user params
	if conf.Args.DisableCache || query == nil || len(task.Params) > 0 || query.CachingTime == 0 {
		return
	}

	queryKey := query.Name + "@" + task.DBName

	q.resultCache.Put(queryKey, query.CachingTime, data)
}

func (q *queryHandler) validateOutput(output []byte) error {
	if conf.Args.ValidateOutput {
		var parser expfmt.TextParser
		if _, err := parser.TextToMetricFamilies(bytes.NewReader(output)); err != nil {
			return fmt.Errorf("validate result error: %w", err)
		}
	}

	return nil
}

func (q *queryHandler) queryDatabases(ctx context.Context, parameters *requestParameters,
	dWriter *dataWriter, cancelCh chan struct{},
) chan *collectors.TaskResult {
	logger := zerolog.Ctx(ctx)
	logger.Debug().Msg("queryhandler: database processing start")

	output := make(chan *collectors.TaskResult, len(parameters.dbNames)*len(parameters.queryNames))
	now := time.Now()
	reqID, _ := hlog.IDFromCtx(ctx)

	for dbName, query := range parameters.iter() {
		metrics.IncQueryTotalCnt(query.Name, dbName)

		if data, ok := q.getFromCache(query, dbName, parameters.extraParameters); ok {
			logger.Debug().Str("dbname", dbName).Str("query", query.Name).Msg("queryhandler: query result from cache")
			support.TracePrintf(ctx, "data from cache for %q from %q", query.Name, dbName)
			dWriter.write(ctx, data)

			continue
		}

		task := &collectors.Task{
			DBName:       dbName,
			QueryName:    query.Name,
			Params:       parameters.extraParameters,
			Output:       output,
			Query:        query,
			RequestStart: now,
			ReqID:        reqID.String(),
			CancelCh:     cancelCh,
		}

		logger.Debug().Object("task", task).Msg("queryhandler: schedule task")
		q.taskQueue.AddTask(ctx, task)
		support.TracePrintf(ctx, "scheduled %q to %q", query.Name, dbName)
		dWriter.incScheduled()
	}

	return output
}

func (q *queryHandler) writeResult(ctx context.Context, dWriter *dataWriter, inp <-chan *collectors.TaskResult) {
	logger := log.Ctx(ctx)
	logger.Debug().Msgf("queryhandler: write result start; waiting for %d results", dWriter.scheduled)

	for dWriter.scheduled > 0 {
		select {
		case res, ok := <-inp:
			if !ok {
				return
			}

			dWriter.scheduled--

			task := res.Task

			if res.Error != nil {
				logger.Warn().Err(res.Error).Object("task", task).Msg("queryhandler: processing query error")
				support.TracePrintf(ctx, "process query %q from %q: %v", task.QueryName, task.DBName, res.Error)
				dWriter.writeError(ctx, "# query "+res.Task.QueryName+" in "+res.Task.DBName+" processing error")

				continue
			}

			logger.Debug().Object("res", res).Msg("queryhandler: write result")
			support.TracePrintf(ctx, "write result %q from %q", task.QueryName, task.DBName)

			if err := q.validateOutput(res.Result); err != nil {
				logger.Warn().Err(err).Object("task", task).Msg("queryhandler: validate output error")
				support.TracePrintf(ctx, "validate result of query %q from %q: %v", task.QueryName, task.DBName, err)

				continue
			}

			dWriter.write(ctx, res.Result)
			q.putIntoCache(task, res.Result)

		case <-ctx.Done():
			err := ctx.Err()
			logger.Warn().Err(err).Msg("queryhandler: context cancelled")
			metrics.IncProcessErrorsCnt("cancel")
			support.TraceErrorf(ctx, "result error: %s", err)

			return
		}
	}
}
