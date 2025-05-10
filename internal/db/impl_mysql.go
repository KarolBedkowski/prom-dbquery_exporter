//go:build mysql
// +build mysql

// impl_pg.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

import (
	"strings"

	// import mysql package only when mysql tag is enabled.
	_ "github.com/go-sql-driver/mysql"
	"prom-dbquery_exporter.app/internal/conf"
)

func newMysqlLoader(cfg *conf.Database) (Database, error) {
	params := &standardParams{
		host: "localhost",
		port: "3306",
	}

	params.load(cfg.Connection)

	if params.dbname == "" {
		return nil, ErrNoDatabaseName
	}

	var connstr strings.Builder

	if params.user != "" {
		connstr.WriteString(params.user)

		if params.pass != "" {
			connstr.WriteRune(':')
			connstr.WriteString(params.pass)
		}

		connstr.WriteRune('@')
	}

	connstr.WriteString("tcp(")
	connstr.WriteString(params.host)
	connstr.WriteRune(':')
	connstr.WriteString(params.port)
	connstr.WriteString(")/")
	connstr.WriteString(params.dbname)

	if len(params.params) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(params.params.Encode())
	}

	l := &genericDatabase{
		connStr: connstr.String(), driver: "mysql", initialSQL: cfg.InitialQuery,
		dbConf: cfg,
	}

	return l, nil
}

func init() {
	registerDatabase(dbDefinition{newMysqlLoader, nil}, "mssql", "mariadb", "tidb")
}
