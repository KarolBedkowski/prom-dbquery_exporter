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
	"strings"

	"prom-dbquery_exporter.app/internal/conf"
)

func newPostgresLoader(cfg *conf.Database) (Loader, error) {
	var connStr string
	if val, ok := cfg.Connection["connstr"]; ok && val != "" {
		connStr, ok = val.(string)

		if !ok {
			return nil, InvalidConfigrationError(fmt.Sprintf("invalid 'connstr' value: %v", val))
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

	l := &genericLoader{
		connStr:    connStr,
		driver:     "postgres",
		initialSQL: cfg.InitialQuery,
		dbConf:     cfg,
	}

	return l, nil
}

func newSqliteLoader(cfg *conf.Database) (Loader, error) {
	params := url.Values{}

	var dbname string

	for k, v := range cfg.Connection {
		vstr := ""
		if v != nil {
			vstr = fmt.Sprintf("%v", v)
		}

		if k == "database" { //nolint:goconst
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
	l := &genericLoader{
		connStr: connstr.String(), driver: "sqlite", initialSQL: cfg.InitialQuery,
		dbConf: cfg,
	}

	if len(params) > 0 {
		l.connStr += "?" + params.Encode()
	}

	return l, nil
}

func newMysqlLoader(cfg *conf.Database) (Loader, error) {
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

	l := &genericLoader{
		connStr: connstr.String(), driver: "mysql", initialSQL: cfg.InitialQuery,
		dbConf: cfg,
	}

	return l, nil
}

type standardParams struct {
	dbname, user, pass, host, port string
	params                         url.Values
}

func newStandardParams(cfg map[string]any) *standardParams {
	s := &standardParams{}

	s.load(cfg)

	return s
}

func (s *standardParams) load(cfg map[string]any) {
	for key, val := range cfg {
		vstr := ""
		if val != nil {
			vstr = fmt.Sprintf("%v", val)
		}

		switch key {
		case "database":
			s.dbname = url.PathEscape(vstr)
		case "host":
			s.host = url.PathEscape(vstr)
		case "port":
			s.port = url.PathEscape(vstr)
		case "user":
			s.user = url.PathEscape(vstr)
		case "password":
			s.pass = url.PathEscape(vstr)
		default:
			s.params.Add(key, vstr)
		}
	}
}

func newOracleLoader(cfg *conf.Database) (Loader, error) {
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

	l := &genericLoader{
		connStr: connstr.String(), driver: "oracle", initialSQL: cfg.InitialQuery,
		dbConf: cfg,
	}

	return l, nil
}

func newMssqlLoader(cfg *conf.Database) (Loader, error) {
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
		return nil, InvalidConfigrationError("missing database")
	}

	connstr := params.Encode()

	l := &genericLoader{
		connStr: connstr, driver: "mssql", initialSQL: cfg.InitialQuery,
		dbConf: cfg,
	}

	return l, nil
}

// newLoader returns configured Loader for given configuration.
func newLoader(cfg *conf.Database) (Loader, error) {
	switch cfg.Driver {
	case "postgresql", "postgres", "cockroach", "cockroachdb":
		return newPostgresLoader(cfg)
	case "sqlite3", "sqlite":
		return newSqliteLoader(cfg)
	case "mysql", "mariadb", "tidb":
		return newMysqlLoader(cfg)
	case "oracle", "oci8":
		return newOracleLoader(cfg)
	case "mssql":
		return newMssqlLoader(cfg)
	}

	return nil, InvalidConfigrationError(fmt.Sprintf("unsupported database type '%s'", cfg.Driver))
}
