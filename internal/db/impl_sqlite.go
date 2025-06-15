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

func init() {
	registerDatabase(sqliteDef{}, "sqlite3", "sqlite")
}

type sqliteDef struct{}

func (sqliteDef) instanate(cfg *conf.Database) (Database, error) {
	var (
		params url.Values
		dbname string
	)

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

	var connstr strings.Builder

	connstr.WriteString("file:")
	connstr.WriteString(dbname)

	if len(params) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(params.Encode())
	}

	// glebarez/go-sqlite uses 'sqlite', mattn/go-sqlite3 - 'sqlite3'
	return newGenericDatabase(connstr.String(), "sqlite", cfg), nil
}

func (sqliteDef) validateConf(cfg *conf.Database) error {
	return checkConnectionParam(cfg, "database")
}
