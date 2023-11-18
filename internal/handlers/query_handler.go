package handlers

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

func (q *queryHandler) query(ctx context.Context, loader db.Loader,
	database *conf.Database, queryName string,
	params map[string]string,
) ([]byte, error) {
	query, ok := (q.configuration.Query)[queryName]
	if !ok {
		return nil, fmt.Errorf("unknown query '%s'", queryName)
	}

	logger := log.Ctx(ctx)

	metrics.IncQueryTotalCnt(queryName, database.Name)
	queryKey := queryName + "@" + database.Name

	logger.Debug().Msg("query start")

	// try to get item from cache
	if query.CachingTime > 0 && !q.disableCache {
		if data, ok := queryResultCache.Get(queryKey); ok {
			metrics.IncQueryCacheHits()
			logger.Debug().Msg("query result from cache")

			return data, nil
		}
	}

	result, err := loader.Query(ctx, query, params)
	if err != nil {
		metrics.IncProcessErrorsCnt("query")
		return nil, fmt.Errorf("query error: %w", err)
	}

	logger.Debug().
		Int("records", len(result.Records)).
		Float64("duration", result.Duration).
		Msg("query finished")

	// format metrics
	output, err := db.FormatResult(ctx, result, query, database)
	if err != nil {
		metrics.IncProcessErrorsCnt("format")
		return nil, fmt.Errorf("format result error: %w", err)
	}

	if q.validateOutputEnabled {
		var parser expfmt.TextParser
		if _, err := parser.TextToMetricFamilies(bytes.NewReader(output)); err != nil {
			return nil, fmt.Errorf("validate result error: %w", err)
		}
	}

	if query.CachingTime > 0 && !q.disableCache {
		// update cache
		queryResultCache.Put(queryKey, query.CachingTime, output)
	}

	metrics.ObserveQueryDuration(queryName, database.Name, result.Duration)

	return output, nil
}

func (q *queryHandler) queryDatabase(ctx context.Context, dbName string,
	queryNames []string, params map[string]string, w chan []byte,
) error {
	d, ok := q.configuration.Database[dbName]
	if !ok {
		return fmt.Errorf("unknown database '%s'", dbName)
	}

	logger := log.Ctx(ctx)
	logger.Debug().Msg("start processing database")

	loader, err := db.GetLoader(d)
	if err != nil {
		return fmt.Errorf("get loader error: %w", err)
	}

	logger.Debug().Str("loader", loader.String()).Msg("loader created")

	for _, queryName := range queryNames {
		loggerQ := logger.With().Str("query", queryName).Logger()

		ctxQuery := loggerQ.WithContext(ctx)
		if output, err := q.query(ctxQuery, loader, d, queryName, params); err == nil {
			select {
			case <-ctx.Done():
				return fmt.Errorf("client disconnected: %w", ctx.Err())
			default:
			}

			w <- output

			if err != nil {
				metrics.IncProcessErrorsCnt("write")
				return fmt.Errorf("write result error: %w", err)
			}
		} else {
			loggerQ.Error().Err(err).Msg("make query error")
			// to not break processing other queries when query fail
		}
	}

	logger.Debug().Msg("database processing finished")

	return nil
}

// queryDatabasesSeq query all given databases sequentially.
func (q *queryHandler) queryDatabasesSeq(ctx context.Context, dbNames []string,
	queryNames []string, params map[string]string, w chan []byte,
) {
	logger := zerolog.Ctx(ctx)
	logger.Debug().Msg("database sequential processing start")

	for _, dbName := range dbNames {
		// check is client is still connected
		select {
		case <-ctx.Done():
			logger.Info().Err(ctx.Err()).Msg("context closed")
			return
		default:
		}

		logger := logger.With().Str("db", dbName).Logger()
		ctx := logger.WithContext(ctx)

		if err := q.queryDatabase(ctx, dbName, queryNames, params, w); err != nil {
			logger.Warn().Err(err).Msg("query database error")
			metrics.IncQueryTotalErrCnt(dbName)
		}
	}

	close(w)
}

// queryDatabasesPar query all databases in parallel.
func (q *queryHandler) queryDatabasesPar(ctx context.Context, dbNames []string,
	queryNames []string, params map[string]string, w chan []byte,
) {
	logger := zerolog.Ctx(ctx)

	logger.Debug().Msg("database parallel processing start")

	// number of successful processes databases

	var waitGroup sync.WaitGroup
	// query databases parallel
	for _, dbName := range dbNames {
		waitGroup.Add(1)

		logger := logger.With().Str("db", dbName).Logger()
		ctx := logger.WithContext(ctx)

		go func(ctx context.Context, dbName string) {
			defer waitGroup.Done()

			if err := q.queryDatabase(ctx, dbName, queryNames, params, w); err != nil {
				zerolog.Ctx(ctx).Warn().Err(err).Msg("query database error")
				metrics.IncQueryTotalErrCnt(dbName)
			}
		}(ctx, dbName)
	}

	waitGroup.Wait()
	close(w)
}

func (q *queryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "missing required parameters", http.StatusBadRequest)
		return
	}

	// prevent to run the same request twice
	if err := q.queryLocker.tryLock(r.URL.RawQuery, requestID.String()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	defer q.queryLocker.unlock(r.URL.RawQuery)

	params := paramsFromQuery(r)
	queryNames = deduplicateStringList(queryNames)
	dbNames = deduplicateStringList(dbNames)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	outputChannel := make(chan []byte)
	finish := make(chan int, 1)

	go func() {
		cnt := 0
		for data := range outputChannel {
			logger.Debug().Bytes("data", data).Msg("output")
			_, _ = w.Write(data)
			cnt++
		}
		finish <- cnt
	}()

	if q.disableParallel || len(dbNames) == 1 {
		q.queryDatabasesSeq(ctx, dbNames, queryNames, params, outputChannel)
	} else {
		q.queryDatabasesPar(ctx, dbNames, queryNames, params, outputChannel)
	}

	time.Sleep(time.Duration(15) * time.Second)

	successProcessed := 0

	select {
	case successProcessed = <-finish:
	case <-ctx.Done():
		logger.Info().Err(ctx.Err()).Msg("context done")
	}

	logger.Debug().Int("successProcessed", successProcessed).
		Msg("all database queries finished")

	if successProcessed == 0 {
		metrics.IncProcessErrorsCnt("bad_requests")
		http.Error(w, "error", http.StatusBadRequest)
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
