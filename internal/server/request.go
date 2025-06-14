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
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/debug"
	"prom-dbquery_exporter.app/internal/metrics"
)

type responseWriter struct {
	writer http.ResponseWriter

	written   int
	scheduled int
}

func (d *responseWriter) writeHeaders() {
	d.writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
}

func (d *responseWriter) write(ctx context.Context, data []byte) {
	if _, err := d.writer.Write(data); err != nil {
		log.Ctx(ctx).Error().Err(err).Msg("responseWriter: write error")
		debug.TraceErrorf(ctx, "write error: %s", err)
		metrics.IncErrorsCnt(metrics.ErrCategoryClientError)
	} else {
		d.written++
	}
}

func (d *responseWriter) incScheduled() {
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
	extraParameters []string

	queries []*conf.Query
}

// MarshalZerologObject implements LogObjectMarshaler.
func (r *requestParameters) MarshalZerologObject(event *zerolog.Event) {
	event.Strs("dbNames", r.dbNames).
		Strs("queryNames", r.queryNames).
		Strs("extraParameters", r.extraParameters)

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

//---------------------------------------------------------------

func paramsFromQuery(req *http.Request) []string {
	query := req.URL.Query()

	var params []string

	for k, v := range query {
		// standard parameters
		if k != "query" && k != "group" && k != "database" && len(v) > 0 {
			params = append(params, k, v[0])
		}
	}

	return params
}

// deduplicateStringList trim and remove empty and duplicated values from `inp`.
func deduplicateStringList(inp []string) []string {
	switch len(inp) {
	case 0:
		return nil

	case 1:
		if s := strings.TrimSpace(inp[0]); s != "" {
			return []string{s}
		}

		return nil

	default:
		tmpMap := make(map[string]struct{}, len(inp))

		for _, s := range inp {
			if s := strings.TrimSpace(s); s != "" {
				tmpMap[s] = struct{}{}
			}
		}

		if len(tmpMap) == 0 {
			return nil
		}

		return slices.Collect(maps.Keys(tmpMap))
	}
}
