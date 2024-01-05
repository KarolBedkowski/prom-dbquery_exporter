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
