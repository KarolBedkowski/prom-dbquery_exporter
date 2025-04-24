package conf

import (
	"time"

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
	if len(j.Query) == 0 || len(j.Database) == 0 {
		return MissingFieldError{"'database' or 'query'"}
	}

	if _, ok := cfg.Database[j.Database]; !ok {
		return NewInvalidFieldError("database", j.Database).
			WithMsg("unknown database")
	}

	if _, ok := cfg.Query[j.Query]; !ok {
		return NewInvalidFieldError("query", j.Database).
			WithMsg("unknown query")
	}

	if j.Interval.Seconds() < 1 {
		log.Logger.Warn().Msgf("job %d (%v, %v): interval < 1s: %v", j.Idx, j.Database, j.Query, j.Interval)
	}

	return nil
}

// MarshalZerologObject implements LogObjectMarshaler.
func (j *Job) MarshalZerologObject(event *zerolog.Event) {
	event.Int("idx", j.Idx).
		Str("query", j.Query).
		Str("database", j.Database).
		Dur("interval", j.Interval)
}
