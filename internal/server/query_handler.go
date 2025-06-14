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
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/expfmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/cache"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/debug"
	"prom-dbquery_exporter.app/internal/metrics"
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

	mu sync.Mutex
}

func newLocker() locker {
	return locker{runningQuery: make(map[string]lockInfo)} //nolint:exhaustruct
}

func (l *locker) tryLock(queryKey, reqID string) (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if li, ok := l.runningQuery[queryKey]; ok {
		if time.Since(li.ts) < maxLockTime {
			return li.String(), false
		}

		log.Logger.Warn().Str("req_id", li.key).Msgf("queryhandler: lock after maxLockTime, since %s", li.ts)
	}

	l.runningQuery[queryKey] = lockInfo{key: reqID, ts: time.Now()}

	return "", true
}

func (l *locker) unlock(queryKey string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.runningQuery, queryKey)
}

// ----------------------------------------------------------------------------------

// queryHandler handle all request for metrics.
type queryHandler struct {
	taskQueue TaskQueue

	cfg         *conf.Configuration
	resultCache *cache.Cache[[]byte]

	// locker protect from running the same request twice
	locker locker
}

func newQueryHandler(c *conf.Configuration, cache *cache.Cache[[]byte], taskQueue TaskQueue) *queryHandler {
	return &queryHandler{
		cfg:         c,
		locker:      newLocker(),
		resultCache: cache,
		taskQueue:   taskQueue,
	}
}

func (q *queryHandler) Handler() http.Handler {
	var handler http.Handler = q
	// 5. log request traces
	handler = debug.NewTraceMiddleware("dbquery_exporter")(handler)
	// 4. update logger, log req, response
	handler = newLogMiddleware(handler)
	// 3. add logger to ctx
	handler = hlog.NewHandler(log.Logger)(handler)
	// 2. set request_handler
	handler = hlog.RequestIDHandler("req_id", "X-Request-Id")(handler)
	// 1. metrics
	handler = promhttp.InstrumentHandlerInFlight(newReqInflightWrapper("query"), handler)
	handler = promhttp.InstrumentHandlerDuration(newReqDurationWrapper("query"), handler)

	// 0. compression
	if conf.Args.EnableCompression() {
		handler = newGzipHandler(handler)
	}

	if q.cfg.Global.MaxRequestInFlight > 0 {
		handler = newLimitRequestInFlightMW(handler, q.cfg.Global.MaxRequestInFlight)
	}

	return handler
}

func (q *queryHandler) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), q.cfg.Global.RequestTimeout)
	defer cancel()

	requestID, _ := hlog.IDFromCtx(ctx)
	logger := log.Ctx(ctx)

	debug.SetGoroutineLabels(ctx, "req_id", requestID.String(), "req", req.URL.String())

	// prevent to run the same request twice
	if locker, ok := q.locker.tryLock(req.URL.RawQuery, requestID.String()); !ok {
		logger.Warn().Msgf("query already in progress, started by %s", locker)
		http.Error(writer, "query in progress", http.StatusInternalServerError)
		debug.TraceErrorf(ctx, "query locked by %s", locker)

		return
	}

	defer q.locker.unlock(req.URL.RawQuery)

	parameters, err := newRequestParams(req, q.cfg)
	if err != nil {
		logger.Info().Err(err).Msg("parse request parameters error")
		http.Error(writer, "invalid parameters", http.StatusBadRequest)
		debug.TraceErrorf(ctx, "bad request")

		return
	}

	logger.Debug().Object("parameters", parameters).Msg("parsed parameters")

	dWriter := responseWriter{writer: writer, written: 0, scheduled: 0}
	dWriter.writeHeaders()

	// cancelCh is used to cancel background / running queries
	cancelCh := make(chan struct{}, 1)
	defer close(cancelCh)

	debug.TracePrintf(ctx, "start querying db")

	output := q.queryDatabases(ctx, parameters, &dWriter, cancelCh)
	defer close(output)

	debug.TracePrintf(ctx, "start reading data, scheduled: %d", dWriter.scheduled)
	q.gatherResults(ctx, &dWriter, output)

	logger.Debug().Int("written", dWriter.written).
		Err(ctx.Err()).
		Msg("queryhandler: all database queries finished")

	if dWriter.written == 0 {
		metrics.IncErrorsCnt(metrics.ErrCategoryBadRequestError)
		http.Error(writer, "error", http.StatusBadRequest)
	}
}

