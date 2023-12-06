package db

// Formatter contain methods used to format query result using defined templates.
// formatters.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.

import (
	"time"
)

// QueryResult is result of Loader.Query.
type QueryResult struct {
	// rows
	Records []Record
	// query duration
	Duration float64
	// query start time
	Start time.Time
	// all query parameters
	Params map[string]interface{}
}
