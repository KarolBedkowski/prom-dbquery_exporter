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
	Params map[string]any
	// Parsed template  (internal)
	MetricTpl *template.Template `yaml:"-"`
	// Parsed template for error response (internal)
	OnErrorTpl *template.Template `yaml:"-"`
	// SQL script to launch
	SQL string
	// Template to generate from query result
	Metrics string
	// Template to generate result on query error
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
func (q *Query) MarshalZerologObject(e *zerolog.Event) {
	e.Str("sql", q.SQL).
		Str("metrics", q.Metrics).
		Str("onerror", q.OnError).
		Interface("params", q.Params).
		Dur("caching_time", q.CachingTime).
		Dur("timeout", q.Timeout).
		Strs("groups", q.Groups).
		Str("name", q.Name)
}

func (q *Query) setup(name string) {
	q.Name = name
	q.SQL = strings.TrimSpace(q.SQL)
	q.Metrics = strings.TrimSpace(q.Metrics)
	q.OnError = strings.TrimSpace(q.OnError)
}

func (q *Query) validate() error {
	if q.SQL == "" {
		return MissingFieldError("sql")
	}

	if q.Metrics == "" {
		return MissingFieldError("metrics template")
	}

	tmpl, err := support.TemplateCompile(q.Name, q.Metrics+"\n")
	if err != nil {
		return NewInvalidFieldError("metrics template", "", err.Error())
	}

	q.MetricTpl = tmpl

	// parse template for error response (if configured)
	if q.OnError != "" {
		tmpl, err := support.TemplateCompile(q.Name, q.OnError+"\n")
		if err != nil {
			return NewInvalidFieldError("on error template", "", err.Error())
		}

		q.OnErrorTpl = tmpl
	}

	if q.Timeout.Seconds() < 1 && q.Timeout > 0 {
		log.Logger.Warn().Msgf("configuration: query %v: timeout < 1s: %v", q.Name, q.Timeout)
	}

	if q.CachingTime.Seconds() < 1 && q.CachingTime > 0 {
		log.Logger.Warn().Msgf("configuration: query %v: caching_time < 1s: %v", q.Name, q.CachingTime)
	}

	return nil
}
