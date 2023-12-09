package collectors

//
// task.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
	"prom-dbquery_exporter.app/internal/conf"
)

// Task is query to perform.
type Task struct {
	// Ctx is context used for cancellation.
	Ctx context.Context //nolint:containedctx

	DBName    string
	QueryName string

	Query  *conf.Query
	Params map[string]any

	Output chan *TaskResult
}

func (d *Task) newResult(err error, result []byte) *TaskResult {
	return &TaskResult{
		Error:  err,
		Result: result,
		Task:   d,
	}
}

// MarshalZerologObject implements LogObjectMarshaler.
func (d Task) MarshalZerologObject(e *zerolog.Event) {
	e.Str("db", d.DBName).
		Str("query", d.QueryName).
		Interface("params", d.Params)

	if d.Ctx != nil {
		if rid, ok := hlog.IDFromCtx(d.Ctx); ok {
			e.Str("req_id", rid.String())
		}
	}
}

// TaskResult is query result.
type TaskResult struct {
	Error  error
	Result []byte

	Task *Task
}

// NewSimpleTaskResult create new TaskResult with basic data.
func NewSimpleTaskResult(res []byte, dbName, queryName string) *TaskResult {
	return &TaskResult{
		Result: res,
		Task: &Task{
			DBName:    dbName,
			QueryName: queryName,
		},
	}
}

// MarshalZerologObject implements LogObjectMarshaler.
func (t TaskResult) MarshalZerologObject(e *zerolog.Event) {
	e.Object("task", t.Task).
		Err(t.Error).
		Int("result_size", len(t.Result))
}
