package collectors

//
// task.go
// Copyright (C) 2023 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"time"

	"github.com/rs/xid"
	"github.com/rs/zerolog"
	"prom-dbquery_exporter.app/internal/conf"
)

// Task is query to perform.
type Task struct {
	RequestStart time.Time
	Query        *conf.Query
	Params       map[string]any
	Output       chan *TaskResult
	DBName       string

	IsScheduledJob bool
	ReqID          string

	CancelCh chan struct{}
}

func NewTask(dbname string, query *conf.Query, output chan *TaskResult) *Task {
	return &Task{
		RequestStart: time.Now(),
		Query:        query,
		Params:       nil,
		Output:       output,
		DBName:       dbname,

		IsScheduledJob: false,
		ReqID:          "",

		CancelCh: nil,
	}
}

func (d *Task) WithReqID(reqID ...string) *Task {
	if len(reqID) > 1 {
		panic("to many parameters")
	}

	if len(reqID) > 0 && reqID[0] != "" {
		d.ReqID = reqID[0]
	} else {
		d.ReqID = xid.New().String()
	}

	return d
}

func (d *Task) WithParams(params map[string]any) *Task {
	d.Params = params

	return d
}

func (d *Task) MarkScheduled() *Task {
	d.IsScheduledJob = true

	return d
}

// Cancelled check is task cancelled.
func (d *Task) Cancelled() <-chan struct{} {
	return d.CancelCh
}

func (d *Task) UseCancel(ch chan struct{}) *Task {
	d.CancelCh = ch

	return d
}

// WithCancel create CancelCh and return cancel function.
func (d *Task) WithCancel() (*Task, func()) {
	if d.CancelCh == nil {
		d.CancelCh = make(chan struct{})
	}

	return d, d.cancel
}

// MarshalZerologObject implements LogObjectMarshaler.
func (d *Task) MarshalZerologObject(e *zerolog.Event) {
	e.Str("db", d.DBName).
		Str("query", d.Query.Name).
		Bool("is_job", d.IsScheduledJob).
		Interface("params", d.Params).
		Str("req_id", d.ReqID)
}

func (d *Task) newResult(err error, result []byte) *TaskResult {
	return &TaskResult{
		Error:  err,
		Result: result,
		Task:   d,
	}
}

func (d *Task) cancel() {
	if d.CancelCh != nil {
		close(d.CancelCh)
	}
}

// TaskResult is query result.
type TaskResult struct {
	Error  error
	Task   *Task
	Result []byte
}

// MarshalZerologObject implements LogObjectMarshaler.
func (t *TaskResult) MarshalZerologObject(e *zerolog.Event) {
	e.Object("task", t.Task).
		Err(t.Error).
		Int("result_size", len(t.Result))
}
