package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"github.com/pkg/errors"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	//_ "github.com/denisenkom/go-mssqldb"
	//_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	//_ "github.com/mattn/go-oci8"
	_ "github.com/mattn/go-sqlite3"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/version"
)

var (
	showVersion = flag.Bool("version", false, "Print version information.")
	configFile  = flag.String("config.file", "dbquery.yml",
		"Path to configuration file.")
	listenAddress = flag.String("web.listen-address", ":9122",
		"Address to listen on for web interface and telemetry.")

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
	requestID uint64
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
	prometheus.MustRegister(version.NewCollector("dbquery_exporter"))
}

func formatResult(db *Database, query *Query, dbName, queryName string, result *queryResult) ([]byte, error) {
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

	log.With("query", queryName).
		With("db", dbName).
		Debugf("query_time=%f rows=%d", data.QueryDuration, data.Count)

	var output bytes.Buffer
	bw := bufio.NewWriter(&output)

	err := query.MetricTpl.Execute(bw, data)
	if err != nil {
		return nil, errors.Wrap(err, "execute template error")
	}

	bw.Flush()

	b := bytes.TrimLeft(output.Bytes(), "\n\r\t ")

	return b, nil
}

type queryHandler struct {
	Configuration *Configuration
	cache         map[string]*cacheItem
	cacheLock     sync.Mutex

	runningQuery     map[string]bool
	runningQueryLock sync.Mutex
}

func NewQueryHandler(c *Configuration) *queryHandler {
	return &queryHandler{
		Configuration: c,
		cache:         make(map[string]*cacheItem),
		runningQuery:  make(map[string]bool),
	}
}

func (q *queryHandler) clearCache() {
	q.cacheLock.Lock()
	defer q.cacheLock.Unlock()
	q.cache = make(map[string]*cacheItem)
}

func (q queryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	requestID := atomic.AddUint64(&requestID, 1)
	log.With("req_id", requestID).
		Infof("request remote='%s', url='%s'", r.RemoteAddr, r.URL)

	queryNames := r.URL.Query()["query"]
	dbNames := r.URL.Query()["database"]
	params := make(map[string]string)
	for k, v := range r.URL.Query() {
		if k != "query" && k != "database" {
			params[k] = v[0]
		}
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	anySuccess := false
	for _, dbName := range dbNames {
		db, ok := q.Configuration.Database[dbName]
		if !ok {
			log.With("req_id", requestID).
				Errorf("unknown database='%s'", dbName)
			queryRequestErrors.Inc()
			continue
		}

		loader, err := GetLoader(db)
		if err != nil {
			log.With("req_id", requestID).
				With("db", dbName).
				Errorf("get loader error '%s'", err)
			queryRequestErrors.Inc()
			continue
		}

		defer loader.Close()

	queryLoop:
		for _, queryName := range queryNames {
			queryKey := queryName + "\t" + dbName

			// TODO: configure
			for i := 120; ; i-- { // 10min
				q.runningQueryLock.Lock()
				running, ok := q.runningQuery[queryKey]
				if !ok || !running {
					q.runningQuery[queryKey] = true
					q.runningQueryLock.Unlock()
					break
				}
				q.runningQueryLock.Unlock()
				if i == 0 {
					log.Warn("timeout while waiting for query finish")
					continue queryLoop
				}
				time.Sleep(time.Duration(5) * time.Second)
				//log.Debugf("waiting %d", i)
			}

			query, ok := (q.Configuration.Query)[queryName]
			if !ok {
				log.With("req_id", requestID).
					With("db", dbName).
					Errorf("unknown query '%s'", queryName)
				queryRequestErrors.Inc()

				q.runningQueryLock.Lock()
				q.runningQuery[queryKey] = false
				q.runningQueryLock.Unlock()
				continue
			}

			queryRequest.WithLabelValues(queryName, dbName).Inc()

			// try to get item from cache
			if query.CachingTime > 0 {
				q.cacheLock.Lock()
				ci, ok := q.cache[queryKey]
				q.cacheLock.Unlock()
				if ok && ci.expireTS.After(time.Now()) {
					log.With("req_id", requestID).
						With("query", queryName).
						With("db", dbName).
						Debugf("cache hit")
					w.Write(ci.content)
					anySuccess = true
					q.runningQueryLock.Lock()
					q.runningQuery[queryKey] = false
					q.runningQueryLock.Unlock()
					continue
				}
			}

			// get rows
			result, err := loader.Query(query, params)
			if err != nil {
				log.With("req_id", requestID).
					With("query", queryName).
					With("db", dbName).
					Errorf("query error: %s", err)
				queryRequestErrors.Inc()
				q.runningQueryLock.Lock()
				q.runningQuery[queryKey] = false
				q.runningQueryLock.Unlock()
				continue
			}

			time.Sleep(time.Duration(30) * time.Second)

			// format metrics
			output, err := formatResult(db, query, dbName, queryName, result)
			if err != nil {
				log.With("req_id", requestID).
					With("query", queryName).
					With("db", dbName).
					Errorf("format result error: %s", err)
				queryRequestErrors.Inc()
				q.runningQueryLock.Lock()
				q.runningQuery[queryKey] = false
				q.runningQueryLock.Unlock()
				continue
			}
			w.Write([]byte(fmt.Sprintf("## query %s\n", queryName)))
			w.Write(output)

			queryDuration.WithLabelValues(queryName, dbName).Observe(result.duration)

			if query.CachingTime > 0 {
				// update cache
				q.cacheLock.Lock()
				q.cache[queryKey] = &cacheItem{
					expireTS: time.Now().Add(time.Duration(query.CachingTime) * time.Second),
					content:  output,
				}
				q.cacheLock.Unlock()
			}

			q.runningQueryLock.Lock()
			q.runningQuery[queryKey] = false
			q.runningQueryLock.Unlock()
			anySuccess = true
		}
	}
	if !anySuccess {
		http.Error(w, "error", 400)
	}
	log.With("req_id", requestID).Debugf("done")
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
		w.Write([]byte("\n"))
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
			w.Write([]byte("\n"))
		}
		w.Write([]byte(" - labels:\n"))
		for k, v := range db.Labels {
			w.Write([]byte("    - "))
			w.Write([]byte(k))
			w.Write([]byte(": "))
			w.Write([]byte(fmt.Sprintf("%v", v)))
			w.Write([]byte("\n"))
		}
	}

	w.Write([]byte("\n\nQueries\n======="))
	for name, q := range q.Configuration.Query {
		w.Write([]byte("\n"))
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
			w.Write([]byte("\n"))
		}
	}
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stdout, version.Print("DBQuery exporter"))
		os.Exit(0)
	}

	log.Infoln("Starting DBQuery exporter", version.Info())
	log.Infoln("Build context", version.BuildContext())

	c, err := loadConfiguration(*configFile)
	if err != nil {
		log.Fatalf("Error parsing config file: %s", err)
	}
	handler := NewQueryHandler(c)
	iHandler := infoHndler{Configuration: c}

	// handle hup for reloading configuration
	hup := make(chan os.Signal)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-hup:
				if newConf, err := loadConfiguration(*configFile); err == nil {
					handler.Configuration = newConf
					handler.clearCache()
					iHandler.Configuration = newConf
					log.Info("configuration reloaded")
				} else {
					log.Errorf("reloading configuration err: %s", err)
					log.Errorf("using old configuration")
				}
			}
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/query", prometheus.InstrumentHandler("query", handler))
	http.Handle("/info", prometheus.InstrumentHandler("info", iHandler))
	log.Infof("Listening on %s", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
