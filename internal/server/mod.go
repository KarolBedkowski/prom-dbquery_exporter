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
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/collectors"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/support"
)

const (
	defaultRwTimeout       = 300 * time.Second
	defaultMaxHeaderBytes  = 1 << 20
	defaultShutdownTimeout = time.Duration(2) * time.Second
)

// WebHandler handle incoming requests.
type WebHandler struct {
	handler       *queryHandler
	infoHandler   *infoHandler
	server        *http.Server
	cfg           *conf.Configuration
	listenAddress string
	webConfig     string
}

// NewWebHandler create new WebHandler.
func NewWebHandler(cfg *conf.Configuration, listenAddress string, webConfig string, cache *support.Cache[[]byte],
	taskQueue chan<- *collectors.Task,
) *WebHandler {
	qh := newQueryHandler(cfg, cache, taskQueue)
	http.Handle("/query", qh.Handler())

	ih := newInfoHandler(cfg)
	if cfg.EnableInfo {
		http.Handle("/info", ih.Handler())
	}

	webHandler := &WebHandler{
		handler:       qh,
		infoHandler:   ih,
		listenAddress: listenAddress,
		webConfig:     webConfig,
		cfg:           cfg,
	}

	local := strings.HasPrefix(listenAddress, "127.0.0.1:") || strings.HasPrefix(listenAddress, "localhost:")

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

	return webHandler
}

// Run webhandler.
func (w *WebHandler) Run() error {
	log.Logger.Info().Msgf("webhandler: listening on %s", w.listenAddress)

	rwTimeout := defaultRwTimeout
	if w.cfg.Global.RequestTimeout > 0 {
		rwTimeout = w.cfg.Global.RequestTimeout
	}

	w.server = &http.Server{
		Addr:           w.listenAddress,
		ReadTimeout:    rwTimeout,
		WriteTimeout:   rwTimeout,
		MaxHeaderBytes: defaultMaxHeaderBytes,
	}

	if err := listenAndServe(w.server, w.webConfig); err != nil {
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
func (w *WebHandler) UpdateConf(newConf *conf.Configuration) {
	w.handler.UpdateConf(newConf)
	w.infoHandler.Configuration = newConf
}
