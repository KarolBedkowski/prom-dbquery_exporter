package server

//
// mod.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/cache"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
)

const (
	defaultRwTimeout       = 300 * time.Second
	defaultMaxHeaderBytes  = 1 << 20
	defaultShutdownTimeout = time.Duration(2) * time.Second
)

type TaskQueue interface {
	AddTask(ctx context.Context, task *collectors.Task)
}

// WebHandler handle incoming requests.
type WebHandler struct {
	handler     *queryHandler
	infoHandler *infoHandler
	server      *http.Server
	cfg         *conf.Configuration
}

// New create new WebHandler.
func New(cfg *conf.Configuration, cache *cache.Cache[[]byte],
	taskQueue TaskQueue, cfgCh chan *conf.Configuration,
) *WebHandler {
	qh := newQueryHandler(cfg, cache, taskQueue)
	http.Handle("/query", qh.Handler())

	ih := newInfoHandler(cfg)
	if conf.Args.EnableInfo {
		http.Handle("/info", ih.Handler())
	}

	webHandler := &WebHandler{
		handler:     qh,
		infoHandler: ih,
		cfg:         cfg,
	}

	local := strings.HasPrefix(conf.Args.ListenAddress, "127.0.0.1:") ||
		strings.HasPrefix(conf.Args.ListenAddress, "localhost:")

	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
			// disable compression when listen on lo; reduce memory allocations & usage
			DisableCompression: local,
		},
	))

	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	if lp, err := newLandingPage(); err != nil {
		slog.Error("create landing pager error", "err", err)
	} else {
		http.Handle("/", lp)
	}

	webHandler.updateConfHandler(cfgCh)

	return webHandler
}

// Run webhandler.
func (w *WebHandler) Run() error {
	log.Logger.Info().Msgf("webhandler: listening on %s", conf.Args.ListenAddress)

	rwTimeout := defaultRwTimeout
	if w.cfg.Global.RequestTimeout > 0 {
		rwTimeout = w.cfg.Global.RequestTimeout
	}

	w.server = &http.Server{
		Addr:           conf.Args.ListenAddress,
		ReadTimeout:    rwTimeout,
		WriteTimeout:   rwTimeout,
		MaxHeaderBytes: defaultMaxHeaderBytes,
	}

	if err := listenAndServe(w.server, conf.Args.WebConfig); err != nil {
		return fmt.Errorf("listen and serve failed: %w", err)
	}

	return nil
}

// Stop stop listen webhandler.
func (w *WebHandler) Stop(err error) {
	_ = err

	log.Logger.Debug().Msg("webhandler: closing")

	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer cancel()

	_ = w.server.Shutdown(ctx)
}

// UpdateConf reload configuration in all handlers.
func (w *WebHandler) updateConfHandler(cfgCh chan *conf.Configuration) {
	go func() {
		for newCfg := range cfgCh {
			w.handler.updateConf(newCfg)
			w.infoHandler.cfg = newCfg
		}
	}()
}

func newLandingPage() (*web.LandingPageHandler, error) {
	landingConfig := web.LandingConfig{
		Name:        "dbquery_exporter",
		Description: "Prometheus dbquery Exporter",
		Version:     version.Info(),
		Links: []web.LandingLinks{
			{
				Address: "/metrics",
				Text:    "Metrics",
			},
		},
	}

	return web.NewLandingPage(landingConfig) //nolint:wrapcheck
}
