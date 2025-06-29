package main

import (
	"context"
	"errors"
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

	fmt.Printf("Available template functions: %s\n", templates.TemplateFunctions()) //nolint:forbidigo
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

	log.Logger.Log().Msgf("available template functions: %s", templates.TemplateFunctions())
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
	log.Logger.Debug().Msgf("cli arguments: %+v", conf.Args)

	sdw, err := newSdWatchdog()
	if err != nil {
		log.Logger.Warn().Err(err).Msg("systemd: init watchdog error")
	}

	cfg, err := conf.Load(conf.Args.ConfigFilename, db.GlobalRegistry)
	if err != nil || cfg == nil {
		log.Logger.Fatal().Err(err).Msgf("failed load config file %q", conf.Args.ConfigFilename)
	}

	log.Logger.Debug().Interface("conf", cfg).Msg("configuration loaded")

	if err := start(cfg, sdw); err != nil {
		log.Logger.Fatal().Err(err).Msg("start failed")
	}

	log.Logger.Log().Msg("finished.")
}

func start(cfg *conf.Configuration, sdw *sdWatchdog) error {
	confHandler := newConfHandler(cfg)
	collectors := collectors.New(cfg, confHandler.newReloadCh())
	cache := cache.New[[]byte]("query_cache")
	webHandler := server.New(cfg, &cache, collectors, confHandler.newReloadCh())
	sched := scheduler.New(cfg, &cache, collectors, confHandler.newReloadCh())
	runGroup := run.Group{}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	// stop all goroutines on error or first exit
	stop := func(_ error) { cancel() }

	runGroup.Add(func() error { return collectors.Run(ctx) }, stop)
	runGroup.Add(func() error { return sched.Run(ctx, conf.Args.ParallelScheduler) }, stop)
	runGroup.Add(confHandler.start, confHandler.stop)
	runGroup.Add(webHandler.Run, webHandler.Stop)

	// start systemd watchdog when available
	if sdw != nil {
		runGroup.Add(func() error { return sdw.start(ctx) }, stop)
	}

	// Termination handler.
	runGroup.Add(
		func() error {
			<-ctx.Done()
			log.Logger.Warn().Msg("exiting...")
			daemon.SdNotify(false, daemon.SdNotifyStopping) //nolint:errcheck

			return nil
		},
		stop,
	)

	daemon.SdNotify(false, daemon.SdNotifyReady) //nolint:errcheck
	log.Logger.Log().Msgf("%s READY!", AppName)

	return runGroup.Run() //nolint:wrapcheck
}

// ------------------------------------------------------------

var (
	ErrSystemdNotAvailable = errors.New("systemd not available")
	ErrSystemdNoWatchdog   = errors.New("systemd watchdog disabled")
)

type sdWatchdog struct{ interval time.Duration }

func newSdWatchdog() (*sdWatchdog, error) {
	ok, err := daemon.SdNotify(false, "STATUS=starting")
	if err != nil {
		return nil, fmt.Errorf("send status to systemd error: %w", err)
	}

	if !ok {
		return nil, ErrSystemdNotAvailable
	}

	interval, err := daemon.SdWatchdogEnabled(false)
	if err != nil {
		return nil, fmt.Errorf("enable sdwatchdog error: %w", err)
	}

	if interval == 0 {
		return nil, ErrSystemdNoWatchdog
	}

	return &sdWatchdog{interval}, nil
}

func (s sdWatchdog) start(ctx context.Context) error {
	// watchdog disabled?
	interval := s.interval / 2 //nolint:mnd

	log.Logger.Info().Msg("systemd: watchdog started")

	tick := time.Tick(interval)

	for {
		select {
		case <-tick:
			daemon.SdNotify(false, daemon.SdNotifyWatchdog) //nolint:errcheck
		case <-ctx.Done():
			return nil
		}
	}
}

// ------------------------------------------------------------

// InitializeLogger set log level and optional log filename.
func initializeLogger(level string, format string) {
	var llog zerolog.Logger

	switch format {
	default:
		log.Error().Msgf("logger: unknown log format %q; using logfmt", format)

		fallthrough
	case "logfmt":
		llog = log.Output(zerolog.ConsoleWriter{ //nolint:exhaustruct
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
		log.Error().Msgf("logger: unknown log level %q; using debug", level)
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

//--------------------------------------------------------

type confHandler struct {
	hupCh    chan os.Signal
	cfg      *conf.Configuration
	reloadCh []chan *conf.Configuration
}

func newConfHandler(cfg *conf.Configuration) confHandler {
	return confHandler{
		hupCh:    make(chan os.Signal, 1),
		cfg:      cfg,
		reloadCh: nil,
	}
}

// newReloadCh create new channel for service that want receive configuration changes.
func (h *confHandler) newReloadCh() chan *conf.Configuration {
	ch := make(chan *conf.Configuration)
	h.reloadCh = append(h.reloadCh, ch)

	return ch
}

func (h *confHandler) start() error {
	signal.Notify(h.hupCh, syscall.SIGHUP)

	for range h.hupCh {
		log.Debug().Msg("reload configuration started")
		daemon.SdNotify(false, daemon.SdNotifyReloading) //nolint:errcheck

		if newConf, err := conf.Load(conf.Args.ConfigFilename, db.GlobalRegistry); err == nil {
			log.Debug().Msg("new configuration loaded")

			for _, rh := range h.reloadCh {
				rh <- newConf
			}

			h.cfg = newConf

			log.Debug().Interface("conf", newConf).Msg("new configuration")
			log.Info().Msg("configuration reloaded")
			daemon.SdNotify(false, daemon.SdNotifyReady) //nolint:errcheck
		} else {
			log.Error().Err(err).Msg("reloading configuration failed; using old one")
		}
	}

	return nil
}

func (h *confHandler) stop(_ error) {
	close(h.hupCh)

	for _, rh := range h.reloadCh {
		close(rh)
	}
}
