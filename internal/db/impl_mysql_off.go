//go:build !mysql
// +build !mysql

// impl_pg.go
// Copyright (C) 2025 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
package db

import (
	"prom-dbquery_exporter.app/internal/conf"
)

const MysqlSupported = false

func newMysqlLoader(_ *conf.Database) (*genericDatabase, error) {
	return nil, NotSupportedError("mysql")
}
