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
	"maps"
	"net/http"
	"slices"
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
	configuration    *conf.Configuration
	queryResultCache *support.Cache[[]byte]
	// runningQuery lock the same request for running twice
	queryLocker locker
	taskQueue   chan<- *collectors.Task
}

func newQueryHandler(c *conf.Configuration, cache *support.Cache[[]byte],
	taskQueue chan<- *collectors.Task,
) *queryHandler {
	return &queryHandler{
		configuration:    c,
		queryLocker:      newLocker(),
		queryResultCache: cache,
		taskQueue:        taskQueue,
	}
}

func (q *queryHandler) Handler() http.Handler {
	h := newLogMiddleware(
		promhttp.InstrumentHandlerDuration(
			metrics.NewReqDurationWrapper("query"),
			q), "query", false)

	h = support.NewTraceMiddleware("dbquery_exporter")(h)
	h = hlog.RequestIDHandler("req_id", "X-Request-Id")(h)
	h = hlog.NewHandler(log.Logger)(h)

	return h
}

// SetConfiguration update handler configuration.
func (q *queryHandler) UpdateConf(c *conf.Configuration) {
	q.configuration = c

	q.queryResultCache.Clear()
}

func (q *queryHandler) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	ctx, cancel := context.WithTimeout(req.Context(), q.configuration.Global.RequestTimeout)
	defer cancel()

	logger := log.Ctx(ctx)
	queryNames := req.URL.Query()["query"]
	dbNames := req.URL.Query()["database"]
	requestID, _ := hlog.IDFromCtx(ctx)

	support.SetGoroutineLabels(ctx, "requestID", requestID.String(), "req", req.URL.String())

	// prevent to run the same request twice
	if locker, ok := q.queryLocker.tryLock(req.URL.RawQuery, requestID.String()); !ok {
		logger.Warn().Msgf("query already in progress, started by %s", locker)
		http.Error(writer, "query in progress", http.StatusInternalServerError)
		support.TraceErrorf(ctx, "query locked by %s", locker)

		return
	}

	defer q.queryLocker.unlock(req.URL.RawQuery)

	for _, g := range req.URL.Query()["group"] {
		q := q.configuration.GroupQueries(g)
		if len(q) > 0 {
			logger.Debug().Str("group", g).Interface("queries", q).Msg("queryhandler: add queries from group")
			queryNames = append(queryNames, q...)
		} else {
			logger.Info().Str("group", g).Msg("queryhandler: group not found")
		}
	}

	if len(queryNames) == 0 || len(dbNames) == 0 {
		http.Error(writer, "missing required parameters", http.StatusBadRequest)
		support.TraceErrorf(ctx, "bad request")

		return
	}

	params := paramsFromQuery(req)
	queryNames = deduplicateStringList(queryNames)
	dbNames = deduplicateStringList(dbNames)
	dWriter := dataWriter{writer: writer}

	dWriter.writeHeaders()
	support.TracePrintf(ctx, "start querying db")

	output := q.queryDatabases(ctx, dbNames, queryNames, params, &dWriter)

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
	if q.configuration.DisableCache || len(params) > 0 {
		return nil, false
	}

	if query.CachingTime > 0 {
		queryKey := query.Name + "@" + dbName
		if data, ok := q.queryResultCache.Get(queryKey); ok {
			return data, ok
		}
	}

	return nil, false
}

func (q *queryHandler) putIntoCache(task *collectors.Task, data []byte) {
	query := task.Query

	// do not cache query with user params
	if query == nil || len(task.Params) > 0 {
		return
	}

	if query.CachingTime > 0 && !q.configuration.DisableCache {
		queryKey := query.Name + "@" + task.DBName

		q.queryResultCache.Put(queryKey, query.CachingTime, data)
	}
}

