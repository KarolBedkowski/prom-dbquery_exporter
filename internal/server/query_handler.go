package server

//
// query_handler.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

type locker struct {
	sync.Mutex

	runningQuery map[string]string
}

func newLocker() locker {
	return locker{runningQuery: make(map[string]string)}
}

func (l *locker) tryLock(queryKey, reqID string) (string, bool) {
	l.Lock()
	defer l.Unlock()

	if rid, ok := l.runningQuery[queryKey]; ok {
		return rid, false
	}

	l.runningQuery[queryKey] = reqID

	return "", true
}

func (l *locker) unlock(queryKey string) {
	l.Lock()
	defer l.Unlock()

	delete(l.runningQuery, queryKey)
}

// queryHandler handle all request for metrics.
type queryHandler struct {
	configuration         *conf.Configuration
	disableCache          bool
	validateOutputEnabled bool

	// runningQuery lock the same request for running twice
	queryLocker      locker
	queryResultCache *support.Cache[[]byte]
}

func newQueryHandler(c *conf.Configuration, disableCache bool, validateOutput bool,
) *queryHandler {
	return &queryHandler{
		configuration:         c,
		queryLocker:           newLocker(),
		disableCache:          disableCache,
		validateOutputEnabled: validateOutput,
		queryResultCache:      support.NewCache[[]byte]("query_cache"),
	}
}

func (q *queryHandler) Handler() http.Handler {
	h := newLogMiddleware(
		promhttp.InstrumentHandlerDuration(
			metrics.NewReqDurationWraper("query"),
			q), "query", false)

	h = support.NewTraceMiddleware("dbquery_exporter")(h)
	h = hlog.RequestIDHandler("req_id", "X-Request-Id")(h)
	h = hlog.NewHandler(log.Logger)(h)

	return h
}

// SetConfiguration update handler configuration.
func (q *queryHandler) SetConfiguration(c *conf.Configuration) {
	q.configuration = c

	q.queryResultCache.Clear()
}

func (q *queryHandler) getFromCache(query *conf.Query, dbName string) ([]byte, bool) {
	if query.CachingTime > 0 && !q.disableCache {
		queryKey := query.Name + "@" + dbName
		if data, ok := q.queryResultCache.Get(queryKey); ok {
			return data, ok
		}
	}

	return nil, false
}

func (q *queryHandler) putIntoCache(query *conf.Query, dbName string, data []byte) {
	if query.CachingTime > 0 && !q.disableCache {
		queryKey := query.Name + "@" + dbName

		q.queryResultCache.Put(queryKey, query.CachingTime, data)
	}
}

// queryDatabases query all given databases sequentially.
func (q *queryHandler) queryDatabases(ctx context.Context, dbNames []string,
	queryNames []string, params map[string]any,
) (chan *collectors.TaskResult, int) {
	logger := zerolog.Ctx(ctx)
	logger.Debug().Msg("database sequential processing start")

	output := make(chan *collectors.TaskResult, len(dbNames)*len(queryNames))

	scheduled := 0

	for _, dbName := range dbNames {
		for _, queryName := range queryNames {
			query, ok := (q.configuration.Query)[queryName]
			if !ok {
				logger.Error().Str("dbname", dbName).Str("query", queryName).Msg("unknown query")

				continue
			}

			metrics.IncQueryTotalCnt(queryName, dbName)

			if data, ok := q.getFromCache(query, dbName); ok {
				logger.Debug().Msg("query result from cache")
				support.TracePrintf(ctx, "data from cache for %q from %q", queryName, dbName)

				output <- collectors.NewSimpleTaskResult(data, dbName, queryName)

				scheduled++

				continue
			}

			task := collectors.Task{
				Ctx:       ctx,
				DBName:    dbName,
				QueryName: queryName,
				Params:    params,
				Output:    output,
				Query:     query,
			}

			if err := collectors.CollectorsPool.ScheduleTask(&task); err != nil { //nolint:contextcheck
				support.TraceErrorf(ctx, "scheduled %q to %q error: %v", queryName, dbName, err)
				logger.Error().Err(err).Str("dbname", dbName).Str("query", queryName).
					Msg("start task error")
			} else {
				support.TracePrintf(ctx, "scheduled %q to %q", queryName, dbName)

				scheduled++
			}
		}
	}

	return output, scheduled
}

