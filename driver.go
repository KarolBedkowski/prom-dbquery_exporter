//
// driver.go
// Based on github.com/chop-dbhi/sql-agent

package main

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/prometheus/common/log"
)

type (
	// Record is one record (row) loaded from database
	Record map[string]interface{}

	// Loader load data from database
	Loader interface {
		// Query execute sql and returns records or error. Open connection when necessary.
		Query(q *Query) ([]Record, error)
		// Close db connection.
		Close()
	}

	genericLoader struct {
		connStr string
		driver  string
		conn    *sqlx.DB
	}
)

func (g *genericLoader) Query(q *Query) ([]Record, error) {
	var err error
	if g.conn == nil {
		log.With("driver", g.driver).Debugf("genericQuery connect to '%s'", g.connStr)
		g.conn, err = sqlx.Connect(g.driver, g.connStr)
		if err != nil {
			return nil, err
		}
	}

	log.With("driver", g.driver).Debugf("genericQuery execute '%v', '%v'", q.SQL, q.Params)

	var rows *sqlx.Rows

	if q.Params != nil && len(q.Params) > 0 {
		rows, err = g.conn.NamedQuery(q.SQL, q.Params)
	} else {
		rows, err = g.conn.Queryx(q.SQL)
	}

	var records []Record

	for rows.Next() {
		rec := Record{}
		if err := rows.MapScan(rec); err != nil {
			return nil, err
		}
		for k, v := range rec {
			switch v.(type) {
			case []byte:
				rec[k] = string(v.([]byte))
			}
		}
		records = append(records, rec)
	}

	return records, nil
}

func (g *genericLoader) Close() {
	if g.conn != nil {
		log.With("driver", g.driver).Debugf("genericQuery disconnect from %s'", g.connStr)
		g.conn.Close()
		g.conn = nil
	}

}

func (g *genericLoader) String() string {
	return fmt.Sprintf("genericLoader driver='%s' connstr='%s' connected=%v",
		g.driver, g.connStr, g.conn != nil)
}

func newPostgresLoader(d *Database) (Loader, error) {
	p := make([]string, 0, len(d.Connection))
	for k, v := range d.Connection {
		vstr := fmt.Sprintf("%v", v)
		if vstr != "" {
			p = append(p, k+"="+vstr)
		}
	}
	l := &genericLoader{
		connStr: strings.Join(p, " "),
		driver:  "postgres",
	}
	log.Debugf("created loader: %s", l.String())
	return l, nil
}

func newSqliteLoader(d *Database) (Loader, error) {
	p := make([]string, 0, len(d.Connection))
	var dbname string
	for k, v := range d.Connection {
		vstr := fmt.Sprintf("%s", v)
		if vstr == "" {
			continue
		} else if k == "database" {
			dbname = vstr
		} else {
			p = append(p, fmt.Sprintf("%s=%v", k, vstr))
		}
	}
	if dbname == "" {
		return nil, fmt.Errorf("missing database")
	}
	l := &genericLoader{connStr: dbname, driver: "sqlite3"}
	if len(p) > 0 {
		l.connStr += "?" + strings.Join(p, "&")
	}
	log.Debugf("created loader: %s", l.String())
	return l, nil
}

func newOracleLoader(d *Database) (Loader, error) {
	p := make([]string, 0, len(d.Connection))
	var dbname, user, pass, host, port string
	for k, v := range d.Connection {
		vstr := fmt.Sprintf("%s", v)
		if vstr == "" {
			continue
		}
		switch k {
		case "database":
			dbname = vstr
		case "host":
			host = vstr
		case "port":
			port = vstr
		case "user":
			user = vstr
		case "password":
			pass = vstr
		default:
			p = append(p, k+"="+vstr)
		}
	}
	if dbname == "" {
		return nil, fmt.Errorf("missing database")
	}
	var connstr string
	if user != "" {
		if pass != "" {
			connstr = user + "/" + pass + "@"
		} else {
			connstr = user + "@"
		}
	}
	connstr += host
	if port != "" {
		connstr += ":" + port
	}
	connstr += "/" + dbname
	if len(p) > 0 {
		connstr += "?" + strings.Join(p, "&")
	}
	l := &genericLoader{connStr: connstr, driver: "ocl8"}
	log.Debugf("created loader: %s", l.String())
	return l, nil
}

func GetLoader(d *Database) (Loader, error) {
	switch d.Driver {
	case "postgresql":
	case "postgres":
		return newPostgresLoader(d)
	case "sqlite3":
	case "sqlite":
		return newSqliteLoader(d)
	case "oracle":
	case "oci8":
		return newOracleLoader(d)
	}
	return nil, fmt.Errorf("unsupported database type '%s'", d.Driver)
}
