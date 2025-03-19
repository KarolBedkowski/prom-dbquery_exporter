package conf

import (
	"strings"
	"text/template"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
	// Query params
	Params map[string]interface{}
	// Parsed template  (internal)
	MetricTpl *template.Template `yaml:"-"`
	// Parsed template for error response (internal)
	OnErrorTpl *template.Template `yaml:"-"`
	// SQL script to launch
	SQL string
	// Template to generate from query result
	Metrics string
	// Template to generate result on error
	OnError string `yaml:"on_error"`
	// Query name for internal use
	Name string `yaml:"-"`

	// Groups define group names that query belong to
	Groups []string `yaml:"groups"`
	// Result caching time
	CachingTime time.Duration `yaml:"caching_time"`
	// Max time for query result
	Timeout time.Duration `yaml:"timeout"`
}

// MarshalZerologObject implements LogObjectMarshaler.
func (q Query) MarshalZerologObject(e *zerolog.Event) {
	e.Str("sql", q.SQL).
		Str("metrics", q.Metrics).
		Str("onerror", q.OnError).
		Interface("params", q.Params).
		Dur("caching_time", q.CachingTime).
		Dur("timeout", q.Timeout).
		Strs("groups", q.Groups).
		Str("name", q.Name)
}

func (q *Query) validate() error {
	if q.SQL == "" {
		return MissingFieldError{"sql"}
	}

	q.SQL = strings.TrimSpace(q.SQL)

	m := strings.TrimSpace(q.Metrics) + "\n"
	if m == "" {
		return MissingFieldError{"metrics template"}
	}

	tmpl, err := support.TemplateCompile(q.Name, m)
	if err != nil {
		return NewInvalidFieldError("metrics template", "").WithMsg(err.Error())
	}

	q.MetricTpl = tmpl

	// parse template for error response (if configured)
	merr := strings.TrimSpace(q.OnError)
	if merr != "" {
		tmpl, err := support.TemplateCompile(q.Name, merr+"\n")
		if err != nil {
			return NewInvalidFieldError("on error template", "").WithMsg(err.Error())
		}

		q.OnErrorTpl = tmpl
	}

	if q.Timeout.Seconds() < 1 && q.Timeout > 0 {
		log.Logger.Warn().Msgf("query %v: timeout < 1s: %v", q.Name, q.Timeout)
	}

	if q.CachingTime.Seconds() < 1 && q.CachingTime > 0 {
		log.Logger.Warn().Msgf("query %v: caching_time < 1s: %v", q.Name, q.CachingTime)
	}

	return nil
}
