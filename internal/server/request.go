package server

//
// dataWriter.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"net/http"

	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/metrics"
	"prom-dbquery_exporter.app/internal/support"
)

type dataWriter struct {
	written   int
	scheduled int
	writer    http.ResponseWriter
}

func (d *dataWriter) writeHeaders() {
	d.writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
}

func (d *dataWriter) write(ctx context.Context, data []byte) {
	if _, err := d.writer.Write(data); err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("queryhandler: write error")
		support.TraceErrorf(ctx, "write error: %s", err)
		metrics.IncProcessErrorsCnt("write")
	} else {
		d.written++
	}
}

func (d *dataWriter) writeError(ctx context.Context, data string) {
	d.write(ctx, []byte(data))
}

func (d *dataWriter) incScheduled() {
	d.scheduled++
}
