package main

import (
	"fmt"
	"net/http"
	"strings"
)

//
// info_handler.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

type infoHndler struct {
	Configuration *Configuration
}

func (q infoHndler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if !strings.HasPrefix(r.RemoteAddr, "127.0.0.1:") && !strings.HasPrefix(r.RemoteAddr, "localhost:") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	w.Write([]byte("DATABASES\n========="))
	for name, db := range q.Configuration.Database {
		w.Write([]byte{'\n'})
		w.Write([]byte(name))
		w.Write([]byte("\n-----"))
		w.Write([]byte("\n - driver: "))
		w.Write([]byte(db.Driver))
		w.Write([]byte("\n - connection:\n"))
		for k, v := range db.Connection {
			w.Write([]byte("    - "))
			w.Write([]byte(k))
			w.Write([]byte(": "))
			if strings.HasPrefix(strings.ToLower(k), "pass") {
				w.Write([]byte("*"))
			} else {
				w.Write([]byte(fmt.Sprintf("%v", v)))
			}
			w.Write([]byte{'\n'})
		}
		w.Write([]byte(" - labels:\n"))
		for k, v := range db.Labels {
			w.Write([]byte("    - "))
			w.Write([]byte(k))
			w.Write([]byte(": "))
			w.Write([]byte(fmt.Sprintf("%v", v)))
			w.Write([]byte{'\n'})
		}
	}

	w.Write([]byte("\n\nQueries\n======="))
	for name, q := range q.Configuration.Query {
		w.Write([]byte{'\n'})
		w.Write([]byte(name))
		w.Write([]byte("\n-----"))
		w.Write([]byte("\n - sql: "))
		w.Write([]byte(q.SQL))
		w.Write([]byte("\n - caching time: "))
		w.Write([]byte(fmt.Sprintf("%v", q.CachingTime)))
		w.Write([]byte("\n - metrics:\n"))
		w.Write([]byte(q.Metrics))
		w.Write([]byte("\n - params:\n"))
		for k, v := range q.Params {
			w.Write([]byte("    - "))
			w.Write([]byte(k))
			w.Write([]byte(": "))
			w.Write([]byte(fmt.Sprintf("%v", v)))
			w.Write([]byte{'\n'})
		}
	}
}
