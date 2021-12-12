package main

//
// formatters.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
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
func FormatResult(ctx context.Context, qr *QueryResult, query *Query,
	db *Database) ([]byte, error) {
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
	bw := bufio.NewWriter(&output)
	err := query.MetricTpl.Execute(bw, r)
	if err != nil {
		return nil, fmt.Errorf("execute template error: %w", err)
	}

	_ = bw.Flush()

	b := bytes.TrimLeft(output.Bytes(), "\n\r\t ")
	return b, nil
}
