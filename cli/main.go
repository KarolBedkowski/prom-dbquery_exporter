package main

import (
	"context"
	"fmt"
	stdlog "log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	cversion "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/version"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/cache"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/db"
	"prom-dbquery_exporter.app/internal/scheduler"
	"prom-dbquery_exporter.app/internal/server"
	"prom-dbquery_exporter.app/internal/templates"
)

const AppName = "dbquery_exporter"

func init() {
	prometheus.MustRegister(cversion.NewCollector(AppName))
}

func printVersion() {
	fmt.Println(version.Print(AppName)) //nolint:forbidigo

	if sdb := db.GlobalRegistry.List(); len(sdb) == 0 {
		fmt.Println("NO DATABASES SUPPORTED, check compilation flags.") //nolint:forbidigo
	} else {
		fmt.Printf("Supported databases: %s\n", sdb) //nolint:forbidigo
	}

	fmt.Printf("Available template functions: %s\n", templates.AvailableTmplFunctions()) //nolint:forbidigo
}

func printWelcome() {
	log.Logger.Log().
		Str("version", version.Info()).
		Str("build_ctx", version.BuildContext()).
		Msgf("starting %s", AppName)

	if sdb := db.GlobalRegistry.List(); len(sdb) == 0 {
		log.Logger.Fatal().Msg("no databases supported, check compilation flags")
	} else {
		log.Logger.Log().Msgf("supported databases: %s", sdb)
	}

	log.Logger.Log().Msgf("available template functions: %s", templates.AvailableTmplFunctions())
}

// Main is main function for cli.
func main() {
	conf.ParseCliArgs()

	if conf.Args.ShowVersion {
		printVersion()
		os.Exit(0)
	}

	initializeLogger(conf.Args.LogLevel, conf.Args.LogFormat)
	printWelcome()
	log.Logger.Debug().Interface("args", conf.Args).Msg("cli arguments")

	if err := startSDWatchdog(); err != nil {
		log.Logger.Warn().Err(err).Msg("initialize systemd error")
	}

	cfg, err := conf.LoadConfiguration(conf.Args.ConfigFilename, db.GlobalRegistry)
	if err != nil || cfg == nil {
		log.Logger.Fatal().Err(err).Str("file", conf.Args.ConfigFilename).Msg("failed load config file")
	}

	log.Logger.Debug().Interface("conf", cfg).Msg("configuration loaded")

	if err := start(cfg); err != nil {
		log.Logger.Fatal().Err(err).Msg("start failed")
	}

	log.Logger.Info().Msg("finished.")
}

func start(cfg *conf.Configuration) error {
	collectors := collectors.NewCollectors(cfg)
	cache := cache.NewCache[[]byte]("query_cache")
	webHandler := server.NewWebHandler(cfg, cache, collectors)
	sched := scheduler.NewScheduler(cache, cfg, collectors)
	runGroup := run.Group{}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	runGroup.Add(func() error { return collectors.Run(ctx) }, func(_ error) { cancel() })
	runGroup.Add(webHandler.Run, webHandler.Stop)
	runGroup.Add(func() error { return sched.Run(ctx, conf.Args.ParallelScheduler) }, sched.Close)

	// Termination handler.
	runGroup.Add(
		func() error {
			<-ctx.Done()
			log.Logger.Warn().Msg("received SIGTERM, exiting...")
			cancel()
			daemon.SdNotify(false, daemon.SdNotifyStopping) //nolint:errcheck

			return nil
		},
		func(_ error) {},
	)

	// Reload handler.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	runGroup.Add(
		func() error {
			for range hup {
				log.Debug().Msg("reload configuration started")
				daemon.SdNotify(false, daemon.SdNotifyReloading) //nolint:errcheck

				if newConf, err := cfg.ReloadConfiguration(conf.Args.ConfigFilename, db.GlobalRegistry); err == nil {
					webHandler.UpdateConf(newConf)
					sched.UpdateConf(newConf)
					collectors.UpdateConf(newConf)
					log.Info().Interface("conf", newConf).Msg("configuration reloaded")
				} else {
					log.Logger.Error().Err(err).Msg("reloading configuration error; using old configuration")
				}

				daemon.SdNotify(false, daemon.SdNotifyReady) //nolint:errcheck
			}

			return nil
		},
		func(_ error) { close(hup) },
	)

	daemon.SdNotify(false, daemon.SdNotifyReady) //nolint:errcheck
	log.Logger.Log().Msgf("%s READY!", AppName)

	return runGroup.Run() //nolint:wrapcheck
}

func startSDWatchdog() error {
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
			daemon.SdNotify(false, daemon.SdNotifyWatchdog) //nolint:errcheck
		}
	}()

	log.Logger.Info().Err(err).Msg("systemd watchdog enabled")

	return nil
}

// InitializeLogger set log level and optional log filename.
func initializeLogger(level string, format string) {
	var llog zerolog.Logger

	switch format {
	default:
		log.Error().Msg("logger: unknown log format; using logfmt")

		fallthrough
	case "logfmt":
		llog = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			NoColor:    !outputIsConsole(),
			TimeFormat: time.RFC3339,
		})
	case "json":
		llog = log.Logger
	}

	if l, err := zerolog.ParseLevel(level); err == nil {
		zerolog.SetGlobalLevel(l)
	} else {
		log.Error().Msgf("logger: unknown log level '%s'; using debug", level)
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Logger = llog.With().Caller().Logger()

	stdlog.SetFlags(0)
	stdlog.SetOutput(log.Logger)
}

func outputIsConsole() bool {
	fileInfo, _ := os.Stdout.Stat()

	return fileInfo != nil && (fileInfo.Mode()&os.ModeCharDevice) != 0
}
