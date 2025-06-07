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
	defaultShutdownTimeout = time.Duration(5) * time.Second
)

type TaskQueue interface {
	AddTask(ctx context.Context, task *collectors.Task)
}

// WebHandler handle incoming requests.
type WebHandler struct {
	queryHandler *queryHandler
	// info is available only on localhost
	infoHandler *infoHandler
	server      *http.Server
	cfg         *conf.Configuration
}

// New create new WebHandler.
func New(cfg *conf.Configuration, cache *cache.Cache[[]byte],
	taskQueue TaskQueue, cfgCh chan *conf.Configuration,
) *WebHandler {
	webHandler := &WebHandler{
		queryHandler: newQueryHandler(cfg, cache, taskQueue),
		infoHandler:  newInfoHandler(cfg),
		cfg:          cfg,
		server:       nil,
	}

	http.Handle("/query", webHandler.queryHandler.Handler())
	http.Handle("/info", webHandler.infoHandler.Handler())
	http.HandleFunc("/health", healthHandler)

	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{ //nolint:exhaustruct
			EnableOpenMetrics: true,
			// disable compression when listen on lo; reduce memory allocations & usage
			DisableCompression: conf.Args.ListenOnLocalAddress(),
		},
	))

	if lp, err := newLandingPage(); err != nil {
		slog.Error("create landing pager error", "err", err)
	} else {
		http.Handle("/", lp)
	}

	webHandler.startConfHandler(cfgCh)

	return webHandler
}

// Run webhandler.
func (w *WebHandler) Run() error {
	log.Logger.Info().Msgf("webhandler: listening on %s", conf.Args.ListenAddress)

	rwTimeout := defaultRwTimeout
	if w.cfg.Global.RequestTimeout > 0 {
		rwTimeout = w.cfg.Global.RequestTimeout
	}

	w.server = &http.Server{ //nolint:exhaustruct
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

// startConfHandler wait for configuration changes and update handlers.
func (w *WebHandler) startConfHandler(cfgCh chan *conf.Configuration) {
	go func() {
		for newCfg := range cfgCh {
			w.cfg = newCfg
			w.infoHandler.cfg = newCfg

			w.queryHandler.updateConf(newCfg)
		}
	}()
}

func newLandingPage() (*web.LandingPageHandler, error) {
	landingConfig := web.LandingConfig{ //nolint:exhaustruct
		Name:        "dbquery_exporter",
		Description: "Prometheus dbquery Exporter",
		Version:     version.Info(),
		Links: []web.LandingLinks{
			{
				Address:     "/metrics",
				Text:        "Metrics",
				Description: "Endpoint with exporter metrics",
			},
		},
	}

	return web.NewLandingPage(landingConfig) //nolint:wrapcheck
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
}
