package collectors

// Formatter contain methods used to format query result using defined templates.
// formatters.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

import (
	"bytes"
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/db"
)

const initialBufferSize = 512

// resultTmplData keep query result and some metadata parsed to template.
type resultTmplData struct {
	// Parameters
	P map[string]any
	// Labels
	L map[string]any
	// Query name
	Query string
	// Database name
	Database string

	Error string

	// Records (rows)
	R []db.Record

	QueryStartTime int64
	QueryDuration  float64
	Count          int
}

// FormatResult format query result using template from query configuration.
func formatResult(ctx context.Context, qRes *db.QueryResult, query *conf.Query,
	db *conf.Database,
) ([]byte, error) {
	res := resultTmplData{
		Query:          query.Name,
		Database:       db.Name,
		R:              qRes.Records,
		P:              qRes.Params,
		L:              db.Labels,
		QueryStartTime: qRes.StartTS.Unix(),
		QueryDuration:  qRes.Duration,
		Count:          len(qRes.Records),
		Error:          "",
	}

	log.Ctx(ctx).Debug().Interface("res", res).Msg("result: executing template")

	var output bytes.Buffer

	output.Grow(initialBufferSize)

	if err := query.MetricTpl.Execute(&output, &res); err != nil {
		return nil, fmt.Errorf("execute template error: %w", err)
	}

	return output.Bytes(), nil
}

func formatError(ctx context.Context, err error, query *conf.Query,
	db *conf.Database,
) ([]byte, error) {
	res := resultTmplData{ //nolint:exhaustruct
		Query:    query.Name,
		Database: db.Name,
		L:        db.Labels,
		Error:    err.Error(),
	}

	log.Ctx(ctx).Debug().Object("query", query).Interface("res", res).Msg("result: executing on_error template")

	var output bytes.Buffer

	if err := query.OnErrorTpl.Execute(&output, &res); err != nil {
		return nil, fmt.Errorf("execute on_error template error: %w", err)
	}

	return output.Bytes(), nil
}
