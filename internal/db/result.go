package db

// Formatter contain methods used to format query result using defined templates.
// formatters.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// QueryResult is result of Loader.Query.
type QueryResult struct {
	// query start time
	StartTS time.Time
	// all query parameters
	Params map[string]any
	// rows
	Records []Record
	// query duration
	Duration float64
}

func newQueryResult(startts time.Time, params map[string]any, records []Record) *QueryResult {
	return &QueryResult{
		StartTS:  startts,
		Params:   params,
		Duration: time.Since(startts).Seconds(),
		Records:  records,
	}
}

// -----------------------------------------------

// Record is one record (row) loaded from database.
type Record map[string]any

func newRecord(rows *sqlx.Rows) (Record, error) {
	rec := Record{}
	if err := rows.MapScan(rec); err != nil {
		return nil, fmt.Errorf("map scan record error: %w", err)
	}

	// convert []byte to string
	for k, v := range rec {
		if v, ok := v.([]byte); ok {
			rec[k] = string(v)
		}
	}

	return rec, nil
}

func recordsFromRows(rows *sqlx.Rows) ([]Record, error) {
	var records []Record

	for rows.Next() {
		rec, err := newRecord(rows)
		if err != nil {
			return nil, err
		}

		records = append(records, rec)
	}

	return records, nil
}
