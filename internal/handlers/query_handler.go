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
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/db"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

var queryResultCache = support.NewCache()

// queryHandler handle all request for metrics
type (
	queryHandler struct {
		configuration         *conf.Configuration
		disableParallel       bool
		disableCache          bool
		validateOutputEnabled bool

		// runningQuery lock the same request for running twice
		runningQuery     map[string]runningQueryInfo
		runningQueryLock sync.Mutex
	}

	runningQueryInfo struct {
		ts    time.Time
		reqID string
	}
)

// SetConfiguration update handler configuration
func (q *queryHandler) SetConfiguration(c *conf.Configuration) {
	q.configuration = c
	queryResultCache.Clear()
}

// errQueryLocked is error when given query is locked more than 5 minutes
var errQueryLocked = errors.New("query locked")

func (q *queryHandler) waitForFinish(ctx context.Context, queryKey string) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var rqi runningQueryInfo

	for i := 0; i < 60; i++ { // 5min
		q.runningQueryLock.Lock()
		var ok bool
		rqi, ok = q.runningQuery[queryKey]
		if !ok || time.Since(rqi.ts).Minutes() > 15 {
			// no running previous qu(eue or last query is at least 15 minutes earlier
			reqID, _ := hlog.IDFromCtx(ctx)
			q.runningQuery[queryKey] = runningQueryInfo{
				ts:    time.Now(),
				reqID: reqID.String(),
			}
			q.runningQueryLock.Unlock()
			return nil
		}
		q.runningQueryLock.Unlock()
		log.Ctx(ctx).Debug().
			Str("queryKey", queryKey).Time("startTs", rqi.ts).
			TimeDiff("age", time.Now(), rqi.ts).Interface("block_by", rqi.reqID).
			Msg("wait for unlock")

		select {
		case <-ticker.C:
			{
			}
		case <-ctx.Done():
			{
				// context cancelled, ie. client disconnected
				return ctx.Err()
			}
		}
	}

	log.Ctx(ctx).Warn().
		Str("queryKey", queryKey).Time("startTs", rqi.ts).
		Interface("block_by", rqi.reqID).TimeDiff("age", time.Now(), rqi.ts).
		Msg("timeout on waiting to unlock; previous query is still executing or stalled")
	metrics.IncProcessErrorsCnt("lock")

	return errQueryLocked
}

func (q *queryHandler) markFinished(ctx context.Context, queryKey string) {
	reqID, _ := hlog.IDFromCtx(ctx)

	// mark query finished
	q.runningQueryLock.Lock()
	defer q.runningQueryLock.Unlock()

	// unlock only own locks;
	if l, ok := q.runningQuery[queryKey]; ok && l.reqID == reqID.String() {
		delete(q.runningQuery, queryKey)
	}
}

