package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	//      _ "github.com/denisenkom/go-mssqldb"
	//      _ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	//      _ "github.com/mattn/go-oci8"
	_ "github.com/mattn/go-sqlite3"

	"github.com/chop-dbhi/sql-agent"
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
			Name: "dbquery_collection_duration_seconds",
			Help: "Duration of collections by the DBQuery exporter",
		},
		[]string{"query"},
	)
	queryRequest = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dbquery_request_total",
			Help: "Total numbers requests given query",
		},
		[]string{"query"},
	)
	queryRequestErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dbquery_request_errors_total",
			Help: "Errors in requests to the DBQuery exporter",
		},
	)
)

type (
	// Result keep query result and some metadata parsed to template
	Result struct {
		// Records (rows)
		R              []sqlagent.Record
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

	queryResult struct {
		records  []sqlagent.Record
		duration float64
		start    time.Time
	}
)

func init() {
	prometheus.MustRegister(queryDuration)
	prometheus.MustRegister(queryRequest)
	prometheus.MustRegister(queryRequestErrors)
	prometheus.MustRegister(version.NewCollector("dbquery_exporter"))
}

func queryDatabase(q *Query, d *Database) (*queryResult, error) {
	start := time.Now()

	if _, ok := sqlagent.Drivers[d.Driver]; !ok {
		return nil, fmt.Errorf("unsupported driver '%s'", d.Driver)
	}

	db, err := sqlagent.PersistentConnect(d.Driver, d.Connection)
	if err != nil {
		return nil, fmt.Errorf("error connecting to db: %s", err)
	}

	iter, err := sqlagent.Execute(db, q.SQL, q.Params)
	if err != nil {
		return nil, fmt.Errorf("error executing query: %s", err)
	}

	defer iter.Close()

	result := &queryResult{
		start: start,
	}

	for iter.Next() {
		rec := make(sqlagent.Record)
		if err := iter.Scan(rec); err != nil {
			return nil, fmt.Errorf("error scanning record: %s", err)
		}
		result.records = append(result.records, rec)
	}

	result.duration = float64(time.Since(start).Seconds())

	return result, nil
}

func formatResult(db *Database, query *Query, dbName, queryName string, result *queryResult) ([]byte, error) {
	data := &Result{
		Query:          queryName,
		Database:       dbName,
		R:              result.records,
		QueryStartTime: result.start.Unix(),
		QueryDuration:  result.duration,
		Count:          len(result.records),
	}

	log.Debugf("query='%s' db='%s' query_time=%f rows=%d",
		queryName, dbName, data.QueryDuration, data.Count)

	var output bytes.Buffer
	bw := bufio.NewWriter(&output)

	err := query.MetricTpl.Execute(bw, data)
	if err != nil {
		return nil, err
	}

	bw.Flush()

	return output.Bytes(), nil
}

type queryHandler struct {
	Configuration *Configuration
	cache         map[string]*cacheItem
}

func (q *queryHandler) handler(w http.ResponseWriter, r *http.Request) {

	log.Debugf("query: %s", r.URL)

	queryNames := r.URL.Query()["query"]
	dbNames := r.URL.Query()["database"]

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	anySuccess := false

	for _, queryName := range queryNames {
		query, ok := (q.Configuration.Query)[queryName]
		if !ok {
			log.Errorf("query='%s' unknown query", queryName)
			queryRequestErrors.Inc()
			continue
		}
		queryRequest.WithLabelValues(queryName).Inc()

		for _, dbName := range dbNames {
			db, ok := q.Configuration.Database[dbName]
			if !ok {
				log.Errorf("query='%s' unknown database='%s'", queryName, dbName)
				queryRequestErrors.Inc()
				continue
			}

			// try to get item from cache
			if query.CachingTime > 0 {
				ci, ok := q.cache[queryName+"\t"+dbName]
				if ok && ci.expireTS.After(time.Now()) {
					log.Debugf("query='%s' db='%s' cache hit", queryName, dbName)
					w.Write(ci.content)
					anySuccess = true
					continue
				}
			}

			// get rows
			result, err := queryDatabase(query, db)
			if err != nil {
				log.Errorf("query='%s' db='%s' query error: %s", queryName, dbName, err)
				queryRequestErrors.Inc()
				continue
			}

			// format metrics
			output, err := formatResult(db, query, dbName, queryName, result)
			if err != nil {
				log.Errorf("query='%s' db='%s' format result error: %s", queryName, dbName, err)
				queryRequestErrors.Inc()
				continue
			}

			w.Write(output)

			queryDuration.WithLabelValues(queryName).Observe(result.duration)

			if query.CachingTime > 0 {
				// update cache
				q.cache[queryName+"\t"+dbName] = &cacheItem{
					expireTS: time.Now().Add(time.Duration(query.CachingTime) * time.Second),
					content:  output,
				}
			}

			anySuccess = true
		}
	}
	if !anySuccess {
		http.Error(w, "error", 400)
	}
}

func onConfLoaded(c *Configuration) {
	for query := range c.Query {
		log.Debugf("found query '%s'", query)
		queryRequest.WithLabelValues(query)
		queryDuration.WithLabelValues(query)
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
	handler := queryHandler{c, make(map[string]*cacheItem)}
	onConfLoaded(c)

	hup := make(chan os.Signal)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-hup:
				if newConf, err := loadConfiguration(*configFile); err == nil {
					handler.Configuration = newConf
					onConfLoaded(newConf)
					log.Info("configuration reloaded")
				} else {
					log.Errorf("reloading configuration err: %s", err)
					log.Errorf("using old configuration")
				}
			}
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/query", handler.handler)
	log.Infof("Listening on %s", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
