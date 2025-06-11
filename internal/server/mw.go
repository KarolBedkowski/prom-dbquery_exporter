package server

//
// mw.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//
// Inspired by: https://arunvelsriram.dev/simple-golang-http-logging-middleware

import (
	"compress/gzip"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
)

type (
	// our http.ResponseWriter implementation.
	logResponseWriter struct {
		http.ResponseWriter // compose original http.ResponseWriter

		status int // http status
		size   int // response size
	}
)

func (r *logResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b) // write response using original http.ResponseWriter
	r.size += size                         // capture size

	if err != nil {
		return size, fmt.Errorf("write response error: %w", err)
	}

	return size, nil
}

func (r *logResponseWriter) WriteHeader(status int) {
	r.ResponseWriter.WriteHeader(status)

	r.status = status
}

// newLogMiddleware create new logging middleware.
// `name` is handler name added to log.
func newLogMiddleware(next http.Handler) http.Handler {
	logFn := func(writer http.ResponseWriter, request *http.Request) {
		start := time.Now()
		ctx := request.Context()
		requestID, _ := hlog.IDFromCtx(ctx)
		llog := log.With().Logger().With().Str("req_id", requestID.String()).Logger()
		request = request.WithContext(llog.WithContext(ctx))

		llog.Info().
			Str("uri", request.RequestURI).
			Str("remote", request.RemoteAddr).
			Str("method", request.Method).
			Strs("agent", request.Header["User-Agent"]).
			Msg("webhandler: request start")

		lrw := logResponseWriter{ResponseWriter: writer, status: 0, size: 0}

		next.ServeHTTP(&lrw, request)

		level := zerolog.InfoLevel
		if lrw.status >= 400 && lrw.status != 404 {
			level = zerolog.WarnLevel
		}

		llog.WithLevel(level).
			Str("uri", request.RequestURI).
			Int("status", lrw.status).
			Int("size", lrw.size).
			Dur("duration", time.Since(start)).
			Msg("webhandler: request finished")
	}

	return http.HandlerFunc(logFn)
}

// -------------------------------------------------

// newLimitRequestInFlightMW create new http middleware that limit concurrent connection to `limit`.
func newLimitRequestInFlightMW(next http.Handler, limit uint) http.Handler {
	inFlightSem := make(chan struct{}, limit)

	logFn := func(w http.ResponseWriter, r *http.Request) {
		select {
		case inFlightSem <- struct{}{}:
			defer func() { <-inFlightSem }()
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "Limit of concurrent requests reached, try again later.", http.StatusTooManyRequests)

			return
		}
	}

	return http.HandlerFunc(logFn)
}

// -------------------------------------------------

type gzipResponseWriter struct {
	http.ResponseWriter

	w *gzip.Writer

	status        int
	headerWritten bool
}

func (gzr *gzipResponseWriter) WriteHeader(status int) {
	gzr.status = status
	gzr.headerWritten = true

	if gzr.status != http.StatusNotModified && gzr.status != http.StatusNoContent {
		gzr.ResponseWriter.Header().Set("Content-Encoding", "gzip")
		gzr.ResponseWriter.Header().Del("Content-Length")
	}

	gzr.ResponseWriter.WriteHeader(status)
}

func (gzr *gzipResponseWriter) Write(b []byte) (int, error) {
	if _, ok := gzr.Header()["Content-Type"]; !ok {
		gzr.ResponseWriter.Header().Set("Content-Type", http.DetectContentType(b))
	}

	if !gzr.headerWritten {
		gzr.WriteHeader(http.StatusOK)
	}

	cnt, err := gzr.w.Write(b)
	if err != nil {
		return cnt, fmt.Errorf("write via gzip error: %w", err)
	}

	return cnt, nil
}

func newGzipHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, req)

			return
		}

		w.Header().Set("Content-Encoding", "gzip")

		gz := gzip.NewWriter(w)
		defer gz.Close()

		gzr := &gzipResponseWriter{ResponseWriter: w, w: gz, status: 0, headerWritten: false}

		next.ServeHTTP(gzr, req)
	})
}
