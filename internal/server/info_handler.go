package server

//
// info_handler.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"fmt"
	"net/http"
	"strings"
	"text/template"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/hlog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
	"prom-dbquery_exporter.app/internal/metrics"
)

const infoTmpl = `
DATABASES
=========

{{- range .Database}}
{{ .Name }}
-----------
- driver {{ .Driver }}
- connection:
  {{- range $key, $val := .Connection }}
    - {{ $key }}: {{ $val | redact $key }}
  {{- end }}
- labels:
  {{- range $key, $val := .Labels }}
    - {{ $key }}: {{ $val | printf "%v" }}
  {{- end }}

{{- end }}



QUERIES
=======

{{- range .Query}}
{{ .Name }}
-----------
- sql: {{ .SQL }}
- caching_time: {{ .CachingTime }}
- metrics: {{ .Metrics }}
- params:
  {{- range $key, $val := .Params }}
  - {{ $key }}: {{ $val | printf "%v"  }}
  {{- end }}


{{- end }}
`

func redact(key string, val any) string {
	if strings.HasPrefix(strings.ToLower(key), "pass") {
		return "***"
	}

	return fmt.Sprintf("%v", val)
}

var funcMap = template.FuncMap{
	"redact": redact,
}

// infoHandler handle request and return information about current configuration.
type infoHandler struct {
	Configuration *conf.Configuration

	tmpl *template.Template
}

// newInfoHandler create new info handler with logging and instrumentation.
func newInfoHandler(conf *conf.Configuration) *infoHandler {
	return &infoHandler{
		Configuration: conf,
		tmpl:          template.Must(template.New("info").Funcs(funcMap).Parse(infoTmpl)),
	}
}

func (q *infoHandler) Handler() http.Handler {
	h := newLogMiddleware(
		promhttp.InstrumentHandlerDuration(
			metrics.NewReqDurationWrapper("info"),
			q), "info", false)

	h = hlog.RequestIDHandler("req_id", "X-Request-Id")(h)
	h = hlog.NewHandler(log.Logger)(h)

	return h
}

func (q *infoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.RemoteAddr, "127.") && !strings.HasPrefix(r.RemoteAddr, "localhost:") {
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if err := q.tmpl.Execute(w, q.Configuration); err != nil {
		log.Logger.Error().Err(err).Msg("infohandler: executing template error")
	}
}
