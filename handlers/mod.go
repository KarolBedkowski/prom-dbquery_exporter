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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"prom-dbquery_exporter.app/conf"
	"prom-dbquery_exporter.app/metrics"
	"prom-dbquery_exporter.app/support"
)

// WebHandler manage http handlers
type WebHandler struct {
	handler       *queryHandler
	infoHandler   *infoHandler
	server        *http.Server
	listenAddress string
	webConfig     string
}

// NewWebHandler create new WebHandler
func NewWebHandler(c *conf.Configuration, listenAddress string, webConfig string,
	disableParallel bool, disableCache bool, validateOutput bool) *WebHandler {

	qh, h := newQueryHandler(c, disableParallel, disableCache, validateOutput)
	http.Handle("/query", h)

	ih, h := newInfoHandler(c)
	http.Handle("/info", h)

	wh := &WebHandler{
		handler:       qh,
		infoHandler:   ih,
		listenAddress: listenAddress,
		webConfig:     webConfig,
	}

	http.Handle("/metrics", promhttp.HandlerFor(
		prometheus.DefaultGatherer,
		promhttp.HandlerOpts{
			// Opt into OpenMetrics to support exemplars.
			EnableOpenMetrics: true,
		},
	))

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	return wh
}

// Run webhandler
func (w *WebHandler) Run() error {
	support.Logger.Info().Msgf("Listening on %s", w.listenAddress)
	w.server = &http.Server{Addr: w.listenAddress}
	if err := listenAndServe(w.server, w.webConfig); err != nil {
		return fmt.Errorf("listen and serve failed: %w", err)
	}
	return nil
}

// Close stop listen webhandler
func (w *WebHandler) Close(err error) {
	support.Logger.Debug().Msg("web handler close")
	w.server.Close()
}

// ReloadConf reload configuration in all handlers
func (w *WebHandler) ReloadConf(newConf *conf.Configuration) {
	w.handler.SetConfiguration(newConf)
	w.infoHandler.Configuration = newConf
}

// newQueryHandler create new query handler with logging and instrumentation
func newQueryHandler(c *conf.Configuration, disableParallel bool,
	disableCache bool, validateOutput bool) (*queryHandler, http.Handler) {
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

// newInfoHandler create new info handler with logging and instrumentation
func newInfoHandler(c *conf.Configuration) (*infoHandler, http.Handler) {
	ih := &infoHandler{Configuration: c}
	h := newLogMiddleware(
		promhttp.InstrumentHandlerDuration(
			metrics.NewReqDurationWraper("info"),
			ih), "info", false)

	return ih, h
}
