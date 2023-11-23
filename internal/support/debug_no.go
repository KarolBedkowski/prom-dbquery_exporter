//go:build !debug
// +build !debug
//
// debug.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

package support

import (
	"context"
)

func SetGoroutineLabels(ctx context.Context, labels ...string) {
	// ignore
}
