//go:build mssql
// +build mssql

// impl_mssql.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

import (
	"fmt"
	"net/url"

	// import go-mssqldb only when mssql tag is enabled.
	_ "github.com/denisenkom/go-mssqldb"
	"prom-dbquery_exporter.app/internal/conf"
)

func init() {
	registerDatabase(dbDefinition{newMssqlLoader, validateMssqlConf}, "mssql")
}

func newMssqlLoader(cfg *conf.Database) (Database, error) {
	params := url.Values{}
	databaseConfigured := false

	for k, v := range cfg.Connection {
		if v != nil {
			vstr := fmt.Sprintf("%v", v)
			params.Add(k, vstr)

			if k == "database" {
				databaseConfigured = true
			}
		}
	}

	if !databaseConfigured {
		return nil, InvalidConfigurationError("missing database")
	}

	connstr := params.Encode()

	l := &genericDatabase{
		connStr: connstr, driver: "mssql", initialSQL: cfg.InitialQuery,
		dbConf: cfg,
	}

	return l, nil
}

func validateMssqlConf(cfg *conf.Database) error {
	return checkConnectionParam(cfg, "database")
}
