package db

//
// loaders.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"fmt"
	"net/url"
)

type standardParams struct {
	params url.Values
	dbname string
	user   string
	pass   string
	host   string
	port   string
}

func newStandardParams(cfg map[string]any) *standardParams {
	s := &standardParams{}
	s.load(cfg)

	return s
}

func (s *standardParams) load(cfg map[string]any) {
	for key, val := range cfg {
		vstr := ""
		if val != nil {
			vstr = fmt.Sprintf("%v", val)
		}

		switch key {
		case "database": //nolint: goconst
			s.dbname = url.PathEscape(vstr)
		case "host":
			s.host = url.PathEscape(vstr)
		case "port":
			s.port = url.PathEscape(vstr)
		case "user":
			s.user = url.PathEscape(vstr)
		case "password":
			s.pass = url.PathEscape(vstr)
		default:
			s.params.Add(key, vstr)
		}
	}
}
