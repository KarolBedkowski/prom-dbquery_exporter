package conf

//
// metrics.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"github.com/prometheus/client_golang/prometheus"
	"prom-dbquery_exporter.app/internal/metrics"
)

var configReloadTime = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Namespace:   metrics.MetricsNamespace,
		Subsystem:   "configuration",
		Name:        "load_time",
		Help:        "Current configuration load time",
		ConstLabels: nil,
	},
)

func init() {
	prometheus.MustRegister(configReloadTime)
}
