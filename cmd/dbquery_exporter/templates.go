//
// templates.go
//

package main

import (
	"strconv"
	"strings"
	"text/template"
	"unicode"
)

var templateFuncsMap = template.FuncMap{
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

func removeSpaces(i string) string {
	rmap := func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}
	return strings.Map(rmap, i)
}

func keepAlfaNum(i string) string {
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
	return strings.Map(rmap, i)
}

func keepAlfaNumUnderline(i string) string {
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
	return strings.Map(rmap, i)
}

func keepAlfaNumUnderlineSpace(i string) string {
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
	return strings.Map(rmap, i)
}

func keepAlfaNumUnderlineU(i string) string {
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

	return strings.Map(rmap, i)
}

func keepAlfaNumUnderlineSpaceU(i string) string {
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

	return strings.Map(rmap, i)
}

func clean(i string) string {
	res := keepAlfaNumUnderlineSpace(i)
	res = strings.TrimSpace(res)
	res = replaceSpaces(res)
	res = strings.ReplaceAll(res, "__", "_")
	res = strings.ToLower(res)
	return res
}
