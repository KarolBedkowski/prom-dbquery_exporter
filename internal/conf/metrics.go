package conf

import (
	"github.com/prometheus/client_golang/prometheus"
	"prom-dbquery_exporter.app/internal/metrics"
)

//
// metrics.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

var configReloadTime = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Namespace: metrics.MetricsNamespace,
		Name:      "configuration_load_time",
		Help:      "Current configuration load time",
	},
)

func init() {
	prometheus.MustRegister(configReloadTime)
}
