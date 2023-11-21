package handlers

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
	"prom-dbquery_exporter.app/internal/conf"
)

const (
	rwTimeout       = 10 * time.Second
	maxHeaderBytes  = 1 << 20
	shutdownTimeout = time.Duration(10) * time.Second
)

// WebHandler manage http handlers.
type WebHandler struct {
	handler       *queryHandler
	infoHandler   *infoHandler
	server        *http.Server
	listenAddress string
	webConfig     string
}

// NewWebHandler create new WebHandler.
func NewWebHandler(c *conf.Configuration, listenAddress string, webConfig string,
	disableCache bool, validateOutput bool,
) *WebHandler {
	qh := newQueryHandler(c, disableCache, validateOutput)
	http.Handle("/query", qh.Handler())

	ih := newInfoHandler(c)
	http.Handle("/info", ih.Handler())

	webHandler := &WebHandler{
		handler:       qh,
		infoHandler:   ih,
		listenAddress: listenAddress,
		webConfig:     webConfig,
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

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	return webHandler
}

// Run webhandler.
func (w *WebHandler) Run() error {
	log.Logger.Info().Msgf("Listening on %s", w.listenAddress)

	w.server = &http.Server{
		Addr:           w.listenAddress,
		ReadTimeout:    rwTimeout,
		WriteTimeout:   rwTimeout,
		MaxHeaderBytes: maxHeaderBytes,
	}

	if err := listenAndServe(w.server, w.webConfig); err != nil {
		return fmt.Errorf("listen and serve failed: %w", err)
	}

	return nil
}

// Close stop listen webhandler.
func (w *WebHandler) Close(err error) {
	_ = err

	log.Logger.Debug().Msg("web handler close")

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	_ = w.server.Shutdown(ctx)
}

// ReloadConf reload configuration in all handlers.
func (w *WebHandler) ReloadConf(newConf *conf.Configuration) {
	w.handler.SetConfiguration(newConf)
	w.infoHandler.Configuration = newConf
}
