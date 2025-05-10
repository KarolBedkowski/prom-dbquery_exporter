//go:build pg
// +build pg

// impl_pg.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

import (
	"fmt"
	"strings"

	// import pg package only when pg tag is enabled.
	"github.com/hashicorp/go-multierror"
	_ "github.com/lib/pq" //noqa:revive
	"prom-dbquery_exporter.app/internal/conf"
)

func newPostgresLoader(cfg *conf.Database) (Database, error) {
	var connStr string
	if val, ok := cfg.Connection["connstr"]; ok && val != "" {
		connStr, ok = val.(string)

		if !ok {
			return nil, InvalidConfigurationError(fmt.Sprintf("invalid 'connstr' value: %v", val))
		}
	} else {
		p := make([]string, 0, len(cfg.Connection))

		for k, v := range cfg.Connection {
			if v != nil {
				vstr := fmt.Sprintf("%v", v)
				vstr = strings.ReplaceAll(vstr, "'", "\\'")
				p = append(p, k+"='"+vstr+"'")
			} else {
				p = append(p, k+"=")
			}
		}

		connStr = strings.Join(p, " ")
	}

	l := &genericDatabase{
		connStr:    connStr,
		driver:     "postgres",
		initialSQL: cfg.InitialQuery,
		dbConf:     cfg,
	}

	return l, nil
}

func init() {
	registerDatabase(dbDefinition{newPostgresLoader, validatePG}, "postgresql", "postgres", "cockroach", "cockroachdb")
}

func validatePG(cfg *conf.Database) error {
	if CheckConnectionParam(cfg, "connstr") == nil {
		return nil
	}

	var errs *multierror.Error

	if err := CheckConnectionParam(cfg, "database"); err != nil {
		if err := CheckConnectionParam(cfg, "dbname"); err != nil {
			errs = multierror.Append(errs, conf.MissingFieldError{Field: "'database' or 'dbname'"})
		}
	}

	if err := CheckConnectionParam(cfg, "user"); err != nil {
		errs = multierror.Append(errs, err)
	}

	return errs.ErrorOrNil()
}
