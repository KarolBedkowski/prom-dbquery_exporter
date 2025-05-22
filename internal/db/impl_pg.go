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

func init() {
	registerDatabase(postgresqlDef{},
		"postgresql", "postgres", "cockroach", "cockroachdb")
}

type postgresqlDef struct{}

func (postgresqlDef) instanate(cfg *conf.Database) (Database, error) {
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
		dbCfg:      cfg,
	}

	return l, nil
}

func (postgresqlDef) validateConf(cfg *conf.Database) error {
	// if connstr is confiured to not check other parameters
	if checkConnectionParam(cfg, "connstr") == nil {
		return nil
	}

	var errs *multierror.Error

	if err := checkConnectionParam(cfg, "database"); err != nil {
		if err := checkConnectionParam(cfg, "dbname"); err != nil {
			errs = multierror.Append(errs, conf.MissingFieldError("'database' or 'dbname'"))
		}
	}

	if err := checkConnectionParam(cfg, "user"); err != nil {
		errs = multierror.Append(errs, err)
	}

	return errs.ErrorOrNil()
}
