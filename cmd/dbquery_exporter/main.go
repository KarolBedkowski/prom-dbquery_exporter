package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	//	_ "net/http/pprof"

	// _ "github.com/denisenkom/go-mssqldb"
	// _ "github.com/go-sql-driver/mysql"
	// _ "github.com/mattn/go-oci8"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"github.com/oklog/run"
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
		configFile  = flag.String("config.file", "dbquery.yaml",
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

	runtime.GOMAXPROCS(runtime.NumCPU())

	InitializeLogger(*loglevel, *logformat)
	Logger.Info().
		Str("version", version.Info()).
		Str("build_ctx", version.BuildContext()).
		Msg("Starting DBQuery exporter")

	c, err := loadConfiguration(*configFile)
	if err != nil {
		Logger.Fatal().Err(err).Str("file", *configFile).Msg("load config file error")
	}

	webHandler := newWebHandler(c, *listenAddress, *webConfig)

	var g run.Group
	{
		// Termination handler.
		term := make(chan os.Signal, 1)
		signal.Notify(term, os.Interrupt, syscall.SIGTERM)
		g.Add(
			func() error {
				<-term
				Logger.Warn().Msg("Received SIGTERM, exiting...")
				CloseLoaders()
				return nil
			},
			func(err error) {
				close(term)
			},
		)
	}
	{
		// Reload handler.
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		g.Add(
			func() error {
				for range hup {
					log.Info().Msg("reloading configuration")
					if newConf, err := loadConfiguration(*configFile); err == nil {
						webHandler.ReloadConf(newConf)
						UpdateConfiguration(newConf)
						log.Info().Msg("configuration reloaded")
					} else {
						Logger.Error().Err(err).Msg("reloading configuration error; using old configuration")
					}
				}
				return nil
			},
			func(err error) {
				close(hup)
			},
		)
	}

	g.Add(webHandler.Run, webHandler.Close)

	if err := g.Run(); err != nil {
		Logger.Error().Err(err).Msg("Start failed")
		os.Exit(1)
	}

	Logger.Info().Msg("finished..")
}

type webHandler struct {
	handler       *QueryHandler
	infoHandler   *infoHandler
	server        *http.Server
	listenAddress string
	webConfig     string
}

func newWebHandler(c *Configuration, listenAddress string, webConfig string) *webHandler {
	wh := &webHandler{
		handler:       NewQueryHandler(c),
		infoHandler:   &infoHandler{Configuration: c},
		listenAddress: listenAddress,
		webConfig:     webConfig,
	}

	reqDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: MetricsNamespace,
			Name:      "request_duration_seconds",
			Help:      "A histogram of latencies for requests.",
			Buckets:   []float64{0.5, 1, 5, 10, 60, 120},
		},
		[]string{"handler"},
	)

	prometheus.MustRegister(reqDuration)

	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))
	http.Handle("/query",
		newLogMiddleware(
			promhttp.InstrumentHandlerDuration(
				reqDuration.MustCurryWith(prometheus.Labels{"handler": "query"}),
				wh.handler), "query", false))
	http.Handle("/info",
		newLogMiddleware(
			promhttp.InstrumentHandlerDuration(
				reqDuration.MustCurryWith(prometheus.Labels{"handler": "info"}),
				wh.infoHandler), "info", true))

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return wh
}

func (w *webHandler) Run() error {
	Logger.Info().Msgf("Listening on %s", w.listenAddress)
	w.server = &http.Server{Addr: w.listenAddress}
	if err := listenAndServe(w.server, w.webConfig); err != nil {
		return fmt.Errorf("listen and serve failed: %w", err)
	}
	return nil
}

func (w *webHandler) Close(err error) {
	Logger.Debug().Msg("web handler close")
	w.server.Close()
}

func (w *webHandler) ReloadConf(newConf *Configuration) {
	w.handler.SetConfiguration(newConf)
	w.infoHandler.Configuration = newConf
}
