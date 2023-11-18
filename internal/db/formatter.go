// formatters.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

import (
	"bytes"
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
)

// resultTmplData keep query result and some metadata parsed to template.
type resultTmplData struct {
	// Records (rows)
	R []Record
	// Parameters
	P map[string]interface{}
	// Labels
	L map[string]interface{}

	QueryStartTime int64
	QueryDuration  float64
	Count          int
	// Query name
	Query string
	// Database name
	Database string
}

// FormatResult format query result using template from query configuration.
func FormatResult(ctx context.Context, result *QueryResult, query *conf.Query,
	db *conf.Database,
) ([]byte, error) {
	llog := log.Ctx(ctx)
	llog.Debug().Msg("format result")

	res := &resultTmplData{
		Query:          query.Name,
		Database:       db.Name,
		R:              result.Records,
		P:              result.Params,
		L:              db.Labels,
		QueryStartTime: result.Start.Unix(),
		QueryDuration:  result.Duration,
		Count:          len(result.Records),
	}

	var output bytes.Buffer

	if err := query.MetricTpl.Execute(&output, res); err != nil {
		return nil, fmt.Errorf("execute template error: %w", err)
	}

	b := bytes.TrimLeft(output.Bytes(), "\n\r\t ")

	return b, nil
}
