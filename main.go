package main

//
// main.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	//	_ "net/http/pprof"

	// _ "github.com/denisenkom/go-mssqldb"
	// _ "github.com/go-sql-driver/mysql"
	_ "github.com/glebarez/go-sqlite"
	_ "github.com/lib/pq"
	_ "github.com/sijms/go-ora/v2"

	"prom-dbquery_exporter.app/cli"
)

func main() {
	cli.Main()
}
