//go:build tmpl_extra_func
// +build tmpl_extra_func

package templates

//
// templates_extra.go
//

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"strconv"
)

func init() {
	FuncMap["buckets"] = buckets
	FuncMap["bucketsInt"] = bucketsInt
	FuncMap["genericBuckets"] = buckets
	FuncMap["genericBucketsInt"] = bucketsInt
}

// Number is any numeric type.
type Number interface {
	int | uint | int8 | uint8 | int16 | uint16 | int32 | uint32 | int64 | uint64 | float32 | float64
}

type reflectBucketGenerator[T Number] struct {
	buckets   []T
	converter func(any) (T, bool)
	formatter func(T) string
}

func (r *reflectBucketGenerator[T]) generate(input any, valueKey string) []map[string]any {
	inp := reflect.ValueOf(input)
	if inp.Len() == 0 {
		return nil
	}

	// get key-vals from first row:
	resKeyVal := make(map[string]any)

	iter := inp.Index(0).MapRange()
	for iter.Next() {
		resKeyVal[iter.Key().String()] = iter.Value().Interface()
	}

	// extract
	bucketsCnt := make([]int, len(r.buckets))
	allCnt := 0
	vk := reflect.ValueOf(valueKey)

	for i := range inp.Len() {
		rec := inp.Index(i)
		val := rec.MapIndex(vk).Interface()

		value, ok := r.converter(val)
		if !ok {
			continue
		}

		for i, b := range r.buckets {
			if value <= b {
				bucketsCnt[i]++
			}
		}

		allCnt++
	}

	res := make([]map[string]any, 0, len(r.buckets)+1)

	for i, b := range r.buckets {
		row := make(map[string]any, len(resKeyVal)+2) //nolint:mnd
		maps.Copy(row, resKeyVal)
		row["le"] = r.formatter(b)
		row["count"] = bucketsCnt[i]
		res = append(res, row)
	}

	// inf
	row := make(map[string]any, len(resKeyVal)+2) //nolint:mnd
	maps.Copy(row, resKeyVal)
	row["le"] = "+Inf"
	row["count"] = allCnt
	res = append(res, row)

	return res
}

func buckets(input any, valueKey string, buckets ...float64) []map[string]any {
	gen := &reflectBucketGenerator[float64]{
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

func bucketsInt(input any, valueKey string, buckets ...int) []map[string]any {
	gen := &reflectBucketGenerator[int]{
		buckets: buckets,
		converter: func(recVal any) (int, bool) {
			var value int

			switch val := recVal.(type) {
			case int:
				value = val
			case uint32:
				value = int(val)
			case uint64:
				if val < uint64(math.MaxInt) {
					value = int(val)
				} else {
					value = math.MaxInt
				}
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