func (q *queryHandler) updateConf(c *conf.Configuration) {
	q.cfg = c

	q.resultCache.Clear()
}

func (q *queryHandler) getFromCache(query *conf.Query, dbname string, params []string) ([]byte, bool) {
	if conf.Args.DisableCache || len(params) > 0 || query.CachingTime == 0 {
		return nil, false
	}

	queryKey := query.Name + "@" + dbname

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
	respWriter *responseWriter, cancelCh chan struct{},
) chan *collectors.TaskResult {
	logger := zerolog.Ctx(ctx)
	logger.Debug().Msg("queryhandler: database processing start")

	output := make(chan *collectors.TaskResult, len(parameters.dbNames)*len(parameters.queryNames))
	reqID, _ := hlog.IDFromCtx(ctx)

	for dbname, query := range parameters.iter() {
		queryTotalCnt.WithLabelValues(query.Name, dbname).Inc()

		if data, ok := q.getFromCache(query, dbname, parameters.extraParameters); ok {
			logger.Debug().Str("database", dbname).Str("query", query.Name).Msg("queryhandler: query result from cache")
			debug.TracePrintf(ctx, "data from cache for %q from %q", query.Name, dbname)
			respWriter.write(ctx, data)

			continue
		}

		task := collectors.NewTask(dbname, query, output).
			WithParams(parameters.extraParameters).
			WithReqID(reqID.String()).
			UseCancel(cancelCh)

		logger.Debug().Object("task", task).Msg("queryhandler: schedule task")
		q.taskQueue.AddTask(ctx, task)
		debug.TracePrintf(ctx, "scheduled %q to %q", query.Name, dbname)
		respWriter.incScheduled()
	}

	return output
}

func (q *queryHandler) gatherResults(ctx context.Context, respWriter *responseWriter,
	inp <-chan *collectors.TaskResult,
) {
	logger := log.Ctx(ctx)
	logger.Debug().Msgf("queryhandler: write result start; waiting for %d results", respWriter.scheduled)

	for respWriter.scheduled > 0 {
		select {
		case res, ok := <-inp:
			if !ok {
				return
			}

			respWriter.scheduled--

			task := res.Task

			if res.Error != nil {
				logger.Warn().Err(res.Error).Object("task", task).Msg("queryhandler: processing query error")
				debug.TracePrintf(ctx, "process query %q from %q: %v", task.Query.Name, task.DBName, res.Error)
				respWriter.write(ctx, []byte("# query "+res.Task.Query.Name+" in "+res.Task.DBName+" processing error\n"))

				var te *collectors.TaskError
				if errors.As(res.Error, &te) {
					metrics.IncErrorsCnt(te.Category)
				}

				continue
			}

			if err := q.validateOutput(res.Result); err != nil {
				logger.Warn().Err(err).Object("task", task).Msg("queryhandler: validate output error")
				debug.TracePrintf(ctx, "validate result of query %q from %q: %v", task.Query.Name, task.DBName, err)
				metrics.IncErrorsCnt(metrics.ErrCategoryInternalError)

				continue
			}

			logger.Debug().Object("res", res).Msg("queryhandler: write result")
			debug.TracePrintf(ctx, "write result %q from %q", task.Query.Name, task.DBName)
			respWriter.write(ctx, res.Result)
			q.putIntoCache(task, res.Result)

		case <-ctx.Done():
			err := ctx.Err()
			logger.Warn().Err(err).Msg("queryhandler: context cancelled")
			debug.TraceErrorf(ctx, "result error: %s", err)
			metrics.IncErrorsCnt(metrics.ErrCategoryCanceledError)

			return
		}
	}
}
