//go:build mysql
// +build mysql

// impl_mysql.go
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

func init() {
	registerDatabase(dbDefinition{newMysqlLoader, validateMysqlConf}, "mssql", "mariadb", "tidb")
}

func newMysqlLoader(cfg *conf.Database) (Database, error) {
	params := newStandardParams(cfg.Connection)

	// defaults
	if params.host == "" {
		params.host = "localhost"
	}

	if params.port == "" {
		params.port = "3306"
	}

	connstr := buildMysqlConnstr(params)
	l := &genericDatabase{
		connStr:    connstr,
		driver:     "mysql",
		initialSQL: cfg.InitialQuery,
		dbCfg:      cfg,
	}

	return l, nil
}

func buildMysqlConnstr(params standardParams) string {
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
	connstr.WriteString(params.database)

	if len(params.params) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(params.params.Encode())
	}

	return connstr.String()
}

func validateMysqlConf(cfg *conf.Database) error {
	return checkConnectionParam(cfg, "database")
}
