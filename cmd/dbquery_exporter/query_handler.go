package main

//
// query_handler.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

var (
	// Metrics about the exporter itself.
	queryDuration = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: "dbquery_query_duration_seconds",
			Help: "Duration of query by the DBQuery exporter",
		},
		[]string{"query", "database"},
	)
	queryRequest = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dbquery_request_total",
			Help: "Total numbers requests given query",
		},
		[]string{"query", "database"},
	)
	queryRequestErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dbquery_request_errors_total",
			Help: "Errors in requests to the DBQuery exporter",
		},
	)
	queryCacheHits = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dbquery_cache_hit",
			Help: "Number of result loaded from cache",
		},
	)

	queryResultCache = resultCache{
		cache:     make(map[string]*cacheItem),
		cacheLock: &sync.Mutex{},
	}
)

func init() {
	prometheus.MustRegister(queryDuration)
	prometheus.MustRegister(queryRequest)
	prometheus.MustRegister(queryRequestErrors)
	prometheus.MustRegister(queryCacheHits)
}

type (
	// resultTmplData keep query result and some metadata parsed to template
	resultTmplData struct {
		// Records (rows)
		R []Record
		// Parameters
		P map[string]interface{}
		// Labels
		L map[string]interface{}

		QueryStartTime int64
		QueryDuration  float64
		Count          int
		// Query name
		Query string
		// Database name
		Database string
	}
)

func formatResult(ctx context.Context, qr *queryResult, query *Query,
	db *Database) ([]byte, error) {
	r := &resultTmplData{
		Query:          query.Name,
		Database:       db.Name,
		R:              qr.records,
		P:              qr.params,
		L:              db.Labels,
		QueryStartTime: qr.start.Unix(),
		QueryDuration:  qr.duration,
		Count:          len(qr.records),
	}

	var output bytes.Buffer
	bw := bufio.NewWriter(&output)
	err := query.MetricTpl.Execute(bw, r)
	if err != nil {
		return nil, fmt.Errorf("execute template error: %w", err)
	}

	_ = bw.Flush()

	b := bytes.TrimLeft(output.Bytes(), "\n\r\t ")
	return b, nil
}

type (
	resultCache struct {
		cache     map[string]*cacheItem
		cacheLock *sync.Mutex
	}

	cacheItem struct {
		expireTS time.Time
		content  []byte
	}
)

func (r *resultCache) get(queryKey string) ([]byte, bool) {
	r.cacheLock.Lock()
	defer r.cacheLock.Unlock()
	ci, ok := r.cache[queryKey]
	if !ok {
		return nil, false
	}

	if ci.expireTS.After(time.Now()) {
		queryCacheHits.Inc()
		return ci.content, true
	}

	delete(r.cache, queryKey)
	return nil, false
}

func (r *resultCache) put(queryKey string, ttl uint, data []byte) {
	r.cacheLock.Lock()
	r.cache[queryKey] = &cacheItem{
		expireTS: time.Now().Add(time.Duration(ttl) * time.Second),
		content:  data,
	}
	r.cacheLock.Unlock()
}

func (r *resultCache) clear() {
	r.cacheLock.Lock()
	r.cache = make(map[string]*cacheItem)
	r.cacheLock.Unlock()
}

// QueryHandler handle all request for metrics
type QueryHandler struct {
	configuration    *Configuration
	runningQuery     map[string]time.Time
	runningQueryLock *sync.Mutex
}

// NewQueryHandler create new QueryHandler from configuration
func NewQueryHandler(c *Configuration) *QueryHandler {
	return &QueryHandler{
		configuration:    c,
		runningQuery:     make(map[string]time.Time),
		runningQueryLock: &sync.Mutex{},
	}
}

// SetConfiguration update handler configuration
func (q *QueryHandler) SetConfiguration(c *Configuration) {
	q.configuration = c
	queryResultCache.clear()
}

func (q *QueryHandler) waitQueryFinish(queryKey string) (ok bool) {
	for i := 0; i < 120; i += 5 { // 2min
		q.runningQueryLock.Lock()
		startTs, ok := q.runningQuery[queryKey]
		if !ok || startTs.IsZero() || time.Since(startTs).Minutes() > 15 {
			// no running previous queue or last query is at least 15 minutes earlier
			q.runningQuery[queryKey] = time.Now()
			q.runningQueryLock.Unlock()
			return true
		}
		q.runningQueryLock.Unlock()
		time.Sleep(time.Duration(5) * time.Second)
	}

	return false
}

func (q *QueryHandler) query(ctx context.Context, loader Loader, db *Database, queryName string, params map[string]string) ([]byte, error) {
	query, ok := (q.configuration.Query)[queryName]
	if !ok {
		return nil, fmt.Errorf("unknown query '%s'", queryName)
	}

	logger := log.Ctx(ctx)
	queryRequest.WithLabelValues(queryName, db.Name).Inc()
	queryKey := queryName + "\t" + db.Name

	logger.Debug().Msg("query start")

	// try to get item from cache
	if query.CachingTime > 0 {
		if data, ok := queryResultCache.get(queryKey); ok {
			logger.Debug().Msg("query result from cache")
			return data, nil
		}
	}

	// check is previous query finished; if yes - lock it
	if !q.waitQueryFinish(queryKey) {
		return nil, errors.New("timeout while waiting for query finish")
	}

	result, err := loader.Query(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	logger.Debug().
		Int("records", len(result.records)).
		Float64("duration", result.duration).
		Msg("query finished")

	// format metrics
	output, err := formatResult(ctx, result, query, db)
	if err != nil {
		return nil, fmt.Errorf("format result error: %w", err)
	}

	if query.CachingTime > 0 {
		// update cache
		queryResultCache.put(queryKey, query.CachingTime, output)
	}

	queryDuration.WithLabelValues(queryName, db.Name).Observe(result.duration)

	// mark query finished
	q.runningQueryLock.Lock()
	q.runningQuery[queryKey] = time.Time{}
	q.runningQueryLock.Unlock()

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

	if loader != nil {
		defer loader.Close(logger.WithContext(ctx))
	}

	logger.Debug().Str("loader", loader.String()).Msg("loader created")

	anyProcessed := false
	for _, queryName := range queryNames {
		loggerQ := logger.With().Str("query", queryName).Logger()
		ctxQuery := loggerQ.WithContext(ctx)
		output, err := q.query(ctxQuery, loader, db, queryName, params)
		if err == nil {
			if _, err := w.Write([]byte(fmt.Sprintf("## query %s\n", queryName))); err != nil {
				return fmt.Errorf("write result error: %w", err)
			}
			if _, err := w.Write(output); err != nil {
				return fmt.Errorf("write result error: %w", err)
			}
			anyProcessed = true
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

func (q QueryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.Ctx(ctx)

	queryNames := r.URL.Query()["query"]
	dbNames := r.URL.Query()["database"]
	params := make(map[string]string)
	for k, v := range r.URL.Query() {
		if k != "query" && k != "database" {
			params[k] = v[0]
		}
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	anyProcessed := false
	for _, dbName := range dbNames {
		logger := logger.With().Str("db", dbName).Logger()
		ctx := logger.WithContext(ctx)

		if err := q.queryDatabase(ctx, dbName, queryNames, params, w); err != nil {
			logger.Warn().Err(err).Msg("query database error")
			queryRequestErrors.Inc()
			continue
		}

		anyProcessed = true
	}

	if !anyProcessed {
		http.Error(w, "error", 400)
	}
}
