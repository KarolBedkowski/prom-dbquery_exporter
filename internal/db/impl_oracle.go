//go:build oracle
// +build oracle

// impl_oracle.go
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

func init() {
	registerDatabase(oracleDef{}, "oracle", "oci8")
}

type oracleDef struct{}

func (o oracleDef) instanate(cfg *conf.Database) (Database, error) {
	params := newStandardParams(cfg.Connection)
	connstr := o.connstr(params)

	return newGenericDatabase(connstr, "oracle", cfg), nil
}

func (oracleDef) validateConf(cfg *conf.Database) error {
	return checkConnectionParam(cfg, "database", "host")
}

func (oracleDef) connstr(params standardParams) string {
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
	connstr.WriteString(params.database)

	if len(params.params) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(params.params.Encode())
	}

	return connstr.String()
}
