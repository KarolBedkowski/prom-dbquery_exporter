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

func newStandardParams(cfg map[string]any) standardParams {
	params := standardParams{}

	for key, val := range cfg {
		vstr := ""
		if val != nil {
			vstr = fmt.Sprintf("%v", val)
		}

		switch key {
		case "database": //nolint: goconst
			params.dbname = url.PathEscape(vstr)
		case "host":
			params.host = url.PathEscape(vstr)
		case "port":
			params.port = url.PathEscape(vstr)
		case "user":
			params.user = url.PathEscape(vstr)
		case "password":
			params.pass = url.PathEscape(vstr)
		default:
			params.params.Add(key, vstr)
		}
	}

	return params
}
