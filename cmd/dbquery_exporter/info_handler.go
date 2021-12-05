package main

import (
	"fmt"
	"net/http"
	"strings"
	"text/template"
)

//
// info_handler.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

type infoHandler struct {
	Configuration *Configuration
}

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

func redact(key string, val interface{}) string {
	if strings.HasPrefix(strings.ToLower(key), "pass") {
		return "***"
	}
	return fmt.Sprintf("%v", val)
}

var funcMap = template.FuncMap{
	"redact": redact,
}

func (q infoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.RemoteAddr, "127.0.0.1:") && !strings.HasPrefix(r.RemoteAddr, "localhost:") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	t := template.Must(template.New("letter").Funcs(funcMap).Parse(infoTmpl))
	err := t.Execute(w, q.Configuration)
	if err != nil {
		Logger.Error().Err(err).Msg("executing template error")
	}
}
