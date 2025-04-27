//go:build !oracle
// +build !oracle

// impl_pg.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

import (
	"prom-dbquery_exporter.app/internal/conf"
)

const OracleSupported = false

func newOracleLoader(_ *conf.Database) (*genericDatabase, error) {
	return nil, NotSupportedError("oracle")
}
