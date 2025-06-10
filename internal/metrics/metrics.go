package metrics

//
// metrics.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//
// Global metrics.

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MetricsNamespace is namespace for prometheus metrics.
const MetricsNamespace = "dbquery_exporter"

var (
	// processErrorsCnt is total number of internal errors by category.
	processErrorsCnt = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   MetricsNamespace,
			Subsystem:   "",
			Name:        "process_errors_total",
			Help:        "Number of internal processing errors",
			ConstLabels: nil,
		},
		[]string{"error"},
	)

	uptime = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace:   MetricsNamespace,
			Subsystem:   "",
			Name:        "start_time",
			Help:        "dbquery_exporter start time",
			ConstLabels: nil,
		},
	)
)

func init() {
	prometheus.MustRegister(processErrorsCnt)
	prometheus.MustRegister(uptime)
	uptime.SetToCurrentTime()
}

// -----------------------------------------------------------------

type ErrorCategory string

const (
	ErrCategoryInternalError   ErrorCategory = "internal"
	ErrCategorySourceError     ErrorCategory = "source"
	ErrCategoryAuthError       ErrorCategory = "unauthorized"
	ErrCategoryCanceledError   ErrorCategory = "canceled"
	ErrCategoryBadRequestError ErrorCategory = "bad_request"
	ErrCategoryFormatError     ErrorCategory = "format"
	ErrCategoryClientError     ErrorCategory = "client"
)

// IncProcessErrorsCnt increment process errors count in category.
func IncProcessErrorsCnt(category ErrorCategory) {
	processErrorsCnt.WithLabelValues(string(category)).Inc()
}
