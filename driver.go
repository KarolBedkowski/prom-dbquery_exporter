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
		Query(q *Query) ([]Record, error)
	}

	genericLoader struct {
		connStr string
		driver  string
	}
)

func (g *genericLoader) Query(q *Query) ([]Record, error) {
	log.Debugf("genericQuery '%s' '%s'", g.driver, g.connStr)
	con, err := sqlx.Connect(g.driver, g.connStr)
	if err != nil {
		return nil, err
	}

	defer con.Close()

	var rows *sqlx.Rows

	if q.Params != nil && len(q.Params) > 0 {
		rows, err = con.NamedQuery(q.SQL, q.Params)
	} else {
		rows, err = con.Queryx(q.SQL)
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

func newPostgresLoader(d *Database) (Loader, error) {
	p := make([]string, 0, len(d.Connection))
	for k, v := range d.Connection {
		vstr := fmt.Sprintf("%v", v)
		if vstr != "" {
			p = append(p, k+"="+vstr)
		}
	}
	return &genericLoader{
		connStr: strings.Join(p, " "),
		driver:  "postgres",
	}, nil
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
	l := &genericLoader{dbname, "sqlite3"}
	if len(p) > 0 {
		l.connStr += "?" + strings.Join(p, "&")
	}
	return l, nil
}

}

func GetLoader(d *Database) (Loader, error) {
	switch d.Driver {
	case "postgresql":
	case "postgres":
		return newPostgresLoader(d)
	case "sqlite3":
	case "sqlite":
		return newSqliteLoader(d)
	}
	return nil, fmt.Errorf("unsupported database type '%s'", d.Driver)
}
