package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	// _ "github.com/denisenkom/go-mssqldb"
	// _ "github.com/go-sql-driver/mysql".
	_ "github.com/glebarez/go-sqlite"
	_ "github.com/lib/pq"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/rs/zerolog/log"
	_ "github.com/sijms/go-ora/v2"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/handlers"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

func init() {
	prometheus.MustRegister(version.NewCollector("dbquery_exporter"))
}

// Main is main function for cli.
func main() { //nolint:funlen
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
		disableCache   = flag.Bool("no-cache", false, "Disable query result caching")
		validateOutput = flag.Bool("validate-output", false, "Enable output validation")
	)

	flag.Parse()

	if *showVersion {
		_, _ = fmt.Println(version.Print("DBQuery exporter")) //nolint:forbidigo

		os.Exit(0)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	support.InitializeLogger(*loglevel, *logformat)
	log.Logger.Info().
		Str("version", version.Info()).
		Str("build_ctx", version.BuildContext()).
		Msg("Starting DBQuery exporter")

	collectors.Init()

	cfg, err := conf.LoadConfiguration(*configFile)
	if err != nil {
		log.Logger.Fatal().Err(err).Str("file", *configFile).
			Msg("load config file error")
	}

	metrics.UpdateConfLoadTime()

	if collectors.CollectorsPool == nil {
		panic("database pool not configured")
	}

	collectors.CollectorsPool.UpdateConf(cfg)

	webHandler := handlers.NewWebHandler(cfg, *listenAddress, *webConfig, *disableCache, *validateOutput)

	var runGroup run.Group
	{
		// Termination handler.
		term := make(chan os.Signal, 1)
		signal.Notify(term, os.Interrupt, syscall.SIGTERM)
		runGroup.Add(
			func() error {
				<-term
				log.Logger.Warn().Msg("Received SIGTERM, exiting...")
				collectors.CollectorsPool.Close()

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
		runGroup.Add(
			func() error {
				for range hup {
					log.Info().Msg("reloading configuration")

					if newConf, err := conf.LoadConfiguration(*configFile); err == nil {
						webHandler.ReloadConf(newConf)
						collectors.CollectorsPool.UpdateConf(newConf)
						metrics.UpdateConfLoadTime()
						log.Info().Msg("configuration reloaded")
					} else {
						log.Logger.Error().Err(err).
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

	runGroup.Add(webHandler.Run, webHandler.Close)

	if err := runGroup.Run(); err != nil {
		log.Logger.Fatal().Err(err).Msg("Start failed")
		os.Exit(1)
	}

	log.Logger.Info().Msg("finished..")
}
