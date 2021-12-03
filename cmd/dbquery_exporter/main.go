package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"

	//	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	// _ "github.com/denisenkom/go-mssqldb"
	// _ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	// _ "github.com/mattn/go-oci8"
	_ "github.com/mattn/go-sqlite3"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/rs/zerolog/log"
)

var (
	showVersion = flag.Bool("version", false, "Print version information.")
	configFile  = flag.String("config.file", "dbquery.yml",
		"Path to configuration file.")
	listenAddress = flag.String("web.listen-address", ":9122",
		"Address to listen on for web interface and telemetry.")
	loglevel = flag.String("log.level", "info",
		"Logging level (debug, info, warn, error, fatal)")
	logformat = flag.String("log.format", "logfmt",
		"Logging log format (logfmt, json)")

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
)

type (
	// Result keep query result and some metadata parsed to template
	Result struct {
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

	cacheItem struct {
		expireTS time.Time
		content  []byte
	}
)

func init() {
	prometheus.MustRegister(queryDuration)
	prometheus.MustRegister(queryRequest)
	prometheus.MustRegister(queryRequestErrors)
	prometheus.MustRegister(queryCacheHits)
	prometheus.MustRegister(version.NewCollector("dbquery_exporter"))
}

func formatResult(ctx context.Context, db *Database, query *Query, dbName, queryName string, result *queryResult) ([]byte, error) {
	data := &Result{
		Query:          queryName,
		Database:       dbName,
		R:              result.records,
		P:              result.params,
		L:              db.Labels,
		QueryStartTime: result.start.Unix(),
		QueryDuration:  result.duration,
		Count:          len(result.records),
	}

	log.Ctx(ctx).Debug().
		Float64("query_time", data.QueryDuration).
		Int("rows", data.Count).
		Msg("format result")

	var output bytes.Buffer
	bw := bufio.NewWriter(&output)

	err := query.MetricTpl.Execute(bw, data)
	if err != nil {
		return nil, fmt.Errorf("execute template error: %w", err)
	}

	bw.Flush()

	b := bytes.TrimLeft(output.Bytes(), "\n\r\t ")

	return b, nil
}

type queryHandler struct {
	Configuration *Configuration
	cache         map[string]*cacheItem
	cacheLock     *sync.Mutex

	runningQuery     map[string]time.Time
	runningQueryLock *sync.Mutex
}

func newQueryHandler(c *Configuration) *queryHandler {
	return &queryHandler{
		Configuration:    c,
		cache:            make(map[string]*cacheItem),
		cacheLock:        &sync.Mutex{},
		runningQuery:     make(map[string]time.Time),
		runningQueryLock: &sync.Mutex{},
	}
}

func (q *queryHandler) clearCache() {
	q.cacheLock.Lock()
	defer q.cacheLock.Unlock()
	q.cache = make(map[string]*cacheItem)
}

func (q *queryHandler) makeQuery(ctx context.Context,
	queryName, dbName, queryKey string, loader Loader,
	params map[string]string, db *Database) ([]byte, error) {

	query, ok := (q.Configuration.Query)[queryName]
	if !ok {
		return nil, fmt.Errorf("unknown query '%s'", queryName)
	}

	// try to get item from cache
	if query.CachingTime > 0 {
		q.cacheLock.Lock()
		ci, ok := q.cache[queryKey]
		q.cacheLock.Unlock()
		if ok && ci.expireTS.After(time.Now()) {
			queryCacheHits.Inc()
			return ci.content, nil
		}
	}

	// get rows
	result, err := loader.Query(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	queryDuration.WithLabelValues(queryName, dbName).Observe(result.duration)

	// format metrics
	output, err := formatResult(ctx, db, query, dbName, queryName, result)
	if err != nil {
		return nil, fmt.Errorf("format result error: %w", err)
	}

	if query.CachingTime > 0 {
		// update cache
		q.cacheLock.Lock()
		q.cache[queryKey] = &cacheItem{
			expireTS: time.Now().Add(time.Duration(query.CachingTime) * time.Second),
			content:  output,
		}
		q.cacheLock.Unlock()
	}

	return output, nil
}

func (q *queryHandler) waitQueryFinish(queryKey string) (ok bool) {
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

func (q queryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	l := log.Ctx(ctx)

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
		db, ok := q.Configuration.Database[dbName]
		if !ok {
			l.Warn().Msgf("unknown database '%s'", dbName)
			queryRequestErrors.Inc()
			continue
		}

		lDb := l.With().Str("db", dbName).Logger()
		loader, err := GetLoader(db)
		if loader != nil {
			defer loader.Close(lDb.WithContext(ctx))
		}
		if err != nil {
			lDb.Error().Err(err).Msg("get loader error")
			queryRequestErrors.Inc()
			continue
		}

		lDb.Debug().Str("loader", loader.String()).Msg("loader created")

		for _, queryName := range queryNames {
			queryKey := queryName + "\t" + dbName
			lQuery := lDb.With().Str("query", queryName).Logger()
			ctxQuery := lQuery.WithContext(ctx)

			// check is previous query finished; if yes - lock it
			if !q.waitQueryFinish(queryKey) {
				lQuery.Warn().Msg("timeout while waiting for query finish")
				continue
			}

			queryRequest.WithLabelValues(queryName, dbName).Inc()

			var timeout time.Duration
			if db.Timeout > 0 {
				timeout = time.Duration(db.Timeout) * time.Second
			} else {
				timeout = time.Duration(5) * time.Minute
			}

			ctx, cancel := context.WithTimeout(ctxQuery, timeout)
			output, err := q.makeQuery(ctx, queryName, dbName, queryKey, loader, params, db)
			cancel()

			// mark query finished
			q.runningQueryLock.Lock()
			q.runningQuery[queryKey] = time.Time{}
			q.runningQueryLock.Unlock()

			if err != nil {
				queryRequestErrors.Inc()
				lQuery.Warn().Err(err).Msg("query error")
				continue
			}

			w.Write([]byte(fmt.Sprintf("## query %s\n", queryName)))
			w.Write(output)

			anyProcessed = true
		}
	}
	if !anyProcessed {
		http.Error(w, "error", 400)
	}
}

type infoHndler struct {
	Configuration *Configuration
}

func (q infoHndler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if !strings.HasPrefix(r.RemoteAddr, "127.0.0.1:") && !strings.HasPrefix(r.RemoteAddr, "localhost:") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	w.Write([]byte("DATABASES\n========="))
	for name, db := range q.Configuration.Database {
		w.Write([]byte{'\n'})
		w.Write([]byte(name))
		w.Write([]byte("\n-----"))
		w.Write([]byte("\n - driver: "))
		w.Write([]byte(db.Driver))
		w.Write([]byte("\n - connection:\n"))
		for k, v := range db.Connection {
			w.Write([]byte("    - "))
			w.Write([]byte(k))
			w.Write([]byte(": "))
			if strings.HasPrefix(strings.ToLower(k), "pass") {
				w.Write([]byte("*"))
			} else {
				w.Write([]byte(fmt.Sprintf("%v", v)))
			}
			w.Write([]byte{'\n'})
		}
		w.Write([]byte(" - labels:\n"))
		for k, v := range db.Labels {
			w.Write([]byte("    - "))
			w.Write([]byte(k))
			w.Write([]byte(": "))
			w.Write([]byte(fmt.Sprintf("%v", v)))
			w.Write([]byte{'\n'})
		}
	}

	w.Write([]byte("\n\nQueries\n======="))
	for name, q := range q.Configuration.Query {
		w.Write([]byte{'\n'})
		w.Write([]byte(name))
		w.Write([]byte("\n-----"))
		w.Write([]byte("\n - sql: "))
		w.Write([]byte(q.SQL))
		w.Write([]byte("\n - caching time: "))
		w.Write([]byte(fmt.Sprintf("%v", q.CachingTime)))
		w.Write([]byte("\n - metrics:\n"))
		w.Write([]byte(q.Metrics))
		w.Write([]byte("\n - params:\n"))
		for k, v := range q.Params {
			w.Write([]byte("    - "))
			w.Write([]byte(k))
			w.Write([]byte(": "))
			w.Write([]byte(fmt.Sprintf("%v", v)))
			w.Write([]byte{'\n'})
		}
	}
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stdout, version.Print("DBQuery exporter"))
		os.Exit(0)
	}

	InitializeLogger(*loglevel, *logformat)
	Logger.Info().
		Str("version", version.Info()).
		Str("build_ctx", version.BuildContext()).
		Msg("Starting DBQuery exporter")

	c, err := loadConfiguration(*configFile)
	if err != nil {
		Logger.Fatal().Err(err).Str("file", *configFile).Msg("Error parsing config file")
	}

	handler := newQueryHandler(c)
	iHandler := infoHndler{Configuration: c}

	// handle hup for reloading configuration
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if newConf, err := loadConfiguration(*configFile); err == nil {
				handler.Configuration = newConf
				handler.clearCache()
				iHandler.Configuration = newConf
				log.Info().Msg("configuration reloaded")
			} else {
				Logger.Error().Err(err).Msg("reloading configuration error; using old configuration")
			}
		}
	}()

	reqDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dbquery_exporter_request_duration_seconds",
			Help:    "A histogram of latencies for requests.",
			Buckets: []float64{.5, 1, 10, 30, 60, 120, 300},
		},
		[]string{"handler"},
	)
	prometheus.MustRegister(reqDuration)

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/query",
		newLogMiddleware(
			promhttp.InstrumentHandlerDuration(
				reqDuration.MustCurryWith(prometheus.Labels{"handler": "query"}),
				handler), "query"))
	http.Handle("/info",
		newLogMiddleware(
			promhttp.InstrumentHandlerDuration(
				reqDuration.MustCurryWith(prometheus.Labels{"handler": "info"}),
				iHandler), "info"))
	Logger.Info().Msgf("Listening on %s", *listenAddress)
	if err := http.ListenAndServe(*listenAddress, nil); err != nil {
		Logger.Fatal().Err(err).Msg("Listen and serve failed")
	}
}
