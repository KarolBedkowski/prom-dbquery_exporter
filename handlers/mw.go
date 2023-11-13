package handlers

//
// mw.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//
// Inspired by: https://arunvelsriram.dev/simple-golang-http-logging-middleware

import (
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

	return size, err
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

		if asDebug {
			llog.Debug().Msg("request start")
		} else {
			llog.Info().Msg("request start")
		}

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
		level := zerolog.WarnLevel

		if responseData.status < 400 && responseData.status != 404 {
			if asDebug {
				level = zerolog.DebugLevel
			} else {
				level = zerolog.InfoLevel
			}
		}

		llog.WithLevel(level).
			Int("status", responseData.status).
			Int("size", responseData.size).
			Dur("duration", duration).
			Msg("request finished")
	}

	return http.HandlerFunc(logFn)
}
