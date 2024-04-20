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

// Number is any numeric type.
type Number interface {
	int | uint | int8 | uint8 | int16 | uint16 | int32 | uint32 | int64 | uint64 | float32 | float64
}

type bucketGenerator[T Number] struct {
	buckets   []T
	converter func(any) (T, bool)
	formatter func(T) string
}

func (b *bucketGenerator[T]) generate(input []db.Record, valueKey string) []db.Record {
	if len(input) == 0 {
		return input
	}

	// extract
	bucketsCnt := make([]int, len(b.buckets))
	allCnt := 0

	for _, rec := range input {
		value, ok := b.converter(rec[valueKey])
		if !ok {
			continue
		}

		for i, b := range b.buckets {
			if value <= b {
				bucketsCnt[i]++
			}
		}

		allCnt++
	}

	res := make([]db.Record, 0, len(b.buckets)+1)

	firstRec := input[0]

	for i, bc := range b.buckets {
		row := support.CloneMap(firstRec)
		row["le"] = b.formatter(bc)
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

func tmplFuncBuckets(input []db.Record, valueKey string, buckets ...float64) []db.Record {
	if len(input) == 0 {
		return input
	}

	gen := &bucketGenerator[float64]{
		buckets: buckets,
		converter: func(recVal any) (float64, bool) {
			var value float64

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
				return 0.0, false
			}

			return value, true
		},
		formatter: func(val float64) string {
			return fmt.Sprintf("%0.2f", val)
		},
	}

	return gen.generate(input, valueKey)
}

func tmplFuncBucketsInt(input []db.Record, valueKey string, buckets ...int) []db.Record {
	if len(input) == 0 {
		return input
	}

	gen := &bucketGenerator[int]{
		buckets: buckets,
		converter: func(recVal any) (int, bool) {
			var value int

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
			default: // ignore other
				return 0, false
			}

			return value, true
		},
		formatter: strconv.Itoa,
	}

	return gen.generate(input, valueKey)
}

// InitTemplates register template functions relate to Records.
func initTemplates() {
	support.FuncMap["buckets"] = tmplFuncBuckets
	support.FuncMap["bucketsInt"] = tmplFuncBucketsInt
}
