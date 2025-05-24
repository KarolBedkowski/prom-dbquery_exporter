package server

//
// mw.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//
// Inspired by: https://arunvelsriram.dev/simple-golang-http-logging-middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
)

type (
	// struct for holding response details.
	responseData struct {
		status int
		size   int
	}

	// our http.ResponseWriter implementation.
	logResponseWriter struct {
		http.ResponseWriter // compose original http.ResponseWriter
		responseData        *responseData
	}
)

func (r *logResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b) // write response using original http.ResponseWriter
	r.responseData.size += size            // capture size

	if err != nil {
		return size, fmt.Errorf("write response error: %w", err)
	}

	return size, nil
}

func (r *logResponseWriter) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
	r.responseData.status = statusCode
}

// newLogMiddleware create new logging middleware.
// `name` is handler name added to log.
// If `asDebug` is true log level for non-error events is DEBUG; if false - is INFO.
func newLogMiddleware(next http.Handler, name string, asDebug bool) http.Handler {
	logFn := func(writer http.ResponseWriter, request *http.Request) {
		start := time.Now()

		ctx := request.Context()
		requestID, _ := hlog.IDFromCtx(ctx)
		llog := log.With().Logger().With().Str("req_id", requestID.String()).Logger()
		request = request.WithContext(llog.WithContext(ctx))

		llog = llog.With().
			Str("remote", request.RemoteAddr).
			Str("uri", request.RequestURI).
			Str("method", request.Method).
			Str("handler", name).
			Logger()

		level := zerolog.InfoLevel

		if asDebug {
			level = zerolog.DebugLevel
		}

		llog.WithLevel(level).
			Strs("agent", request.Header["User-Agent"]).
			Msg("webhandler: request start")

		responseData := &responseData{
			status: 0,
			size:   0,
		}
		lrw := logResponseWriter{
			ResponseWriter: writer,
			responseData:   responseData,
		}

		next.ServeHTTP(&lrw, request)

		// log request result
		duration := time.Since(start)

		if responseData.status >= 400 && responseData.status != 404 {
			level = zerolog.WarnLevel
		}

		llog.WithLevel(level).
			Int("status", responseData.status).
			Int("size", responseData.size).
			Dur("duration", duration).
			Msg("webhandler: request finished")
	}

	return http.HandlerFunc(logFn)
}

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
