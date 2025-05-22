package db

//
// validators.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/conf"
)

// checkConnectionParam return true when all keys exists
// and is not empty.
func checkConnectionParam(cfg *conf.Database, keys ...string) error {
	var missing []string

	for _, key := range keys {
		if val, ok := cfg.Connection[key]; !ok {
			missing = append(missing, key)
		} else if val, ok := val.(string); ok {
			if strings.TrimSpace(val) == "" {
				missing = append(missing, key)
			}
		}
	}

	if len(missing) > 0 {
		return conf.MissingFieldError(strings.Join(missing, ", "))
	}

	return nil
}

func validateCommon(dbcfg *conf.Database) error {
	var errs *multierror.Error

	if port, ok := dbcfg.Connection["port"]; ok {
		if v, ok := port.(int); !ok || v < 1 || v > 65535 {
			errs = multierror.Append(errs, conf.NewInvalidFieldError("port", port, "port must be in range 1-65535"))
		}
	}

	if dbcfg.Timeout.Seconds() < 1 && dbcfg.Timeout > 0 {
		log.Logger.Warn().Msgf("configuration: database %v: timeout < 1s: %s", dbcfg.Name, dbcfg.Timeout)
	}

	if dbcfg.ConnectTimeout.Seconds() < 1 && dbcfg.ConnectTimeout > 0 {
		log.Logger.Warn().Msgf("configuration: database %v: connect_timeout < 1s: %s", dbcfg.Name, dbcfg.ConnectTimeout)
	}

	return errs.ErrorOrNil()
}
