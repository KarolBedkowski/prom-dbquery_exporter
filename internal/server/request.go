package server

//
// dataWriter.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"
	"iter"
	"net/http"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
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
	d.write(ctx, append([]byte(data), '\n'))
}

func (d *dataWriter) incScheduled() {
	d.scheduled++
}

//---------------------------------------------------------------

type InvalidRequestParameterError string

func (g InvalidRequestParameterError) Error() string {
	return string(g)
}

//---------------------------------------------------------------

type requestParameters struct {
	dbNames         []string
	queryNames      []string
	extraParameters map[string]any

	queries []*conf.Query
}

// MarshalZerologObject implements LogObjectMarshaler.
func (r *requestParameters) MarshalZerologObject(event *zerolog.Event) {
	event.Strs("dbNames", r.dbNames).
		Strs("queryNames", r.queryNames).
		Interface("extraParameters", r.extraParameters)

	qa := zerolog.Arr()
	for _, q := range r.queries {
		qa.Str(q.Name)
	}

	event.Array("queries", qa)
}

func (r *requestParameters) iter() iter.Seq2[string, *conf.Query] {
	return func(yield func(string, *conf.Query) bool) {
		for _, d := range r.dbNames {
			for _, q := range r.queries {
				if !yield(d, q) {
					return
				}
			}
		}
	}
}
