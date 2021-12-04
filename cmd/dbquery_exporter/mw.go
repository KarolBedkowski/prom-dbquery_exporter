package main

//
// mw.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//
// Inspired by: https://arunvelsriram.dev/simple-golang-http-logging-middleware

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
)

type (
	// struct for holding response details
	responseData struct {
		status int
		size   int
	}

	// our http.ResponseWriter implementation
	logResponseWriter struct {
		http.ResponseWriter // compose original http.ResponseWriter
		responseData        *responseData
	}
)

func (r *logResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b) // write response using original http.ResponseWriter
	r.responseData.size += size            // capture size
	return size, err
}

func (r *logResponseWriter) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
	r.responseData.status = statusCode
}

var requestID uint64

// newLogMiddleware create new logging middleware.
// `name` is handler name added to log.
// If `asDebug` is true log level for non-error events is DEBUG; if false - is INFO.
func newLogMiddleware(next http.Handler, name string, asDebug bool) http.Handler {
	mlog := Logger
	logFn := func(rw http.ResponseWriter, r *http.Request) {
		start := time.Now()

		requestID := atomic.AddUint64(&requestID, 1)
		l := mlog.With().Uint64("req_id", requestID).Logger()
		ctx := l.WithContext(context.Background())
		r = r.WithContext(ctx)

		ll := l.With().
			Str("remote", r.RemoteAddr).
			Str("uri", r.RequestURI).
			Str("method", r.Method).
			Str("handler", name).
			Logger()

		if asDebug {
			ll.Debug().Msg("request start")
		} else {
			ll.Info().Msg("request start")
		}

		responseData := &responseData{
			status: 0,
			size:   0,
		}
		lrw := logResponseWriter{
			ResponseWriter: rw,
			responseData:   responseData,
		}

		next.ServeHTTP(&lrw, r)
		duration := time.Since(start)

		// log request result
		var e *zerolog.Event
		if responseData.status < 400 && responseData.status != 404 {
			if asDebug {
				e = ll.Debug()
			} else {
				e = ll.Info()
			}
		} else {
			e = ll.Warn()
		}
		e.Int("status", responseData.status).
			Int("size", responseData.size).
			Dur("duration", duration).
			Msg("request finished")
	}

	return http.HandlerFunc(logFn)
}
