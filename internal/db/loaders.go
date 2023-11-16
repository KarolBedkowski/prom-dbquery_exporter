package db

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"prom-dbquery_exporter.app/internal/conf"
)

//
// loaders.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

func newPostgresLoader(d *conf.Database) (Loader, error) {
	var connStr string
	if val, ok := d.Connection["connstr"]; ok && val != "" {
		connStr = val.(string)
	} else {
		p := make([]string, 0, len(d.Connection))

		for k, v := range d.Connection {
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
		initialSQL: d.InitialQuery,
		dbConf:     d,
	}

	return l, nil
}

func newSqliteLoader(d *conf.Database) (Loader, error) {
	p := url.Values{}

	var dbname string

	for k, v := range d.Connection {
		vstr := ""
		if v != nil {
			vstr = fmt.Sprintf("%v", v)
		}

		if k == "database" {
			dbname = vstr
		} else {
			p.Add(k, vstr)
		}
	}

	if dbname == "" {
		return nil, errors.New("missing database")
	}

	var connstr strings.Builder

	connstr.WriteString("file:")
	connstr.WriteString(dbname)

	if len(p) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(p.Encode())
	}

	// glebarez/go-sqlite uses 'sqlite', mattn/go-sqlite3 - 'sqlite3'
	l := &genericLoader{
		connStr: connstr.String(), driver: "sqlite", initialSQL: d.InitialQuery,
		dbConf: d,
	}
	if len(p) > 0 {
		l.connStr += "?" + p.Encode()
	}

	return l, nil
}

func newMysqlLoader(d *conf.Database) (Loader, error) {
	p := url.Values{}
	host := "localhost"
	port := "3306"

	var dbname, user, pass string

	for k, v := range d.Connection {
		vstr := ""
		if v != nil {
			vstr = fmt.Sprintf("%v", v)
		}

		switch k {
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
			p.Add(k, vstr)
		}
	}

	if dbname == "" {
		return nil, errors.New("missing database")
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

	if len(p) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(p.Encode())
	}

	l := &genericLoader{
		connStr: connstr.String(), driver: "mysql", initialSQL: d.InitialQuery,
		dbConf: d,
	}

	return l, nil
}

func newOracleLoader(d *conf.Database) (Loader, error) {
	p := url.Values{}

	var dbname, user, pass, host, port string

	for k, v := range d.Connection {
		vstr := ""
		if v != nil {
			vstr = fmt.Sprintf("%v", v)
		}

		switch k {
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
			p.Add(k, vstr)
		}
	}

	if dbname == "" {
		return nil, errors.New("missing database")
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

	if len(p) > 0 {
		connstr.WriteRune('?')
		connstr.WriteString(p.Encode())
	}

	l := &genericLoader{
		connStr: connstr.String(), driver: "oracle", initialSQL: d.InitialQuery,
		dbConf: d,
	}

	return l, nil
}

func newMssqlLoader(d *conf.Database) (Loader, error) {
	p := url.Values{}
	databaseConfigured := false

	for k, v := range d.Connection {
		if v != nil {
			vstr := fmt.Sprintf("%v", v)
			p.Add(k, vstr)

			if k == "database" {
				databaseConfigured = true
			}
		}
	}

	if !databaseConfigured {
		return nil, errors.New("missing database")
	}

	connstr := p.Encode()

	l := &genericLoader{
		connStr: connstr, driver: "mssql", initialSQL: d.InitialQuery,
		dbConf: d,
	}

	return l, nil
}

// newLoader returns configured Loader for given configuration.
func newLoader(d *conf.Database) (Loader, error) {
	switch d.Driver {
	case "postgresql", "postgres", "cockroach", "cockroachdb":
		return newPostgresLoader(d)
	case "sqlite3", "sqlite":
		return newSqliteLoader(d)
	case "mysql", "mariadb", "tidb":
		return newMysqlLoader(d)
	case "oracle", "oci8":
		return newOracleLoader(d)
	case "mssql":
		return newMssqlLoader(d)
	}

	return nil, fmt.Errorf("unsupported database type '%s'", d.Driver)
}
