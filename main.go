package main

import (
	"flag"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
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
		Query          string
		Database       string
	}
)

func init() {
	prometheus.MustRegister(queryDuration)
	prometheus.MustRegister(queryRequest)
	prometheus.MustRegister(queryRequestErrors)
	prometheus.MustRegister(version.NewCollector("dbquery_exporter"))
}

func sendQuery(q *Query, d *Database) ([]sqlagent.Record, error) {
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

	var records []sqlagent.Record
	for iter.Next() {
		rec := make(sqlagent.Record)
		if err := iter.Scan(rec); err != nil {
			return nil, fmt.Errorf("error scanning record: %s", err)
		}
		records = append(records, rec)
	}

	return records, nil
}

type queryHandler struct {
	Configuration *Configuration
}

func (q *queryHandler) handler(w http.ResponseWriter, r *http.Request) {
	queryName := r.URL.Query().Get("query")
	query, ok := (q.Configuration.Query)[queryName]
	if !ok {
		log.Errorf("query='%s' unknown query", queryName)
		http.Error(w, fmt.Sprintf("Unknown query '%s'", queryName), 400)
		queryRequestErrors.Inc()
		return
	}

	queryRequest.WithLabelValues(queryName).Inc()

	db := q.Configuration.Database[query.Database]

	log.Debugf("query='%s' start", queryName)

	result := &Result{
		Query:    queryName,
		Database: query.Database,
	}
	var err error
	start := time.Now()
	result.R, err = sendQuery(query, db)
	result.QueryStartTime = start.Unix()
	result.QueryDuration = time.Since(start).Seconds()
	result.Count = len(result.R)

	log.Debugf("query='%s' query_time=%f rows=%d",
		queryName, result.QueryDuration, result.Count)

	if err != nil {
		log.Errorf("query='%s' execute error: %s", queryName, err)
		http.Error(w, fmt.Sprintf("Query error: '%s'", err), 400)
		queryRequestErrors.Inc()
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	err = query.MetricTpl.Execute(w, result)
	if err != nil {
		log.Errorf("query='%s' execute template error: %v", queryName, err)
		http.Error(w, fmt.Sprintf("Internal error: '%s'", err), 400)
		queryRequestErrors.Inc()
		return
	}

	duration := float64(time.Since(start).Seconds())
	queryDuration.WithLabelValues(queryName).Observe(duration)
	log.Debugf("query='%s' scrape_time=%f", queryName, duration)
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

	for query := range c.Query {
		log.Debugf("found query '%s'", query)
		queryRequest.WithLabelValues(query)
		queryDuration.WithLabelValues(query)
	}

	http.Handle("/metrics", promhttp.Handler())
	handler := queryHandler{c}
	http.HandleFunc("/query", handler.handler)
	log.Infof("Listening on %s", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
