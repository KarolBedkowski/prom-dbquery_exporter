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
	"maps"
	"net/http"
	"slices"

	"github.com/hashicorp/go-multierror"
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

func newRequestParams(req *http.Request, cfg *conf.Configuration) (*requestParameters, error) {
	var errs *multierror.Error

	dbNames := deduplicateStringList(req.URL.Query()["database"])
	if len(dbNames) == 0 {
		errs = multierror.Append(errs, InvalidRequestParameterError("missing database parameter"))
	}

	queryNames := req.URL.Query()["query"]

	for _, g := range req.URL.Query()["group"] {
		q := cfg.GroupQueries(g)
		if len(q) > 0 {
			queryNames = append(queryNames, q...)
		} else {
			errs = multierror.Append(errs, InvalidRequestParameterError("unknown group "+g))
		}
	}

	queryNames = deduplicateStringList(queryNames)
	if len(queryNames) == 0 {
		errs = multierror.Append(errs, InvalidRequestParameterError("missing query or group parameter"))
	}

	queries := make([]*conf.Query, 0, len(queryNames))

	for _, name := range queryNames {
		if query, ok := cfg.Query[name]; ok {
			queries = append(queries, query)
		} else {
			errs = multierror.Append(errs, InvalidRequestParameterError("unknown query "+name))
		}
	}

	if len(queries) == 0 {
		errs = multierror.Append(errs, InvalidRequestParameterError("no valid query given"))
	}

	if err := errs.ErrorOrNil(); err != nil {
		return nil, err
	}

	return &requestParameters{dbNames, queryNames, paramsFromQuery(req), queries}, nil
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

func paramsFromQuery(req *http.Request) map[string]any {
	params := make(map[string]any)

	for k, v := range req.URL.Query() {
		// standard parameters
		if k != "query" && k != "group" && k != "database" && len(v) > 0 {
			params[k] = v[0]
		}
	}

	return params
}

func deduplicateStringList(inp []string) []string {
	if len(inp) <= 1 {
		return inp
	}

	tmpMap := make(map[string]bool, len(inp))
	for _, s := range inp {
		tmpMap[s] = true
	}

	return slices.Collect(maps.Keys(tmpMap))
}
