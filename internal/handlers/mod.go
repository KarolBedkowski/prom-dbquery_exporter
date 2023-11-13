package handlers

//
// mod.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
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
	disableParallel bool, disableCache bool, validateOutput bool,
) *WebHandler {
	qh, h := newQueryHandler(c, disableParallel, disableCache, validateOutput)
	h = hlog.RequestIDHandler("req_id", "X-Request-Id")(h)
	h = hlog.NewHandler(log.Logger)(h)
	http.Handle("/query", h)

	ih, h := newInfoHandler(c)
	h = hlog.RequestIDHandler("req_id", "X-Request-Id")(h)
	h = hlog.NewHandler(log.Logger)(h)
	http.Handle("/info", h)

	wh := &WebHandler{
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

	return wh
}

// Run webhandler.
func (w *WebHandler) Run() error {
	log.Logger.Info().Msgf("Listening on %s", w.listenAddress)

	w.server = &http.Server{Addr: w.listenAddress}
	if err := listenAndServe(w.server, w.webConfig); err != nil {
		return fmt.Errorf("listen and serve failed: %w", err)
	}

	return nil
}

// Close stop listen webhandler.
func (w *WebHandler) Close(err error) {
	log.Logger.Debug().Msg("web handler close")
	w.server.Close()
}

// ReloadConf reload configuration in all handlers.
func (w *WebHandler) ReloadConf(newConf *conf.Configuration) {
	w.handler.SetConfiguration(newConf)
	w.infoHandler.Configuration = newConf
}

// newQueryHandler create new query handler with logging and instrumentation.
func newQueryHandler(c *conf.Configuration, disableParallel bool,
	disableCache bool, validateOutput bool,
) (*queryHandler, http.Handler) {
	qh := &queryHandler{
		configuration:         c,
		runningQuery:          make(map[string]runningQueryInfo),
		disableParallel:       disableParallel,
		disableCache:          disableCache,
		validateOutputEnabled: validateOutput,
	}
	h := newLogMiddleware(
		promhttp.InstrumentHandlerDuration(
			metrics.NewReqDurationWraper("query"),
			qh), "query", false)

	return qh, h
}

// newInfoHandler create new info handler with logging and instrumentation.
func newInfoHandler(c *conf.Configuration) (*infoHandler, http.Handler) {
	ih := &infoHandler{Configuration: c}
	h := newLogMiddleware(
		promhttp.InstrumentHandlerDuration(
			metrics.NewReqDurationWraper("info"),
			ih), "info", false)

	return ih, h
}
