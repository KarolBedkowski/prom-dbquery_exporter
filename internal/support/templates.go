//
// templates.go
//

package support

import (
	"fmt"
	"strconv"
	"strings"
	"text/template"
	"unicode"
)

// TemplateCompile try to compile template.
func TemplateCompile(name, tmpl string) (*template.Template, error) {
	t, err := template.New(name).Funcs(templateFuncsMap).Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("template %s compile error: %w", name, err)
	}

	return t, nil
}

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
