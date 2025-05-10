//go:build sqlite
// +build sqlite

// impl_pg.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

import (
	"fmt"
	"net/url"
	"strings"

	// import go-sqlite package only when sqlite tag is enabled.
	_ "github.com/glebarez/go-sqlite"
	"prom-dbquery_exporter.app/internal/conf"
)

func newSqliteLoader(cfg *conf.Database) (Database, error) {
	params := url.Values{}

	var dbname string

	for k, v := range cfg.Connection {
		vstr := ""
		if v != nil {
			vstr = fmt.Sprintf("%v", v)
		}

		if k == "database" { //nolint: goconst,nolintlint
			dbname = vstr
		} else {
			params.Add(k, vstr)
		}
	}

	if dbname == "" {
		return nil, ErrNoDatabaseName
	}

	var connstr strings.Builder

	connstr.WriteString("file:")
	connstr.WriteString(dbname)

	if len(params) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(params.Encode())
	}

	// glebarez/go-sqlite uses 'sqlite', mattn/go-sqlite3 - 'sqlite3'
	l := &genericDatabase{
		connStr: connstr.String(), driver: "sqlite", initialSQL: cfg.InitialQuery,
		dbConf: cfg,
	}

	if len(params) > 0 {
		l.connStr += "?" + params.Encode()
	}

	return l, nil
}

func init() {
	registerDatabase(dbDefinition{newSqliteLoader, nil}, "sqlite3", "sqlite")
}
