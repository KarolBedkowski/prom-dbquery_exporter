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

// --------------------------------------------------------------------------------

// infoHandler handle request and return information about current configuration.
type infoHandler struct {
	cfg  *conf.Configuration
	tmpl *template.Template
}

// newInfoHandler create new info handler with logging and instrumentation.
func newInfoHandler(cfg *conf.Configuration) *infoHandler {
	return &infoHandler{
		cfg:  cfg,
		tmpl: template.Must(template.New("info").Funcs(funcMap).Parse(infoTmpl)),
	}
}

func (q *infoHandler) Handler() http.Handler {
	var handler http.Handler = q

	handler = newLogMiddleware(handler)
	handler = hlog.RequestIDHandler("req_id", "X-Request-Id")(handler)
	handler = hlog.NewHandler(log.Logger)(handler)
	handler = promhttp.InstrumentHandlerDuration(newReqDurationWrapper("info"), handler)

	return handler
}

func (q *infoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger := log.Ctx(r.Context())

	if !isRemoteLocal(r, conf.Args.ListenAddress) {
		logger.Info().Msg("infohandler: remote not accepted")
		http.Error(w, "forbidden", http.StatusForbidden)

		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	if err := q.tmpl.Execute(w, q.cfg); err != nil {
		logger.Error().Err(err).Msg("infohandler: executing template error")
	}
}

func isRemoteLocal(r *http.Request, listenAddress string) bool {
	if strings.HasPrefix(r.RemoteAddr, "127.") || strings.HasPrefix(r.RemoteAddr, "localhost:") {
		return true
	}

	// accept connection if remote address is the same as listen address
	if listenhost, _, ok := strings.Cut(listenAddress, ":"); ok && listenhost != "" {
		return strings.HasPrefix(r.RemoteAddr, listenhost+":")
	}

	return false
}
