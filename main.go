package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"time"

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
	queryRequestErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dbquery_request_errors_total",
			Help: "Errors in requests to the DBQuery exporter",
		},
	)
)

type (
	record map[string]interface{}

	// Result keep query result and some metadata parsed to template
	Result struct {
		// Records (rows)
		R              []record
		QueryStartTime int64
		QueryDuration  float64
		Count          int
		Query          string
		Database       string
	}
)

func init() {
	prometheus.MustRegister(queryDuration)
	prometheus.MustRegister(queryRequestErrors)
	prometheus.MustRegister(version.NewCollector("dbquery_exporter"))
}

func sendQuery(q *Query, d *Database) ([]record, error) {
	payload, err := json.Marshal(map[string]interface{}{
		"driver":     d.Driver,
		"connection": d.Connection,
		"sql":        q.SQL,
		"params":     q.Params,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", d.AgentURL, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}

	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if err == nil && resp.StatusCode != 200 {
		b, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("query error (%s) %s", resp.Status, string(b))
	}

	var records []record

	defer resp.Body.Close()
	if err = json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, err
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
		http.Error(w, fmt.Sprintf("Unknown query '%s'", queryName), 400)
		queryRequestErrors.Inc()
		return
	}

	db := q.Configuration.Database[query.Database]

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

	if err != nil {
		log.Errorf("query error for '%s': %s", queryName, err)
		http.Error(w, fmt.Sprintf("Query error: '%s'", err), 400)
		queryRequestErrors.Inc()
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	err = query.MetricTpl.Execute(w, result)
	if err != nil {
		log.Errorf("execute template error: %v", err)
		http.Error(w, fmt.Sprintf("Internal error: '%s'", err), 400)
		queryRequestErrors.Inc()
		return
	}

	duration := float64(time.Since(start).Seconds())
	queryDuration.WithLabelValues(queryName).Observe(duration)
	log.Debugf("Scrape of query '%s' took %f seconds", queryName, duration)
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
		queryDuration.WithLabelValues(query)
	}

	http.Handle("/metrics", promhttp.Handler())
	handler := queryHandler{c}
	http.HandleFunc("/query", handler.handler)
	log.Infof("Listening on %s", *listenAddress)
	log.Fatal(http.ListenAndServe(*listenAddress, nil))
}
