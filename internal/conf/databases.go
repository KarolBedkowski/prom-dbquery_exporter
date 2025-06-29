//
// databases.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

package conf

import (
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	defaultMaxConnections   = 10
	defaultIldeConnection   = 2
	defaultConnMaxLifetime  = time.Duration(600) * time.Second
	defaultConnectioTimeout = time.Duration(15) * time.Second
)

// ----------------------------------------------------

// PoolConfiguration configure database connection pool.
type PoolConfiguration struct {
	MaxConnections     int           `yaml:"max_connections"`
	MaxIdleConnections int           `yaml:"max_idle_connections"`
	ConnMaxLifeTime    time.Duration `yaml:"conn_max_life_time"`
}

func (p *PoolConfiguration) validate() error {
	var err *multierror.Error

	if p.MaxConnections < 0 {
		err = multierror.Append(err,
			NewInvalidFieldError("max_connections", p.MaxConnections, "value must positive number or 0"))
	}

	if p.MaxIdleConnections < 0 {
		err = multierror.Append(err,
			NewInvalidFieldError("max_idle_connections", p.MaxIdleConnections, "value must positive number or 0"))
	}

	if p.ConnMaxLifeTime < 0 {
		err = multierror.Append(err,
			NewInvalidFieldError("conn_max_life_time", p.MaxIdleConnections, "value must positive number or 0"))
	}

	if p.ConnMaxLifeTime.Seconds() < 1 && p.ConnMaxLifeTime > 0 {
		log.Logger.Warn().Msgf("configuration: pool configuration conn_max_life_time < 1s: %v", p.ConnMaxLifeTime)
	}

	return err.ErrorOrNil()
}

// ----------------------------------------------------

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

	// Has database valid configuration?
	Valid bool
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
			if vs, ok := v.(string); ok && len(vs) > 0 {
				conn.Str(k, "** set **")
			} else {
				conn.Str(k, "** NOT SET **")
			}
		} else {
			conn.Interface(k, v)
		}
	}

	event.Dict("connection", conn)
}

func (d *Database) setup(name string) {
	d.Name = name

	// create default pool configuration when not defined in conf file
	if d.Pool == nil {
		d.Pool = &PoolConfiguration{
			MaxConnections:     defaultMaxConnections,
			MaxIdleConnections: defaultIldeConnection,
			ConnMaxLifeTime:    defaultConnMaxLifetime,
		}
	}

	if d.Pool.MaxIdleConnections > 0 {
		d.MaxWorkers = d.Pool.MaxConnections
	} else {
		d.MaxWorkers = 1
	}

	if d.BackgroundWorkers >= d.MaxWorkers {
		log.Logger.Warn().
			Msgf("configuration: number of background workers must be lower than max_connections (%d);"+
				" disabling background jobs",
				d.MaxWorkers)

		d.BackgroundWorkers = 0
	} else {
		d.MaxWorkers -= d.BackgroundWorkers
	}

	if d.ConnectTimeout <= 0 {
		d.ConnectTimeout = defaultConnectioTimeout
	}
}

func (d *Database) validate(dbp DatabaseProvider) error {
	if err := dbp.Validate(d); err != nil {
		return fmt.Errorf("database configuration error: %w", err)
	}

	if d.Pool != nil {
		if err := d.Pool.validate(); err != nil {
			return fmt.Errorf("database pool configuration error: %w", err)
		}
	}

	d.Valid = true

	return nil
}
