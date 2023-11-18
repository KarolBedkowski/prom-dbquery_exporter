package conf

import (
	"strings"
	"text/template"

	"prom-dbquery_exporter.app/internal/support"
)

//
// query.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

// Query is definition of single query.
type Query struct {
	// SQL script to launch
	SQL string
	// Template to generate from query result
	Metrics string
	// Query params
	Params map[string]interface{}
	// Result caching time
	CachingTime uint `yaml:"caching_time"`
	// Max time for query result
	Timeout uint `yaml:"timeout"`

	// Groups define group names that query belong to
	Groups []string `yaml:"groups"`

	// Parsed template  (internal)
	MetricTpl *template.Template `yaml:"-"`
	// Query name for internal use
	Name string `yaml:"-"`
}

func (q *Query) validate() error {
	if q.SQL == "" {
		return MissingFieldError{"sql"}
	}

	m := strings.TrimSpace(q.Metrics) + "\n"
	if m == "" {
		return MissingFieldError{"metrics template"}
	}

	tmpl, err := support.TemplateCompile(q.Name, m)
	if err != nil {
		return NewInvalidFieldError("metrics template", "").WithMsg(err.Error())
	}

	q.MetricTpl = tmpl

	return nil
}