func (q *queryHandler) writeResult(ctx context.Context, output chan *collectors.TaskResult, scheduled int,
	writer http.ResponseWriter,
) int {
	logger := log.Ctx(ctx)
	logger.Debug().Msg("database sequential processing start")

	successProcessed := 0

loop:
	for scheduled > 0 {
		select {
		case res := <-output:
			scheduled--

			task := res.Task

			if res.Error != nil {
				logger.Error().Err(res.Error).Object("task", task).Msg("processing query error")
				support.TracePrintf(ctx, "process query %q from %q: %v", task.QueryName, task.DBName, res.Error)

				msg := fmt.Sprintf("# query %q in %q processing error: %q\n",
					res.Task.QueryName, res.Task.DBName, res.Error.Error())
				if _, err := writer.Write([]byte(msg)); err != nil {
					logger.Error().Err(err).Msg("write error")
					support.TraceErrorf(ctx, "write error: %s", err)
					metrics.IncProcessErrorsCnt("write")
				}

				continue
			}

			logger.Debug().Object("res", res).Msg("write result")
			support.TracePrintf(ctx, "write result %q from %q", task.QueryName, task.DBName)

			if _, err := writer.Write(res.Result); err != nil {
				logger.Error().Err(err).Msg("write error")
				metrics.IncProcessErrorsCnt("write")
			} else {
				successProcessed++
			}

			if task.Query != nil {
				q.putIntoCache(task.Query, task.DBName, res.Result)
			}

		case <-ctx.Done():
			err := ctx.Err()
			logger.Error().Err(err).Msg("result error")
			metrics.IncProcessErrorsCnt("cancel")
			support.TraceErrorf(ctx, "result error: %s", err)

			break loop
		}
	}

	close(output)

	return successProcessed
}

func (q *queryHandler) ServeHTTP(writer http.ResponseWriter, req *http.Request) { //nolint:funlen
	ctx := req.Context()
	logger := log.Ctx(ctx)

	queryNames := req.URL.Query()["query"]
	dbNames := req.URL.Query()["database"]
	requestID, _ := hlog.IDFromCtx(ctx)

	support.SetGoroutineLabels(ctx, "requestID", requestID.String(), "req", req.URL.String())

	for _, g := range req.URL.Query()["group"] {
		q := q.configuration.GroupQueries(g)
		if len(q) > 0 {
			logger.Debug().Str("group", g).Interface("queries", q).Msg("add queries from group")
			queryNames = append(queryNames, q...)
		}
	}

	if len(queryNames) == 0 || len(dbNames) == 0 {
		http.Error(writer, "missing required parameters", http.StatusBadRequest)
		support.TraceErrorf(ctx, "bad request")

		return
	}

	// prevent to run the same request twice
	if locker, ok := q.queryLocker.tryLock(req.URL.RawQuery, requestID.String()); !ok {
		http.Error(writer, "query in progress, started by "+locker, http.StatusInternalServerError)
		support.TraceErrorf(ctx, "query locked by %s", locker)

		return
	}

	defer q.queryLocker.unlock(req.URL.RawQuery)

	params := paramsFromQuery(req)
	queryNames = deduplicateStringList(queryNames)
	dbNames = deduplicateStringList(dbNames)

	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if t := q.configuration.Global.RequestTimeout; t > 0 {
		logger.Debug().Msgf("set request timeout %d", t)

		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(t)*time.Second)

		defer cancel()
	}

	support.TracePrintf(ctx, "start querying db")

	out, scheduled := q.queryDatabases(ctx, dbNames, queryNames, params)

	support.TracePrintf(ctx, "start reading data, scheduled: %d", scheduled)

	successProcessed := q.writeResult(ctx, out, scheduled, writer)

	logger.Debug().Int("successProcessed", successProcessed).
		Err(ctx.Err()).
		Msg("all database queries finished")

	if successProcessed == 0 {
		metrics.IncProcessErrorsCnt("bad_requests")
		http.Error(writer, "error", http.StatusBadRequest)
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

	res := make([]string, 0, len(tmpMap))
	for k := range tmpMap {
		res = append(res, k)
	}

	return res
}
