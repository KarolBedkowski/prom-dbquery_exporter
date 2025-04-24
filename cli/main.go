package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	// _ "github.com/denisenkom/go-mssqldb"
	// _ "github.com/go-sql-driver/mysql".
	// _ "github.com/sijms/go-ora/v2".
	_ "github.com/glebarez/go-sqlite"
	_ "github.com/lib/pq"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	cversion "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/version"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/scheduler"
	"prom-dbquery_exporter.app/internal/server"
	"prom-dbquery_exporter.app/internal/support"
)

func init() {
	prometheus.MustRegister(cversion.NewCollector("dbquery_exporter"))
}

// Main is main function for cli.
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
		disableCache            = flag.Bool("no-cache", false, "Disable query result caching")
		enableSchedulerParallel = flag.Bool("parallel-scheduler", false, "Run scheduler ask parallel")
		validateOutput          = flag.Bool("validate-output", false, "Enable output validation")
	)

	flag.Parse()

	if *showVersion {
		fmt.Println(version.Print("DBQuery exporter")) //nolint:forbidigo
		os.Exit(0)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	support.InitializeLogger(*loglevel, *logformat)
	log.Logger.Info().
		Str("version", version.Info()).
		Str("build_ctx", version.BuildContext()).
		Msg("Starting DBQuery exporter")

	if err := enableSDNotify(); err != nil {
		log.Logger.Warn().Err(err).Msg("initialize systemd error")
	}

	collectors.Init()

	cfg, err := conf.LoadConfiguration(*configFile)
	if err != nil {
		log.Logger.Fatal().Err(err).Str("file", *configFile).
			Msg("load config file error")
	}

	log.Logger.Debug().Interface("configuration", cfg).Msg("configuration loaded")
	metrics.UpdateConfLoadTime()

	if collectors.CollectorsPool == nil {
		panic("database pool not configured")
	}

	collectors.CollectorsPool.UpdateConf(cfg)

	if err := start(cfg, *configFile, *listenAddress, *webConfig, *disableCache, *validateOutput,
		*enableSchedulerParallel); err != nil {
		log.Logger.Fatal().Err(err).Msg("Start failed")
		os.Exit(1)
	}

	log.Logger.Info().Msg("finished..")
}

func start(cfg *conf.Configuration, configFile, listenAddress, webConfig string,
	disableCache, validateOutput bool, enableSchedulerParallel bool,
) error {
	cache := support.NewCache[[]byte]("query_cache")
	webHandler := server.NewWebHandler(cfg, listenAddress, webConfig, disableCache, validateOutput, cache)
	sched := scheduler.NewScheduler(cache, cfg)

	var runGroup run.Group

	// Termination handler.
	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	runGroup.Add(
		func() error {
			<-term
			log.Logger.Warn().Msg("Received SIGTERM, exiting...")
			daemon.SdNotify(false, daemon.SdNotifyStopping) //nolint:errcheck
			collectors.CollectorsPool.Close()

			return nil
		},
		func(_ error) {
			close(term)
		},
	)

	// Reload handler.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	runGroup.Add(
		func() error {
			for range hup {
				log.Info().Msg("reloading configuration")

				if newConf, err := conf.LoadConfiguration(configFile); err == nil {
					webHandler.ReloadConf(newConf)
					sched.ReloadConf(newConf)
					collectors.CollectorsPool.UpdateConf(newConf)
					metrics.UpdateConfLoadTime()
					log.Info().Interface("configuration", newConf).Msg("configuration reloaded")
				} else {
					log.Logger.Error().Err(err).
						Msg("reloading configuration error; using old configuration")
				}
			}

			return nil
		},
		func(_ error) {
			close(hup)
		},
	)

	runGroup.Add(webHandler.Run, webHandler.Close)

	if enableSchedulerParallel {
		runGroup.Add(sched.RunParallel, sched.Close)
	} else {
		runGroup.Add(sched.Run, sched.Close)
	}

	daemon.SdNotify(false, daemon.SdNotifyReady) //nolint:errcheck
	daemon.SdNotify(false, "STATUS=ready")       //nolint:errcheck

	return runGroup.Run() //nolint:wrapcheck
}

func enableSDNotify() error {
	ok, err := daemon.SdNotify(false, "STATUS=starting")
	if err != nil {
		return fmt.Errorf("send sd status error: %w", err)
	}

	// not running under systemd?
	if !ok {
		return nil
	}

	interval, err := daemon.SdWatchdogEnabled(false)
	if err != nil {
		return fmt.Errorf("enable sdwatchdog error: %w", err)
	}

	// watchdog disabled?
	if interval == 0 {
		return nil
	}

	go func(interval time.Duration) {
		tick := time.Tick(interval)
		for range tick {
			_, _ = daemon.SdNotify(false, daemon.SdNotifyWatchdog)
		}
	}(interval / 2) //nolint:mnd

	return nil
}
