package conf

import (
	"strings"
	"time"
)

//
// databases.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

// PoolConfiguration configure database connection pool.
type PoolConfiguration struct {
	MaxConnections     int `yaml:"max_connections"`
	MaxIdleConnections int `yaml:"max_idle_connections"`
	ConnMaxLifeTime    int `yaml:"conn_max_life_time"`
}

func (p *PoolConfiguration) validate() error {
	if p.MaxConnections < 0 {
		return NewInvalidFieldError("max_connections", p.MaxConnections)
	}

	if p.MaxIdleConnections < 0 {
		return NewInvalidFieldError("max_idle_connections", p.MaxIdleConnections)
	}

	if p.ConnMaxLifeTime < 0 {
		return NewInvalidFieldError("conn_max_life_time", p.MaxIdleConnections)
	}

	return nil
}

// Database define database connection.
type Database struct {
	// Driver name - see sql-agent
	Driver string
	// Connection params - see sql-agent
	Connection map[string]interface{}
	// Labels configured per database; may be used in templates
	Labels map[string]interface{}
	// InitialQuery allow run custom query or set some parameters after
	// connect and before target query
	InitialQuery []string `yaml:"initial_query"`

	// Default timeout for all queries
	Timeout uint `yaml:"timeout"`

	// Connection and ping timeout
	ConnectTimeout uint `yaml:"connect_timeout"`

	Pool *PoolConfiguration `yaml:"pool"`

	// Database name for internal use
	Name string `yaml:"-"`
}

func (d *Database) validatePG() error {
	if d.CheckConnectionParam("connstr") == nil {
		return nil
	}

	if err := d.CheckConnectionParam("database"); err != nil {
		if err := d.CheckConnectionParam("dbname"); err != nil {
			return MissingFieldError{"'database' or 'dbname'"}
		}
	}

	if err := d.CheckConnectionParam("user"); err != nil {
		return err
	}

	return nil
}

func (d *Database) validate() error {
	if d.Driver == "" {
		return MissingFieldError{"driver"}
	}

	if d.Pool != nil {
		if err := d.Pool.validate(); err != nil {
			return err
		}
	}

	switch d.Driver {
	case "postgresql", "postgres", "cockroach", "cockroachdb":
		if err := d.validatePG(); err != nil {
			return err
		}
	case "mysql", "mariadb", "tidb", "oracle", "oci8":
		if err := d.CheckConnectionParam("database", "host", "port", "user", "password"); err != nil {
			return err
		}
	case "sqlite3", "sqlite", "mssql":
		if err := d.CheckConnectionParam("database"); err != nil {
			return err
		}
	default:
		return NewInvalidFieldError("database", "d.Driver").WithMsg("unknown database")
	}

	if port, ok := d.Connection["port"]; ok {
		if v, ok := port.(int); !ok || v < 1 || v > 65535 {
			return NewInvalidFieldError("port", port)
		}
	}

	return nil
}

// CheckConnectionParam return true when all keys exists
// and is not empty.
func (d *Database) CheckConnectionParam(keys ...string) error {
	var missing []string
	for _, key := range keys {
		val, ok := d.Connection[key]
		if !ok {
			missing = append(missing, key)
		} else if val, ok := val.(string); ok {
			if strings.TrimSpace(val) == "" {
				missing = append(missing, key)
			}
		}
	}

	if len(missing) > 0 {
		return MissingFieldError{strings.Join(missing, ", ")}
	}

	return nil
}

const defaultConnectioTimeout = 15 * time.Second

// GetConnectTimeout return connection timeout from configuration or default.
func (d *Database) GetConnectTimeout() time.Duration {
	if d.ConnectTimeout > 0 {
		return time.Duration(d.ConnectTimeout) * time.Second
	}

	return defaultConnectioTimeout
}
