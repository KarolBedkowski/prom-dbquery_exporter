package conf

import (
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

//
// scheduler.go
// Copyright (C) 2024 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

// Job is configuration for one scheduler job.
type Job struct {
	Query    string
	Database string
	Interval time.Duration

	Idx int `yaml:"-"`
}

func (j *Job) validate(cfg *Configuration) error {
	var errs *multierror.Error

	if j.Database == "" {
		errs = multierror.Append(errs, MissingFieldError{"database"})
	} else if _, ok := cfg.Database[j.Database]; !ok {
		errs = multierror.Append(errs, NewInvalidFieldError("database", j.Database).
			WithMsg("unknown database"))
	}

	if j.Query == "" {
		errs = multierror.Append(errs, MissingFieldError{"query"})
	} else if _, ok := cfg.Query[j.Query]; !ok {
		errs = multierror.Append(errs, NewInvalidFieldError("query", j.Database).
			WithMsg("unknown query"))
	}

	if j.Interval.Seconds() < 1 {
		log.Logger.Warn().Msgf("configuration: job %d (%v, %v): interval < 1s: %v", j.Idx, j.Database, j.Query, j.Interval)
	}

	return errs.ErrorOrNil()
}

// MarshalZerologObject implements LogObjectMarshaler.
func (j *Job) MarshalZerologObject(event *zerolog.Event) {
	event.Int("idx", j.Idx).
		Str("query", j.Query).
		Str("database", j.Database).
		Dur("interval", j.Interval)
}
