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
	// _ "github.com/mattn/go-oci8"
	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"

	"prom-dbquery_exporter.app/cli"
)

func main() {
	cli.Main()
}
