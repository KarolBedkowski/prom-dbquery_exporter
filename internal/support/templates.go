//
// templates.go
//

package support

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"text/template"
	"unicode"
)

// TemplateCompile try to compile template.
func TemplateCompile(name, tmpl string) (*template.Template, error) {
	t, err := template.New(name).Funcs(FuncMap).Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("template %s compile error: %w", name, err)
	}

	return t, nil
}

// FuncMap is global map of functions available in template.
var FuncMap = template.FuncMap{
	"toLower":                    strings.ToLower,
	"toUpper":                    strings.ToUpper,
	"trim":                       strings.TrimSpace,
	"quote":                      strconv.Quote,
	"replaceSpaces":              replaceSpaces,
	"removeSpaces":               removeSpaces,
	"keepAlfaNum":                keepAlfaNum,
	"keepAlfaNumUnderline":       keepAlfaNumUnderline,
	"keepAlfaNumUnderlineSpace":  keepAlfaNumUnderlineSpace,
	"keepAlfaNumUnderlineU":      keepAlfaNumUnderlineU,
	"keepAlfaNumUnderlineSpaceU": keepAlfaNumUnderlineSpaceU,
	"clean":                      clean,
	"removeQuotes":               removeQuotes,
	"genericBuckets":             buckets,
	"genericBucketsInt":          bucketsInt,
}

func replaceSpaces(i string) string {
	rmap := func(r rune) rune {
		if unicode.IsSpace(r) {
			return '_'
		}

		return r
	}

	return strings.Map(rmap, i)
}

func removeSpaces(input string) string {
	rmap := func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}

		return r
	}

	return strings.Map(rmap, input)
}

func keepAlfaNum(input string) string {
	rmap := func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		}

		return -1
	}

	return strings.Map(rmap, input)
}

func keepAlfaNumUnderline(input string) string {
	rmap := func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_':
			return r
		}

		return -1
	}

	return strings.Map(rmap, input)
}

func keepAlfaNumUnderlineSpace(input string) string {
	rmap := func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_':
			return r
		case r == ' ':
			return r
		}

		return -1
	}

	return strings.Map(rmap, input)
}

func keepAlfaNumUnderlineU(input string) string {
	rmap := func(r rune) rune {
		switch {
		case unicode.IsLetter(r):
			return r
		case unicode.IsDigit(r):
			return r
		case r == '_':
			return r
		}

		return -1
	}

	return strings.Map(rmap, input)
}

func keepAlfaNumUnderlineSpaceU(input string) string {
	rmap := func(r rune) rune {
		switch {
		case unicode.IsLetter(r):
			return r
		case unicode.IsDigit(r):
			return r
		case r == ' ':
			return r
		case r == '_':
			return r
		}

		return -1
	}

	return strings.Map(rmap, input)
}

func clean(i string) string {
	res := keepAlfaNumUnderlineSpace(i)
	res = strings.TrimSpace(res)
	res = replaceSpaces(res)
	res = strings.ReplaceAll(res, "__", "_")
	res = strings.ToLower(res)

	return res
}

func removeQuotes(i string) string {
	return strings.ReplaceAll(i, "\"", "'")
}

func buckets(input any, valueKey string, buckets ...float64) []map[string]any {
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
	bucketsCnt := make([]int, len(buckets))
	allCnt := 0
	vk := reflect.ValueOf(valueKey)

	for i := 0; i < inp.Len(); i++ {
		// get value
		rec := inp.Index(i)
		val := rec.MapIndex(vk).Interface()

		var value float64

		switch v := val.(type) {
		case float32:
			value = float64(v)
		case float64:
			value = v
		case int:
			value = float64(v)
		case uint32:
			value = float64(v)
		case uint64:
			value = float64(v)
		case int32:
			value = float64(v)
		case int64:
			value = float64(v)
		default:
			// ignore other
			continue
		}

		for i, b := range buckets {
			if value <= b {
				bucketsCnt[i]++
			}
		}

		allCnt++
	}

	res := make([]map[string]any, 0, len(buckets)+1)

	for i, b := range buckets {
		row := make(map[string]any, len(bucketsCnt))
		for k, v := range resKeyVal {
			row[k] = v
		}

		row["le"] = fmt.Sprintf("%0.2f", b)
		row["count"] = bucketsCnt[i]
		res = append(res, row)
	}

	// inf
	row := make(map[string]any)
	for k, v := range resKeyVal {
		row[k] = v
	}

	row["le"] = "+Inf"
	row["count"] = allCnt
	res = append(res, row)

	return res
}

func bucketsInt(input any, valueKey string, buckets ...int) []map[string]any {
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
	bucketsCnt := make([]int, len(buckets))
	allCnt := 0
	vk := reflect.ValueOf(valueKey)

	for i := 0; i < inp.Len(); i++ {
		// get value
		rec := inp.Index(i)
		val := rec.MapIndex(vk).Interface()

		var value int

		switch v := val.(type) {
		case int:
			value = v
		case uint32:
			value = int(v)
		case uint64:
			value = int(v)
		case int32:
			value = int(v)
		case int64:
			value = int(v)
		case float64:
			value = int(math.Ceil(v))
		case float32:
			value = int(math.Ceil(float64(v)))
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

	res := make([]map[string]any, 0, len(buckets)+1)

	for i, b := range buckets {
		row := make(map[string]any, len(bucketsCnt))
		for k, v := range resKeyVal {
			row[k] = v
		}

		row["le"] = strconv.Itoa(b)
		row["count"] = bucketsCnt[i]
		res = append(res, row)
	}

	// inf
	row := make(map[string]any)
	for k, v := range resKeyVal {
		row[k] = v
	}

	row["le"] = "+Inf"
	row["count"] = allCnt
	res = append(res, row)

	return res
}
