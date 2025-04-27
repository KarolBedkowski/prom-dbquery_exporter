//go:build !pg
// +build !pg

// impl_pg.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

import (
	"prom-dbquery_exporter.app/internal/conf"
)

const PostgresqlSupported = false

func newPostgresLoader(_ *conf.Database) (*genericDatabase, error) {
	return nil, NotSupportedError("posgresql")
}