func (q *queryHandler) query(ctx context.Context, loader db.Loader,
	d *conf.Database, queryName string,
	params map[string]string,
) ([]byte, error) {
	query, ok := (q.configuration.Query)[queryName]
	if !ok {
		return nil, fmt.Errorf("unknown query '%s'", queryName)
	}

	logger := log.Ctx(ctx)
	metrics.IncQueryTotalCnt(queryName, d.Name)
	queryKey := queryName + "@" + d.Name

	logger.Debug().Msg("query start")

	// try to get item from cache
	if query.CachingTime > 0 && !q.disableCache {
		if data, ok := queryResultCache.Get(queryKey); ok {
			metrics.IncQueryCacheHits()
			logger.Debug().Msg("query result from cache")
			return data.([]byte), nil
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
	output, err := db.FormatResult(ctx, result, query, d)
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

	metrics.ObserveQueryDuration(queryName, d.Name, result.Duration)

	return output, nil
}

func (q *queryHandler) queryDatabase(ctx context.Context, dbName string,
	queryNames []string, params map[string]string, w http.ResponseWriter,
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

	writeMutex := ctx.Value(support.CtxWriteMutexID).(*sync.Mutex)
	anyProcessed := false
	for _, queryName := range queryNames {
		loggerQ := logger.With().Str("query", queryName).Logger()
		ctxQuery := loggerQ.WithContext(ctx)
		if output, err := q.query(ctxQuery, loader, d, queryName, params); err == nil {
			select {
			case <-ctx.Done():
				return fmt.Errorf("client disconnected: %w", ctx.Err())
			default:
			}

			anyProcessed = true

			writeMutex.Lock()
			_, err := w.Write(output)
			writeMutex.Unlock()
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

	if !anyProcessed {
		return errors.New("no queries successfully processed")
	}

	return nil
}

// queryDatabasesSeq query all given databases sequentially
func (q *queryHandler) queryDatabasesSeq(ctx context.Context, dbNames []string,
	queryNames []string, params map[string]string, w http.ResponseWriter,
) uint32 {
	logger := zerolog.Ctx(ctx)
	logger.Debug().Msg("database sequential processing start")

	writeMutex := sync.Mutex{}
	var successProcessed uint32 = 0

	for _, dbName := range dbNames {
		// check is client is still connected
		select {
		case <-ctx.Done():
			logger.Info().Err(ctx.Err()).Msg("context closed")
			return successProcessed
		default:
		}

		logger := logger.With().Str("db", dbName).Logger()
		ctx := logger.WithContext(ctx)
		ctx = context.WithValue(ctx, support.CtxWriteMutexID, &writeMutex)

		if err := q.queryDatabase(ctx, dbName, queryNames, params, w); err != nil {
			logger.Warn().Err(err).Msg("query database error")
			metrics.IncQueryTotalErrCnt(dbName)
		} else {
			successProcessed++
		}
	}

	return successProcessed
}

// queryDatabasesPar query all databases in parallel
func (q *queryHandler) queryDatabasesPar(ctx context.Context, dbNames []string,
	queryNames []string, params map[string]string, w http.ResponseWriter,
) uint32 {
	logger := zerolog.Ctx(ctx)

	logger.Debug().Msg("database parallel processing start")

	// writeMutex block parallel writes to Writer
	writeMutex := sync.Mutex{}
	// number of successful processes databases
	var successProcessed uint32 = 0
	var wg sync.WaitGroup
	// query databases parallel
	for _, dbName := range dbNames {
		wg.Add(1)

		logger := logger.With().Str("db", dbName).Logger()
		ctx := logger.WithContext(ctx)
		ctx = context.WithValue(ctx, support.CtxWriteMutexID, &writeMutex)

		go func(ctx context.Context, dbName string, successProcessed *uint32) {
			defer wg.Done()

			if err := q.queryDatabase(ctx, dbName, queryNames, params, w); err != nil {
				zerolog.Ctx(ctx).Warn().Err(err).Msg("query database error")
				metrics.IncQueryTotalErrCnt(dbName)
			} else {
				atomic.AddUint32(successProcessed, 1)
			}
		}(ctx, dbName, &successProcessed)
	}

	wg.Wait()
	return successProcessed
}

func (q *queryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx)

	queryNames := r.URL.Query()["query"]
	dbNames := r.URL.Query()["database"]

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
	if err := q.waitForFinish(ctx, r.URL.RawQuery); err == errQueryLocked {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer q.markFinished(ctx, r.URL.RawQuery)

	params := make(map[string]string)
	for k, v := range r.URL.Query() {
		if k != "query" && k != "database" {
			params[k] = v[0]
		}
	}

	queryNames = deduplicateStringList(queryNames)
	dbNames = deduplicateStringList(dbNames)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	var successProcessed uint32
	if q.disableParallel || len(dbNames) == 1 {
		successProcessed = q.queryDatabasesSeq(ctx, dbNames, queryNames, params, w)
	} else {
		successProcessed = q.queryDatabasesPar(ctx, dbNames, queryNames, params, w)
	}

	logger.Debug().Uint32("successProcessed", successProcessed).
		Msg("all database queries finished")

	if successProcessed == 0 {
		metrics.IncProcessErrorsCnt("bad_requests")
		http.Error(w, "error", http.StatusBadRequest)
	}
}

func deduplicateStringList(inp []string) []string {
	if len(inp) == 1 {
		return inp
	}

	tmpMap := make(map[string]bool)
	for _, s := range inp {
		tmpMap[s] = true
	}

	res := make([]string, 0, len(tmpMap))
	for k := range tmpMap {
		res = append(res, k)
	}

	return res
}
