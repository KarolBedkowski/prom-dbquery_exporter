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
		llog := log.Ctx(ctx).With().
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
			Msg("request start")

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
			Msg("request finished")
	}

	return http.HandlerFunc(logFn)
}
