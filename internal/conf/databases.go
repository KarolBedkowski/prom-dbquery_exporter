//
// databases.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

package conf

import (
	"strings"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	defaultMaxWorkers       = 10
	defaultConnectioTimeout = 15 * time.Second
)

// PoolConfiguration configure database connection pool.
type PoolConfiguration struct {
	MaxConnections     int           `yaml:"max_connections"`
	MaxIdleConnections int           `yaml:"max_idle_connections"`
	ConnMaxLifeTime    time.Duration `yaml:"conn_max_life_time"`
}

func (p *PoolConfiguration) validate() error {
	var err *multierror.Error

	if p.MaxConnections < 0 {
		err = multierror.Append(err, NewInvalidFieldError("max_connections", p.MaxConnections))
	}

	if p.MaxIdleConnections < 0 {
		err = multierror.Append(err, NewInvalidFieldError("max_idle_connections", p.MaxIdleConnections))
	}

	if p.ConnMaxLifeTime < 0 {
		err = multierror.Append(err, NewInvalidFieldError("conn_max_life_time", p.MaxIdleConnections))
	}

	if p.ConnMaxLifeTime.Seconds() < 1 && p.ConnMaxLifeTime > 0 {
		log.Logger.Warn().Msgf("configuration: pool configuration conn_max_life_time < 1s: %v", p.ConnMaxLifeTime)
	}

	return err.ErrorOrNil()
}

// Database define database connection.
type Database struct {
	// Connection params - see sql-agent
	Connection map[string]any
	// Labels configured per database; may be used in templates
	Labels map[string]any

	Pool *PoolConfiguration `yaml:"pool"`

	// Driver name - see sql-agent
	Driver string
	// Database name for internal use
	Name string `yaml:"-"`
	// InitialQuery allow run custom query or set some parameters after
	// connect and before target query
	InitialQuery []string `yaml:"initial_query"`
	// Default timeout for all queries
	Timeout time.Duration `yaml:"timeout"`
	// Connection and ping timeout
	ConnectTimeout time.Duration `yaml:"connect_timeout"`

	// Number of connection dedicated to run only jobs by scheduler
	BackgroundWorkers int `yaml:"background_workers"`
	// Max workers is defined by pool max_connections-background_workers
	MaxWorkers int `yaml:"-"`
}

// MarshalZerologObject implements LogObjectMarshaler.
func (d *Database) MarshalZerologObject(event *zerolog.Event) {
	event.Str("driver", d.Driver).
		Interface("labels", d.Labels).
		Strs("initial_query", d.InitialQuery).
		Dur("timeout", d.Timeout).
		Dur("connect_timeout", d.ConnectTimeout).
		Interface("pool", d.Pool).
		Int("background_workers", d.BackgroundWorkers).
		Int("max_workers", d.MaxWorkers).
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

// GetConnectTimeout return connection timeout from configuration or default.
func (d *Database) GetConnectTimeout() time.Duration {
	if d.ConnectTimeout > 0 {
		return d.ConnectTimeout
	}

	return defaultConnectioTimeout
}

func (d *Database) validatePG() error {
	if d.CheckConnectionParam("connstr") == nil {
		return nil
	}

	var errs *multierror.Error

	if err := d.CheckConnectionParam("database"); err != nil {
		if err := d.CheckConnectionParam("dbname"); err != nil {
			errs = multierror.Append(errs, MissingFieldError{"'database' or 'dbname'"})
		}
	}

	if err := d.CheckConnectionParam("user"); err != nil {
		errs = multierror.Append(errs, err)
	}

	return errs.ErrorOrNil()
}

func (d *Database) validateCommon() error {
	var errs *multierror.Error

	if port, ok := d.Connection["port"]; ok {
		if v, ok := port.(int); !ok || v < 1 || v > 65535 {
			errs = multierror.Append(errs, NewInvalidFieldError("port", port))
		}
	}

	if d.Timeout.Seconds() < 1 && d.Timeout > 0 {
		log.Logger.Warn().Msgf("configuration: database %v: timeout < 1s: %s", d.Name, d.Timeout)
	}

	if d.ConnectTimeout.Seconds() < 1 && d.ConnectTimeout > 0 {
		log.Logger.Warn().Msgf("configuration: database %v: connect_timeout < 1s: %s", d.Name, d.ConnectTimeout)
	}

	d.MaxWorkers = defaultMaxWorkers

	if d.Pool != nil {
		if d.Pool.MaxIdleConnections > 0 {
			d.MaxWorkers = d.Pool.MaxConnections
		}

		if d.BackgroundWorkers >= d.MaxWorkers {
			log.Logger.Warn().
				Msg("configuration: number of background workers must be lower than max_connections; disabling background jobs")

			d.BackgroundWorkers = 0
		} else {
			d.MaxWorkers -= d.BackgroundWorkers
		}
	}

	return errs.ErrorOrNil()
}

func (d *Database) validate() error {
	var err *multierror.Error

	switch d.Driver {
	case "":
		return MissingFieldError{"driver"}
	case "postgresql", "postgres", "cockroach", "cockroachdb":
		err = multierror.Append(err, d.validatePG())
	case "mysql", "mariadb", "tidb", "oracle", "oci8":
		err = multierror.Append(err, d.CheckConnectionParam("database", "host", "port", "user", "password"))
	case "sqlite3", "sqlite", "mssql":
		err = multierror.Append(err, d.CheckConnectionParam("database"))
	default:
		return NewInvalidFieldError("database", d.Driver).WithMsg("unknown database")
	}

	if d.Pool != nil {
		err = multierror.Append(err, d.Pool.validate())
	}

	err = multierror.Append(err, d.validateCommon())

	return err.ErrorOrNil()
}
