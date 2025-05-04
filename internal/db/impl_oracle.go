//go:build oracle
// +build oracle

// impl_pg.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

import (
	"strings"

	// import go-ora package only when oracle tag is enabled.
	_ "github.com/sijms/go-ora/v2"
	"prom-dbquery_exporter.app/internal/conf"
)

func newOracleLoader(cfg *conf.Database) (Database, error) {
	params := newStandardParams(cfg.Connection)

	if params.dbname == "" {
		return nil, ErrNoDatabaseName
	}

	var connstr strings.Builder

	connstr.WriteString("oracle://")

	if params.user != "" {
		connstr.WriteString(params.user)

		if params.pass != "" {
			connstr.WriteRune(':')
			connstr.WriteString(params.pass)
		}

		connstr.WriteRune('@')
	}

	connstr.WriteString(params.host)

	if params.port != "" {
		connstr.WriteRune(':')
		connstr.WriteString(params.port)
	}

	connstr.WriteRune('/')
	connstr.WriteString(params.dbname)

	if len(params.params) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(params.params.Encode())
	}

	l := &genericDatabase{
		connStr: connstr.String(), driver: "oracle", initialSQL: cfg.InitialQuery,
		dbConf: cfg,
	}

	return l, nil
}

func init() {
	registerDatabase(newOracleLoader, "oracle", "oci8")
}
