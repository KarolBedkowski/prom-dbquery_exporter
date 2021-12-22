package cli

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"prom-dbquery_exporter.app/conf"
	"prom-dbquery_exporter.app/db"
	"prom-dbquery_exporter.app/handlers"
	"prom-dbquery_exporter.app/metrics"
	"prom-dbquery_exporter.app/support"

	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/rs/zerolog/log"
)

func init() {
	prometheus.MustRegister(version.NewCollector("dbquery_exporter"))
}

// Main is main function for cli
func Main() {
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
		disableParallel = flag.Bool("no-parallel-query", false, "Disable parallel queries")
		disableCache    = flag.Bool("no-cache", false, "Disable query result caching")
	)
	flag.Parse()

	if *showVersion {
		_, _ = fmt.Println(version.Print("DBQuery exporter"))
		os.Exit(0)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	support.InitializeLogger(*loglevel, *logformat)
	support.Logger.Info().
		Str("version", version.Info()).
		Str("build_ctx", version.BuildContext()).
		Msg("Starting DBQuery exporter")

	c, err := conf.LoadConfiguration(*configFile)
	if err != nil {
		support.Logger.Fatal().Err(err).Str("file", *configFile).Msg("load config file error")
	}
	metrics.UpdateConfLoadTime()

	webHandler := newWebHandler(c, *listenAddress, *webConfig, *disableParallel,
		*disableCache)

	var g run.Group
	{
		// Termination handler.
		term := make(chan os.Signal, 1)
		signal.Notify(term, os.Interrupt, syscall.SIGTERM)
		g.Add(
			func() error {
				<-term
				support.Logger.Warn().Msg("Received SIGTERM, exiting...")
				db.CloseLoaders()
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
					if newConf, err := conf.LoadConfiguration(*configFile); err == nil {
						webHandler.ReloadConf(newConf)
						db.UpdateConfiguration(newConf)
						metrics.UpdateConfLoadTime()
						log.Info().Msg("configuration reloaded")
					} else {
						support.Logger.Error().Err(err).Msg("reloading configuration error; using old configuration")
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
		support.Logger.Error().Err(err).Msg("Start failed")
		os.Exit(1)
	}

	support.Logger.Info().Msg("finished..")
}

type webHandler struct {
	handler       *handlers.QueryHandler
	infoHandler   *handlers.InfoHandler
	server        *http.Server
	listenAddress string
	webConfig     string
}

func newWebHandler(c *conf.Configuration, listenAddress string, webConfig string,
	disableParallel bool, disableCache bool) *webHandler {
	wh := &webHandler{
		handler:       handlers.NewQueryHandler(c, disableParallel, disableCache),
		infoHandler:   &handlers.InfoHandler{Configuration: c},
		listenAddress: listenAddress,
		webConfig:     webConfig,
	}

	reqDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: support.MetricsNamespace,
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
		handlers.NewLogMiddleware(
			promhttp.InstrumentHandlerDuration(
				reqDuration.MustCurryWith(prometheus.Labels{"handler": "query"}),
				wh.handler), "query", false))
	http.Handle("/info",
		handlers.NewLogMiddleware(
			promhttp.InstrumentHandlerDuration(
				reqDuration.MustCurryWith(prometheus.Labels{"handler": "info"}),
				wh.infoHandler), "info", true))

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return wh
}

func (w *webHandler) Run() error {
	support.Logger.Info().Msgf("Listening on %s", w.listenAddress)
	w.server = &http.Server{Addr: w.listenAddress}
	if err := handlers.ListenAndServe(w.server, w.webConfig); err != nil {
		return fmt.Errorf("listen and serve failed: %w", err)
	}
	return nil
}

func (w *webHandler) Close(err error) {
	support.Logger.Debug().Msg("web handler close")
	w.server.Close()
}

func (w *webHandler) ReloadConf(newConf *conf.Configuration) {
	w.handler.SetConfiguration(newConf)
	w.infoHandler.Configuration = newConf
}
