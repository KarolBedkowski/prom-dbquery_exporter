package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	cversion "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/version"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/db"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/scheduler"
	"prom-dbquery_exporter.app/internal/server"
	"prom-dbquery_exporter.app/internal/support"
)

func init() {
	prometheus.MustRegister(cversion.NewCollector("dbquery_exporter"))
}

func printVersion() {
	fmt.Println(version.Print("DBQuery exporter")) //nolint:forbidigo

	if sdb := db.SupportedDatabases(); len(sdb) == 0 {
		fmt.Println("NO DATABASES SUPPORTED, check compile flags.") //nolint:forbidigo
	} else {
		fmt.Printf("Supported databases: %s\n", sdb) //nolint:forbidigo
	}
}

func printWelcome() {
	log.Logger.Info().
		Str("version", version.Info()).
		Str("build_ctx", version.BuildContext()).
		Msg("Starting DBQuery exporter")

	if sdb := db.SupportedDatabases(); len(sdb) == 0 {
		log.Logger.Fatal().Msg("no databases supported, check compile flags")
	} else {
		log.Logger.Info().Msgf("supported databases: %s", sdb)
	}
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
		printVersion()
		os.Exit(0)
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	support.InitializeLogger(*loglevel, *logformat)
	printWelcome()

	if err := enableSDNotify(); err != nil {
		log.Logger.Warn().Err(err).Msg("initialize systemd error")
	}

	cfg, err := conf.LoadConfiguration(*configFile)
	if err != nil || cfg == nil {
		log.Logger.Fatal().Err(err).Str("file", *configFile).
			Msg("load config file error")
	}

	cfg.SetCliOptions(disableCache, enableSchedulerParallel, validateOutput)

	log.Logger.Debug().Interface("configuration", cfg).Msg("configuration loaded")
	metrics.UpdateConf()

	if err := start(cfg, *listenAddress, *webConfig); err != nil {
		log.Logger.Fatal().Err(err).Msg("Start failed")
		os.Exit(1)
	}

	log.Logger.Info().Msg("finished..")
}

func start(cfg *conf.Configuration, listenAddress, webConfig string) error {
	collectors, taskQueue := collectors.NewCollectors(cfg)
	defer close(taskQueue)

	cache := support.NewCache[[]byte]("query_cache")
	webHandler := server.NewWebHandler(cfg, listenAddress, webConfig, cache, taskQueue)
	sched := scheduler.NewScheduler(cache, cfg, taskQueue)
	runGroup := run.Group{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Collectors
	runGroup.Add(func() error { return collectors.Start(ctx) }, func(_ error) { cancel() })

	// Termination handler.
	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	runGroup.Add(
		func() error {
			<-term
			cancel()
			log.Logger.Warn().Msg("Received SIGTERM, exiting...")
			daemon.SdNotify(false, daemon.SdNotifyStopping) //nolint:errcheck

			return nil
		},
		func(_ error) { close(term) },
	)

	// Reload handler.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	runGroup.Add(
		func() error {
			for range hup {
				log.Info().Msg("reloading configuration")

				if newConf, err := conf.LoadConfiguration(cfg.ConfigFilename); err == nil {
					newConf.CopyRuntimeOptions(cfg)
					webHandler.UpdateConf(newConf)
					sched.UpdateConf(newConf)
					collectors.UpdateConf(newConf)
					metrics.UpdateConf()
					log.Info().Interface("configuration", newConf).Msg("configuration reloaded")
				} else {
					log.Logger.Error().Err(err).Msg("reloading configuration error; using old configuration")
				}
			}

			return nil
		},
		func(_ error) { close(hup) },
	)

	runGroup.Add(webHandler.Run, webHandler.Close)

	if cfg.ParallelScheduler {
		runGroup.Add(func() error { return sched.RunParallel(ctx) }, sched.Close)
	} else {
		runGroup.Add(func() error { return sched.Run(ctx) }, sched.Close)
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

	interval /= 2

	go func() {
		tick := time.Tick(interval)
		for range tick {
			_, _ = daemon.SdNotify(false, daemon.SdNotifyWatchdog)
		}
	}()

	return nil
}
