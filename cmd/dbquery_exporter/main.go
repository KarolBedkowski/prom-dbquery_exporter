package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	//	_ "net/http/pprof"

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

func init() {
	prometheus.MustRegister(version.NewCollector("dbquery_exporter"))
}

func main() {
	var (
		showVersion = flag.Bool("version", false, "Print version information.")
		configFile  = flag.String("config.file", "dbquery.yml",
			"Path to configuration file.")
		listenAddress = flag.String("web.listen-address", ":9122",
			"Address to listen on for web interface and telemetry.")
		loglevel = flag.String("log.level", "info",
			"Logging level (debug, info, warn, error, fatal)")
		logformat = flag.String("log.format", "logfmt",
			"Logging log format (logfmt, json).")
		webConfig = flag.String("web.config", "",
			"Path to config yaml file that can enable TLS or authentication.")
	)
	flag.Parse()

	if *showVersion {
		_, _ = fmt.Println(version.Print("DBQuery exporter"))
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

	handler := NewQueryHandler(c)
	iHandler := infoHndler{Configuration: c}

	// handle hup for reloading configuration
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if newConf, err := loadConfiguration(*configFile); err == nil {
				handler.SetConfiguration(newConf)
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
				handler), "query", false))
	http.Handle("/info",
		newLogMiddleware(
			promhttp.InstrumentHandlerDuration(
				reqDuration.MustCurryWith(prometheus.Labels{"handler": "info"}),
				iHandler), "info", true))

	Logger.Info().Msgf("Listening on %s", *listenAddress)
	server := &http.Server{Addr: *listenAddress}
	if err := listenAndServe(server, *webConfig); err != nil {
		Logger.Fatal().Err(err).Msg("Listen and serve failed")
	}
}
