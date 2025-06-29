# dbquery_exporter v0.11.x

The dbquery_exporter allow to scrap metrics from supported SQL databases by user-defined SQL-s
and templates.

Support: SQLite, PostgrSQL/Cockroach, Oracle, MySQL/MariaDB/TiDB and MSSQL (not tested).


## Building and running

### Dependency

* golang 1.23+
* see: go.mod

#### Database drivers
Each driver must be added during build by tag.

* PostgrSQL
	* lib: github.com/lib/pq
	* configuration driver name: postgres, postgresql, cockroach, cockroachdb
	* build tag: pg
* SQLite
	* lib: github.com/glebarez/go-sqlite
	* configuration driver name: dqlite3, sqlite
	* build tag: sqlite
* Oracle
	* lib: https://github.com/sijms/go-ora
	* configuration driver name: oracle, oci8
	* build tag: oracle
* MSSQL
	* lib: github.com/denisenkom/go-mssqldb - require enable on compile
	* configuration driver name: mssql
	* build tag: mssql
* MySQL/MariaDB/TiB
	* lib: github.com/go-sql-driver/mysql - require enable on compile
	* configuration driver name: mysql, mariadb, tidb
	* build tag: mysql


### Local Build & Run

Build in all drivers:

    make build

Build only selected drivers:

    go build -v -o dbquery_exporter --tags pg,mssql,oracle,mysql,sqlite cli/main.go

(or modify Makefile)

Run:

    ./dbquery_exporter

For help:

    ./dbquery_exporter -help

Edit `dbquery.yaml` file, run and visit `http://localhost:9122/query?query=<query_name>&database=<database_name>`.
See dbquery.yaml for configuration examples.


Other features enabled by compilation tags:

* `tmpl_extra_func` - enable additional template functions `genericBuckets` and `genericBucketsInt`.
* `debug` - enable internal debugging and tracing routines (`/debug/` endpoint).


## Prometheus config

    scrape_configs:

        [...]

     # query one query on many databases
     - job_name: dbquery_scrape
       static_configs:
       - targets:
           - testq1  # query name
       params:
         database: [testdb]  # databases list
         other_query_param: [100]  # additional query params
       metrics_path: /query
       relabel_configs:
       - source_labels: [__address__]
           target_label: __param_query
       - source_labels: [__param_query]
           target_label: query
       - target_label: __address__
           replacement: 127.0.0.1:9122       # dbquery_exporter address

     # query many queries on one database
     - job_name: dbquery_scrape_db
       static_configs:
         - targets:
             - testdbpgsql  # database names
       metrics_path: /query
       params:
         query: [pg_stats_activity_state, pg_stat_database]  # launch many queries
       relabel_configs:
         - source_labels: [__address__]
           target_label: __param_database
         - source_labels: [__param_database]
           target_label: database
         - target_label: __address__
           replacement: 127.0.0.1:9122

     - job_name: dbquery_exporter
         static_configs:
         - targets:
                - 127.0.0.1:9122       # dbquery_exporter address

    # query by group
    - job_name: dbquery_scrape_pg
        static_configs:
        - targets:
            - testdbpgsql
        params:
        group:
            - pgstats
        metrics_path: /query
        relabel_configs:
        - source_labels: [__address__]
            target_label: __param_database
        - source_labels: [__param_database]
            target_label: database
        - target_label: __address__
            replacement: 127.0.0.1:9122       # dbquery_exporter address



## Templates

Templates use standard golang text/template format (see: `https://pkg.go.dev/text/template`).

Template should generate real, correct text-formatted Prometheus metrics. Result is
not validate by dbquery_exporter.


### Template context

Data available in templates:

* `.R` - list of records generated by executing query; each record is map
  column name -> value;
* `.Query` - query name (from request/config)
* `.Database` - database name (from request/config)
* `.P` - additional request parameters
* `.L` - labels defined for database
* `.Count` - total number of query result
* `.QueryDuration` - query duration in sec
* `.QueryStartTime` - start query as UNIX time (time of real execution; caching not
  change it)


### Template functions

* `toLower` - convert value to lower case
* `toUpper` - convert value to upper case
* `trim` - trim non-printable characters
* `quote` - quote " characters
* `replaceSpaces` - replace spaces by "_"
* `removeSpaces` - remove spaces
* `keepAlfaNum` - keep only A-Za-z0-9 characters
* `keepAlfaNumUnderline` - keep only A-Za-z0-9 and "_" characters
* `keepAlfaNumUnderlineSpace` - keep only A-Za-z0-9, "_" and space characters
* `keepAlfaNumUnderlineU` - keep only unicode letter, digits and "_"
* `keepAlfaNumUnderlineSpaceU` - keep only unicode letter, digits, space and "_"
* `clean` - keep only A-Za-z0-9 and "_", replace spaces to "_", trim, convert to lower case


# Requests

## Database scrape

One on more `database` argument is required.
At least on query (defined by `query` or `group` arguments) is required.

Simple query:
`http://localhost:9122/query?query=<query_name>&database=<database_name>`.

By group:
`http://localhost:9122/query?group=<group_name>&database=<database_name>`.

Multiple queries:
`http://localhost:9122/query?query=<query_name>&query=<query_name2>&database=<database_name>`.


## Other endpoints

* `/health` - simple health status
* `/info` - current loaded configuration (available only from localhost)
* '/' - landing page


# Note

Oracle return column in upper case. Probably NLS_LANG environment variable should be also used in most cases (i.e. `NLS_LANG=American_America.UTF8`).

All queries should be reasonable fast - long queries should be avoided. As a rule of thumb - each request should take no more 1 minute. Longer queries can by scheduled, run and it result cached in background in defined intervals.

There is a simple protection preventing to run exactly the same queries (request) parallel.
When next query arrive it may wait up to 5 minutes to previous request finished. After 15 minutes from start
query is considered as death and next request will be processes.

There is also simple caching mechanism - for each query there can be defined timeout in which previous result
of query will be returned. This helpful when metrics are gathered i.e. every minute but there is no need to
get data from database that often.



# License
Copyright (c) 2017-2021, Karol Będkowski.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
