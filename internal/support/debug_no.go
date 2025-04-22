//go:build !debug
// +build !debug

//
// debug_no.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

package support

import (
	"context"
	"fmt"
	"net/http"
)

const (
	// TraceMaxEvents is max events gathered in trace; 0 = disabled.
	TraceMaxEvents = 0
)

// SetGoroutineLabels is dummy method.
func SetGoroutineLabels(ctx context.Context, labels ...string) { //nolint:all
	// ignore
}

// TracePrintf is dummy method.
func TracePrintf(ctx context.Context, msg string, args ...any) { //nolint:all
	// ignore
}

// TraceErrorf is dummy method.
func TraceErrorf(ctx context.Context, msg string, args ...any) { //nolint:all
	// ignore
}

// TraceLog is dummy method.
func TraceLog(ctx context.Context, x fmt.Stringer, sensitive bool) { //nolint:all
	// ignore
}

// TraceSetError is dummy method.
func TraceSetError(ctx context.Context) { //nolint:all
	// ignore
}

// NewTraceMiddleware create new dummy middleware.
func NewTraceMiddleware(name string) func(next http.Handler) http.Handler { //nolint:all
	return func(next http.Handler) http.Handler {
		return next
	}
}