func (q *queryHandler) validateOutput(output []byte) error {
	if q.configuration.ValidateOutput {
		var parser expfmt.TextParser
		if _, err := parser.TextToMetricFamilies(bytes.NewReader(output)); err != nil {
			return fmt.Errorf("validate result error: %w", err)
		}
	}

	return nil
}

func (q *queryHandler) queryDatabases(ctx context.Context, dbNames []string,
	queryNames []string, params map[string]any, dWriter *dataWriter,
) chan *collectors.TaskResult {
	logger := zerolog.Ctx(ctx)
	logger.Debug().Msg("queryhandler: database processing start")

	output := make(chan *collectors.TaskResult, len(dbNames)*len(queryNames))
	now := time.Now()

	for _, dbName := range dbNames {
		for _, queryName := range queryNames {
			query, ok := (q.configuration.Query)[queryName]
			if !ok {
				logger.Error().Str("dbname", dbName).Str("query", queryName).Msg("queryhandler: unknown query")

				continue
			}

			metrics.IncQueryTotalCnt(queryName, dbName)

			if data, ok := q.getFromCache(query, dbName, params); ok {
				logger.Debug().Str("dbname", dbName).Str("query", queryName).Msg("queryhandler: query result from cache")
				support.TracePrintf(ctx, "data from cache for %q from %q", queryName, dbName)
				dWriter.write(ctx, data)

				continue
			}

			task := collectors.Task{
				Ctx:          ctx,
				DBName:       dbName,
				QueryName:    queryName,
				Params:       params,
				Output:       output,
				Query:        query,
				RequestStart: now,
			}

			logger.Debug().Object("task", &task).Msg("queryhandler: schedule task")

			q.taskQueue <- &task

			support.TracePrintf(ctx, "scheduled %q to %q", queryName, dbName)

			dWriter.incScheduled()
		}
	}

	return output
}

func (q *queryHandler) writeResult(ctx context.Context, dWriter *dataWriter, inp <-chan *collectors.TaskResult) {
	logger := log.Ctx(ctx)
	logger.Debug().Msgf("queryhandler: write result start; waiting for %d results", dWriter.scheduled)

loop:
	for dWriter.scheduled > 0 {
		select {
		case res := <-inp:
			dWriter.scheduled--

			task := res.Task

			if res.Error != nil {
				logger.Error().Err(res.Error).Object("task", task).Msg("queryhandler: processing query error")
				support.TracePrintf(ctx, "process query %q from %q: %v", task.QueryName, task.DBName, res.Error)
				dWriter.writeError(ctx, "# query "+res.Task.QueryName+" in "+res.Task.DBName+" processing error")

				continue
			}

			logger.Debug().Object("res", res).Msg("queryhandler: write result")
			support.TracePrintf(ctx, "write result %q from %q", task.QueryName, task.DBName)

			if err := q.validateOutput(res.Result); err != nil {
				logger.Error().Err(err).Object("task", task).Msg("queryhandler: validate output error")
				support.TracePrintf(ctx, "validate result of query %q from %q: %v", task.QueryName, task.DBName, err)

				continue
			}

			dWriter.write(ctx, res.Result)
			q.putIntoCache(task, res.Result)

		case <-ctx.Done():
			err := ctx.Err()
			logger.Error().Err(err).Msg("queryhandler: result error")
			metrics.IncProcessErrorsCnt("cancel")
			support.TraceErrorf(ctx, "result error: %s", err)

			break loop
		}
	}
}

func paramsFromQuery(req *http.Request) map[string]any {
	params := make(map[string]any)

	for k, v := range req.URL.Query() {
		if k != "query" && k != "database" && len(v) > 0 {
			params[k] = v[0]
		}
	}

	return params
}

func deduplicateStringList(inp []string) []string {
	if len(inp) == 1 {
		return inp
	}

	tmpMap := make(map[string]bool, len(inp))
	for _, s := range inp {
		tmpMap[s] = true
	}

	return slices.Collect(maps.Keys(tmpMap))
}
