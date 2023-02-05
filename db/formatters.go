package db

//
// formatters.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"bytes"
	"context"
	"fmt"

	"prom-dbquery_exporter.app/conf"
)

// resultTmplData keep query result and some metadata parsed to template
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

// FormatResult format query result using template from query configuration
func FormatResult(ctx context.Context, qr *QueryResult, query *conf.Query,
	db *conf.Database) ([]byte, error) {
	r := &resultTmplData{
		Query:          query.Name,
		Database:       db.Name,
		R:              qr.Records,
		P:              qr.Params,
		L:              db.Labels,
		QueryStartTime: qr.Start.Unix(),
		QueryDuration:  qr.Duration,
		Count:          len(qr.Records),
	}

	var output bytes.Buffer
	err := query.MetricTpl.Execute(&output, r)
	if err != nil {
		return nil, fmt.Errorf("execute template error: %w", err)
	}

	b := bytes.TrimLeft(output.Bytes(), "\n\r\t ")
	return b, nil
}
