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
	// Records (rows)
	R              []db.Record
	Error          string
	QueryStartTime int64
	QueryDuration  float64
	Count          int
}

// FormatResult format query result using template from query configuration.
func formatResult(ctx context.Context, qRes *db.QueryResult, query *conf.Query,
	db *conf.Database,
) ([]byte, error) {
	llog := log.Ctx(ctx)
	llog.Debug().Msg("format result")

	res := resultTmplData{
		Query:          query.Name,
		Database:       db.Name,
		R:              qRes.Records,
		P:              qRes.Params,
		L:              db.Labels,
		QueryStartTime: qRes.Start.Unix(),
		QueryDuration:  qRes.Duration,
		Count:          len(qRes.Records),
	}

	var output bytes.Buffer

	if err := query.MetricTpl.Execute(&output, &res); err != nil {
		return nil, fmt.Errorf("execute template error: %w", err)
	}

	return output.Bytes(), nil
}

func formatError(ctx context.Context, err error, query *conf.Query,
	db *conf.Database,
) ([]byte, error) {
	llog := log.Ctx(ctx)
	llog.Debug().Object("query", query).Msg("format result on error")

	res := resultTmplData{
		Query:    query.Name,
		Database: db.Name,
		L:        db.Labels,
		Error:    err.Error(),
	}

	var output bytes.Buffer

	if err := query.OnErrorTpl.Execute(&output, &res); err != nil {
		return nil, fmt.Errorf("execute template error: %w", err)
	}

	return output.Bytes(), nil
}
