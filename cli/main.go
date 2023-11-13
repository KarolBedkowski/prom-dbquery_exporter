package cli

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/db"
	"prom-dbquery_exporter.app/internal/handlers"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"

	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
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
		validateOutput  = flag.Bool("validate-output", false, "Enable output validation")
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
		support.Logger.Fatal().Err(err).Str("file", *configFile).
			Msg("load config file error")
	}
	metrics.UpdateConfLoadTime()

	webHandler := handlers.NewWebHandler(c, *listenAddress, *webConfig, *disableParallel,
		*disableCache, *validateOutput)

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
						support.Logger.Error().Err(err).
							Msg("reloading configuration error; using old configuration")
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
		support.Logger.Fatal().Err(err).Msg("Start failed")
		os.Exit(1)
	}

	support.Logger.Info().Msg("finished..")
}
