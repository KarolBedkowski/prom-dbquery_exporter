package conf

import (
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

//
// databases.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

// PoolConfiguration configure database connection pool.
type PoolConfiguration struct {
	MaxConnections     int           `yaml:"max_connections"`
	MaxIdleConnections int           `yaml:"max_idle_connections"`
	ConnMaxLifeTime    time.Duration `yaml:"conn_max_life_time"`
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

	if p.ConnMaxLifeTime.Seconds() < 1 && p.ConnMaxLifeTime > 0 {
		log.Logger.Warn().Msgf("pool configuration conn_max_life_time < 1s: %v", p.ConnMaxLifeTime)
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
	Timeout time.Duration `yaml:"timeout"`

	// Connection and ping timeout
	ConnectTimeout time.Duration `yaml:"connect_timeout"`

	Pool *PoolConfiguration `yaml:"pool"`

	// Database name for internal use
	Name string `yaml:"-"`
}

// MarshalZerologObject implements LogObjectMarshaler.
func (d Database) MarshalZerologObject(event *zerolog.Event) {
	event.Str("driver", d.Driver).
		Interface("labels", d.Labels).
		Strs("initial_query", d.InitialQuery).
		Dur("timeout", d.Timeout).
		Dur("connect_timeout", d.ConnectTimeout).
		Interface("pool", d.Pool).
		Str("name", d.Name)

	conn := zerolog.Dict()

	for k, v := range d.Connection {
		if k == "password" {
			conn.Str(k, "***")
		} else {
			conn.Interface(k, v)
		}
	}

	event.Dict("connection", conn)
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

func (d *Database) validateCommon() error {
	if port, ok := d.Connection["port"]; ok {
		if v, ok := port.(int); !ok || v < 1 || v > 65535 {
			return NewInvalidFieldError("port", port)
		}
	}

	return nil
}

func (d *Database) validate() error {
	var err error

	switch d.Driver {
	case "":
		err = MissingFieldError{"driver"}
	case "postgresql", "postgres", "cockroach", "cockroachdb":
		err = d.validatePG()
	case "mysql", "mariadb", "tidb", "oracle", "oci8":
		err = d.CheckConnectionParam("database", "host", "port", "user", "password")
	case "sqlite3", "sqlite", "mssql":
		err = d.CheckConnectionParam("database")
	default:
		err = NewInvalidFieldError("database", "d.Driver").WithMsg("unknown database")
	}

	if err == nil && d.Pool != nil {
		err = d.Pool.validate()
	}

	if err == nil {
		err = d.validateCommon()
	}

	if d.Timeout.Seconds() < 1 && d.Timeout > 0 {
		log.Logger.Warn().Msgf("database %v: timeout < 1s: %v", d.Name, d.Timeout)
	}

	if d.ConnectTimeout.Seconds() < 1 && d.ConnectTimeout > 0 {
		log.Logger.Warn().Msgf("database %v: connect_timeout < 1s: %v", d.Name, d.ConnectTimeout)
	}

	return err
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
		return d.ConnectTimeout
	}

	return defaultConnectioTimeout
}
