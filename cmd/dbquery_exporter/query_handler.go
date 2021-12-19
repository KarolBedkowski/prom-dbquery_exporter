package main

//
// query_handler.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var queryResultCache = NewCache()

// QueryHandler handle all request for metrics
type (
	QueryHandler struct {
		configuration   *Configuration
		disableParallel bool

		// runningQuery lock the same request for running twice
		runningQuery     map[string]runningQueryInfo
		runningQueryLock sync.Mutex
	}

	runningQueryInfo struct {
		ts    time.Time
		reqID interface{}
	}
)

// NewQueryHandler create new QueryHandler from configuration
func NewQueryHandler(c *Configuration, disableParallel bool) *QueryHandler {
	return &QueryHandler{
		configuration:   c,
		runningQuery:    make(map[string]runningQueryInfo),
		disableParallel: disableParallel,
	}
}

// SetConfiguration update handler configuration
func (q *QueryHandler) SetConfiguration(c *Configuration) {
	q.configuration = c
	queryResultCache.Clear()
}

// ErrQueryLocked is error when given query is locked more than 5 minutes
var ErrQueryLocked = errors.New("query locked")

func (q *QueryHandler) waitForFinish(ctx context.Context, queryKey string) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var rqi runningQueryInfo

	for i := 0; i < 60; i++ { // 5min
		q.runningQueryLock.Lock()
		var ok bool
		rqi, ok = q.runningQuery[queryKey]
		if !ok || time.Since(rqi.ts).Minutes() > 15 {
			// no running previous queue or last query is at least 15 minutes earlier
			q.runningQuery[queryKey] = runningQueryInfo{ts: time.Now(), reqID: ctx.Value(CtxRequestID)}
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
	processErrorsCnt.WithLabelValues("lock").Inc()

	return ErrQueryLocked
}

func (q *QueryHandler) markFinished(queryKey string) {
	// mark query finished
	q.runningQueryLock.Lock()
	delete(q.runningQuery, queryKey)
	q.runningQueryLock.Unlock()
}

func (q *QueryHandler) query(ctx context.Context, loader Loader, db *Database, queryName string,
	params map[string]string) ([]byte, error) {
	query, ok := (q.configuration.Query)[queryName]
	if !ok {
		return nil, fmt.Errorf("unknown query '%s'", queryName)
	}

	logger := log.Ctx(ctx)
	queryTotalCnt.WithLabelValues(queryName, db.Name).Inc()
	queryKey := queryName + "@" + db.Name

	logger.Debug().Msg("query start")

	// try to get item from cache
	if query.CachingTime > 0 {
		if data, ok := queryResultCache.Get(queryKey); ok {
			queryCacheHits.Inc()
			logger.Debug().Msg("query result from cache")
			return data.([]byte), nil
		}
	}

	result, err := loader.Query(ctx, query, params)
	if err != nil {
		processErrorsCnt.WithLabelValues("query").Inc()
		return nil, fmt.Errorf("query error: %w", err)
	}
	logger.Debug().
		Int("records", len(result.Records)).
		Float64("duration", result.Duration).
		Msg("query finished")

	// format metrics
	output, err := FormatResult(ctx, result, query, db)
	if err != nil {
		processErrorsCnt.WithLabelValues("format").Inc()
		return nil, fmt.Errorf("format result error: %w", err)
	}

	if query.CachingTime > 0 {
		// update cache
		queryResultCache.Put(queryKey, query.CachingTime, output)
	}

	queryDuration.WithLabelValues(queryName, db.Name).Observe(result.Duration)

	return output, nil
}

func (q *QueryHandler) queryDatabase(ctx context.Context, dbName string,
	queryNames []string, params map[string]string, w http.ResponseWriter) error {
	db, ok := q.configuration.Database[dbName]
	if !ok {
		return fmt.Errorf("unknown database '%s'", dbName)
	}

	logger := log.Ctx(ctx)
	logger.Debug().Msg("start processing database")

	loader, err := GetLoader(db)
	if err != nil {
		return errors.New("get loader error")
	}

	logger.Debug().Str("loader", loader.String()).Msg("loader created")

	writeMutex := ctx.Value(CtxWriteMutexID).(*sync.Mutex)
	anyProcessed := false
	for _, queryName := range queryNames {
		loggerQ := logger.With().Str("query", queryName).Logger()
		ctxQuery := loggerQ.WithContext(ctx)
		if output, err := q.query(ctxQuery, loader, db, queryName, params); err == nil {
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
				processErrorsCnt.WithLabelValues("write").Inc()
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
func (q *QueryHandler) queryDatabasesSeq(ctx context.Context, dbNames []string,
	queryNames []string, params map[string]string, w http.ResponseWriter) uint32 {
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
		ctx = context.WithValue(ctx, CtxWriteMutexID, &writeMutex)

		if err := q.queryDatabase(ctx, dbName, queryNames, params, w); err != nil {
			logger.Warn().Err(err).Msg("query database error")
			queryErrorCnt.WithLabelValues(dbName).Inc()
		} else {
			successProcessed++
		}
	}

	return successProcessed
}

// queryDatabasesPar query all databases in parallel
func (q *QueryHandler) queryDatabasesPar(ctx context.Context, dbNames []string,
	queryNames []string, params map[string]string, w http.ResponseWriter) uint32 {
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
		ctx = context.WithValue(ctx, CtxWriteMutexID, &writeMutex)

		go func(ctx context.Context, dbName string, successProcessed *uint32) {
			defer wg.Done()

			if err := q.queryDatabase(ctx, dbName, queryNames, params, w); err != nil {
				zerolog.Ctx(ctx).Warn().Err(err).Msg("query database error")
				queryErrorCnt.WithLabelValues(dbName).Inc()
			} else {
				atomic.AddUint32(successProcessed, 1)
			}
		}(ctx, dbName, &successProcessed)
	}

	wg.Wait()
	return successProcessed
}

func (q *QueryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx)

	queryNames := r.URL.Query()["query"]
	dbNames := r.URL.Query()["database"]

	if len(queryNames) == 0 || len(dbNames) == 0 {
		http.Error(w, "missing parameters", http.StatusBadRequest)
		return
	}

	// prevent to run the same request twice
	if err := q.waitForFinish(ctx, r.URL.RawQuery); err == ErrQueryLocked {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer q.markFinished(r.URL.RawQuery)

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
		processErrorsCnt.WithLabelValues("bad_requests").Inc()
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
