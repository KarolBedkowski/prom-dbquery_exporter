package collectors

// Formatter contain methods used to format query result using defined templates.
// formatters.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"math"
	"strconv"

	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/db"
	"prom-dbquery_exporter.app/internal/support"
)

// resultTmplData keep query result and some metadata parsed to template.
type resultTmplData struct {
	// Records (rows)
	R []db.Record
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
func formatResult(ctx context.Context, qRes *db.QueryResult, query *conf.Query,
	db *conf.Database,
) ([]byte, error) {
	llog := log.Ctx(ctx)
	llog.Debug().Msg("format result")

	res := &resultTmplData{
		Query:          query.Name,
		Database:       db.Name,
		R:              qRes.Records,
		P:              qRes.Params,
		L:              db.Labels,
		QueryStartTime: qRes.Start.Unix(),
		QueryDuration:  qRes.Duration,
		Count:          len(qRes.Records),
	}

	var buf bytes.Buffer

	if err := query.MetricTpl.Execute(&buf, res); err != nil {
		return nil, fmt.Errorf("execute template error: %w", err)
	}

	// trim lines
	var output bytes.Buffer

	scanner := bufio.NewScanner(&buf)
	for scanner.Scan() {
		line := bytes.Trim(scanner.Bytes(), "\n\r\t ")

		if len(line) > 0 {
			output.Write(line)
			output.WriteRune('\n')
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan result error: %w", err)
	}

	return output.Bytes(), nil
}

func tmplFuncBuckets(input []db.Record, valueKey string, buckets ...float64) []db.Record {
	if len(input) == 0 {
		return input
	}

	// extract
	bucketsCnt := make([]int, len(buckets))
	allCnt := 0

	for _, rec := range input {
		var value float64

		recVal := rec[valueKey]
		switch val := recVal.(type) {
		case float32:
			value = float64(val)
		case float64:
			value = val
		case int:
			value = float64(val)
		case uint32:
			value = float64(val)
		case uint64:
			value = float64(val)
		case int32:
			value = float64(val)
		case int64:
			value = float64(val)
		default: // ignore other
			continue
		}

		for i, b := range buckets {
			if value <= b {
				bucketsCnt[i]++
			}
		}

		allCnt++
	}

	res := make([]db.Record, 0, len(buckets)+1)

	firstRec := input[0]

	for i, b := range buckets {
		row := support.CloneMap(firstRec)
		row["le"] = fmt.Sprintf("%0.2f", b)
		row["count"] = bucketsCnt[i]
		res = append(res, row)
	}

	// inf
	row := support.CloneMap(firstRec)
	row["le"] = "+Inf"
	row["count"] = allCnt
	res = append(res, row)

	return res
}

func tmplFuncBucketsInt(input []db.Record, valueKey string, buckets ...int) []db.Record {
	if len(input) == 0 {
		return input
	}

	// extract
	bucketsCnt := make([]int, len(buckets))
	allCnt := 0

	for _, rec := range input {
		var value int

		recVal := rec[valueKey]
		switch val := recVal.(type) {
		case int:
			value = val
		case uint32:
			value = int(val)
		case uint64:
			value = int(val)
		case int32:
			value = int(val)
		case int64:
			value = int(val)
		case float64:
			value = int(math.Ceil(val))
		case float32:
			value = int(math.Ceil(float64(val)))
		default:
			continue
		}

		for i, b := range buckets {
			if value <= b {
				bucketsCnt[i]++
			}
		}

		allCnt++
	}

	res := make([]db.Record, 0, len(buckets)+1)

	firstRec := input[0]

	for i, b := range buckets {
		row := support.CloneMap(firstRec)
		row["le"] = strconv.Itoa(b)
		row["count"] = bucketsCnt[i]
		res = append(res, row)
	}

	// inf
	row := support.CloneMap(firstRec)
	row["le"] = "+Inf"
	row["count"] = allCnt
	res = append(res, row)

	return res
}

// InitTemplates register template functions relate to Records.
func initTemplates() {
	support.FuncMap["buckets"] = tmplFuncBuckets
	support.FuncMap["bucketsInt"] = tmplFuncBucketsInt
}
