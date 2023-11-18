package db

//
// loaders.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"errors"
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
			return nil, fmt.Errorf("invalid 'connstr' value: %v", val)
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

		if k == "database" {
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
	params := url.Values{}
	host := "localhost"
	port := "3306"

	var dbname, user, pass string

	for key, val := range cfg.Connection {
		vstr := ""
		if val != nil {
			vstr = fmt.Sprintf("%v", val)
		}

		switch key {
		case "database":
			dbname = url.PathEscape(vstr)
		case "host":
			host = url.PathEscape(vstr)
		case "port":
			port = url.PathEscape(vstr)
		case "user":
			user = url.PathEscape(vstr)
		case "password":
			pass = url.PathEscape(vstr)
		default:
			params.Add(key, vstr)
		}
	}

	if dbname == "" {
		return nil, ErrNoDatabaseName
	}

	var connstr strings.Builder

	if user != "" {
		connstr.WriteString(user)

		if pass != "" {
			connstr.WriteRune(':')
			connstr.WriteString(pass)
		}

		connstr.WriteRune('@')
	}

	connstr.WriteString("tcp(")
	connstr.WriteString(host)
	connstr.WriteRune(':')
	connstr.WriteString(port)
	connstr.WriteString(")/")
	connstr.WriteString(dbname)

	if len(params) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(params.Encode())
	}

	l := &genericLoader{
		connStr: connstr.String(), driver: "mysql", initialSQL: cfg.InitialQuery,
		dbConf: cfg,
	}

	return l, nil
}

func newOracleLoader(cfg *conf.Database) (Loader, error) {
	params := url.Values{}

	var dbname, user, pass, host, port string

	for key, val := range cfg.Connection {
		vstr := ""
		if val != nil {
			vstr = fmt.Sprintf("%v", val)
		}

		switch key {
		case "database":
			dbname = url.PathEscape(vstr)
		case "host":
			host = url.PathEscape(vstr)
		case "port":
			port = url.PathEscape(vstr)
		case "user":
			user = url.PathEscape(vstr)
		case "password":
			pass = url.PathEscape(vstr)
		default:
			params.Add(key, vstr)
		}
	}

	if dbname == "" {
		return nil, ErrNoDatabaseName
	}

	var connstr strings.Builder

	connstr.WriteString("oracle://")

	if user != "" {
		connstr.WriteString(user)

		if pass != "" {
			connstr.WriteRune(':')
			connstr.WriteString(pass)
		}

		connstr.WriteRune('@')
	}

	connstr.WriteString(host)

	if port != "" {
		connstr.WriteRune(':')
		connstr.WriteString(port)
	}

	connstr.WriteRune('/')
	connstr.WriteString(dbname)

	if len(params) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(params.Encode())
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
		return nil, errors.New("missing database")
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

	return nil, fmt.Errorf("unsupported database type '%s'", cfg.Driver)
}
