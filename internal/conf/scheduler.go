package conf

import "time"

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

	return nil
}
