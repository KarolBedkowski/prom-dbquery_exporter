package handlers

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

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/db"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

var queryResultCache = support.NewCache[[]byte]()

type locker struct {
	sync.Mutex

	runningQuery map[string]string
}

func newLocker() locker {
	return locker{runningQuery: make(map[string]string)}
}

func (l *locker) tryLock(queryKey, reqID string) error {
	l.Lock()
	defer l.Unlock()

	if rid, ok := l.runningQuery[queryKey]; ok {
		return fmt.Errorf("query already running, started by %s", rid)
	}

	l.runningQuery[queryKey] = reqID

	return nil
}

func (l *locker) unlock(queryKey string) {
	l.Lock()
	defer l.Unlock()

	delete(l.runningQuery, queryKey)
}

// queryHandler handle all request for metrics.
type (
	queryHandler struct {
		configuration         *conf.Configuration
		disableParallel       bool
		disableCache          bool
		validateOutputEnabled bool

		// runningQuery lock the same request for running twice
		queryLocker locker
	}
)

func newQueryHandler(c *conf.Configuration, disableParallel bool,
	disableCache bool, validateOutput bool,
) *queryHandler {
	return &queryHandler{
		configuration:         c,
		queryLocker:           newLocker(),
		disableParallel:       disableParallel,
		disableCache:          disableCache,
		validateOutputEnabled: validateOutput,
	}
}

func (q *queryHandler) Handler() http.Handler {
	h := newLogMiddleware(
		promhttp.InstrumentHandlerDuration(
			metrics.NewReqDurationWraper("query"),
			q), "query", false)

	h = hlog.RequestIDHandler("req_id", "X-Request-Id")(h)
	h = hlog.NewHandler(log.Logger)(h)

	return h
}

// SetConfiguration update handler configuration.
func (q *queryHandler) SetConfiguration(c *conf.Configuration) {
	q.configuration = c

	queryResultCache.Clear()
}

func (q *queryHandler) getFromCache(query *conf.Query, dbName string) ([]byte, bool) {
	if query.CachingTime > 0 && !q.disableCache {
		queryKey := query.Name + "@" + dbName
		if data, ok := queryResultCache.Get(queryKey); ok {
			metrics.IncQueryCacheHits()

			return data, ok
		}
	}

	return nil, false
}

func (q *queryHandler) putIntoCache(query *conf.Query, dbName string, data []byte) {
	if query.CachingTime > 0 && !q.disableCache {
		queryKey := query.Name + "@" + dbName
		queryResultCache.Put(queryKey, query.CachingTime, data)
	}
}

// queryDatabases query all given databases sequentially.
func (q *queryHandler) queryDatabases(ctx context.Context, dbNames []string,
	queryNames []string, params map[string]string,
) (chan *db.TaskResult, int) {
	logger := zerolog.Ctx(ctx)
	logger.Debug().Msg("database sequential processing start")

	output := make(chan *db.TaskResult, len(dbNames)*len(queryNames))

	scheduled := 0

	for _, dbName := range dbNames {
		for _, queryName := range queryNames {
			metrics.IncQueryTotalCnt(queryName, dbName)

			query, ok := (q.configuration.Query)[queryName]
			if !ok {
				logger.Error().Str("dbname", dbName).Str("query", queryName).Msg("unknown query")

				continue
			}

			if data, ok := q.getFromCache(query, dbName); ok {
				logger.Debug().Msg("query result from cache")
				output <- &db.TaskResult{Result: data}
				scheduled++

				continue
			}

			task := db.Task{
				Ctx:       ctx,
				DBName:    dbName,
				QueryName: queryName,
				Params:    params,
				Output:    output,
				Query:     query,
			}

			if err := db.DatabasesPool.PutTask(&task); err != nil {
				logger.Error().Err(err).Str("dbname", dbName).Str("query", queryName).
					Msg("start task error")
			} else {
				scheduled++
			}
		}
	}

	return output, scheduled
}

func (q *queryHandler) writeResult(ctx context.Context, output chan *db.TaskResult, scheduled int,
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

			if res.Error == nil {
				logger.Debug().Object("res", res).Msg("write result")

				if _, err := writer.Write(res.Result); err != nil {
					logger.Error().Err(err).Msg("write errror")
					metrics.IncProcessErrorsCnt("write")
				} else {
					successProcessed++
				}

				if res.Query != nil {
					q.putIntoCache(res.Query, res.DBName, res.Result)
				}
			}
		case <-ctx.Done():
			logger.Error().Err(ctx.Err()).Msg("result error")

			break loop
		}
	}

	close(output)

	return successProcessed
}

func (q *queryHandler) ServeHTTP(writer http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx)

	queryNames := r.URL.Query()["query"]
	dbNames := r.URL.Query()["database"]
	requestID, _ := hlog.IDFromCtx(ctx)

	for _, g := range r.URL.Query()["group"] {
		q := q.configuration.GroupQueries(g)
		if len(q) > 0 {
			logger.Debug().Str("group", g).Interface("queries", q).Msg("add queries from group")
			queryNames = append(queryNames, q...)
		}
	}

	if len(queryNames) == 0 || len(dbNames) == 0 {
		http.Error(writer, "missing required parameters", http.StatusBadRequest)

		return
	}

	// prevent to run the same request twice
	if err := q.queryLocker.tryLock(r.URL.RawQuery, requestID.String()); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)

		return
	}

	defer q.queryLocker.unlock(r.URL.RawQuery)

	params := paramsFromQuery(r)
	queryNames = deduplicateStringList(queryNames)
	dbNames = deduplicateStringList(dbNames)

	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")

	out, scheduled := q.queryDatabases(ctx, dbNames, queryNames, params)
	successProcessed := q.writeResult(ctx, out, scheduled, writer)

	logger.Debug().Int("successProcessed", successProcessed).
		Msg("all database queries finished")

	if successProcessed == 0 {
		metrics.IncProcessErrorsCnt("bad_requests")
		http.Error(writer, "error", http.StatusBadRequest)
	}
}

func paramsFromQuery(req *http.Request) map[string]string {
	params := make(map[string]string)

	for k, v := range req.URL.Query() {
		if k != "query" && k != "database" {
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
