//go:build debug
// +build debug

//
// debug.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

package support

import (
	"context"
	"fmt"
	"net/http"
	"runtime/pprof"

	// setup pprof endpont
	_ "net/http/pprof"

	"github.com/rs/zerolog/hlog"
	"golang.org/x/net/trace"
)

const (
	// TraceMaxEvents is max events gathered in trace
	TraceMaxEvents = 25
)

// SetGoroutineLabels set debug labels for current goroutine.
func SetGoroutineLabels(ctx context.Context, labels ...string) {
	pprof.SetGoroutineLabels(pprof.WithLabels(ctx, pprof.Labels(labels...)))
}

// TracePrintf add message to trace.
func TracePrintf(ctx context.Context, msg string, args ...any) {
	if tr, ok := trace.FromContext(ctx); ok && tr != nil {
		tr.LazyPrintf(msg, args...)
	}
}

// TraceErrorf add message to trace ans set error on trace.
func TraceErrorf(ctx context.Context, msg string, args ...any) { //nolint:all
	if tr, ok := trace.FromContext(ctx); ok && tr != nil {
		tr.LazyPrintf(msg, args...)
		tr.SetError()
	}
}

// TraceLog add log to trace.
func TraceLog(ctx context.Context, x fmt.Stringer, sensitive bool) { //nolint:all
	if tr, ok := trace.FromContext(ctx); ok && tr != nil {
		tr.LazyLog(x, sensitive)
	}
}

// TraceSetError set error flag on trace.
func TraceSetError(ctx context.Context) { //nolint:all
	if tr, ok := trace.FromContext(ctx); ok && tr != nil {
		tr.SetError()
	}
}

// NewTraceMiddleware create new middleware that for each request create
// trace and put it into context.
func NewTraceMiddleware(name string) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			url := req.URL.String()

			if requestID, ok := hlog.IDFromCtx(ctx); ok {
				url += " " + requestID.String()
			}

			tr := trace.New(name, url)
			defer tr.Finish()

			tr.SetMaxEvents(TraceMaxEvents)

			ctx = trace.NewContext(ctx, tr)
			req = req.WithContext(ctx)

			next.ServeHTTP(w, req)
		})
	}
}
