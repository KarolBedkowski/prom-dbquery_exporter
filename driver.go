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
	Record map[string]interface{}

	Loader interface {
		Query(q *Query) ([]Record, error)
	}
)

type postgresLoader struct {
	connStr string
}

func genericQuery(driver, connstr string, q *Query) ([]Record, error) {
	log.Debugf("genericQuery '%s' '%s'", driver, connstr)
	con, err := sqlx.Connect(driver, connstr)
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

func NewPostgresLoader(d *Database) (*postgresLoader, error) {
	p := make([]string, 0, len(d.Connection))
	for k, v := range d.Connection {
		vstr := fmt.Sprintf("%v", v)
		if vstr != "" {
			p = append(p, k+"="+vstr)
		}
	}
	return &postgresLoader{
		connStr: strings.Join(p, " "),
	}, nil
}

func (p *postgresLoader) Query(q *Query) ([]Record, error) {
	return genericQuery("postgres", p.connStr, q)
}

type sqliteLoader struct {
	connStr string
}

func NewSqliteLoader(d *Database) (*sqliteLoader, error) {
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
	l := &sqliteLoader{dbname}
	if len(p) > 0 {
		l.connStr += "?" + strings.Join(p, "&")
	}
	return l, nil
}

func (p *sqliteLoader) Query(q *Query) ([]Record, error) {
	return genericQuery("sqlite3", p.connStr, q)
}

func GetLoader(d *Database) (Loader, error) {
	switch d.Driver {
	case "postgresql":
	case "postgres":
		return NewPostgresLoader(d)
	case "sqlite3":
	case "sqlite":
		return NewSqliteLoader(d)
	}
	return nil, fmt.Errorf("unsupported database type '%s'", d.Driver)
}
