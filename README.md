# DBQuery exporter

The DBQuery exporter allows get metrics from sql databases by user-defined sql-s.

Metrics templates uses golang text/template format.

Support: SQLite, PostgrSQL, MySQL/MariaDB/TiDB, Oracle, MSSQL (not tested)


## Building and running

### Dependency

* golang 1.17
* see: go.mod

#### Database drivers
* MySQL/MariaDB/TiB: github.com/go-sql-driver/mysql
* PostgrSQL: github.com/lib/pq
* SQLite: github.com/mattn/go-sqlite3
* Oracle: github.com/mattn/go-oci8
* MSSQL: github.com/denisenkom/go-mssqldb


### Local Build & Run

    go build
    ./prom-dbquery_exporter

    ./prom-dbquery_exporter -help

Configure `dbquery.yml` file, and visit `http://localhost:9122/query?query=<query_name>&database=<database_name>`
See dbquery.yml for configuration examples.

### Prometheus config

    scrape_configs:

        [...]

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

     - job_name: dbquery
         static_configs:
         - targets:
                - 127.0.0.1:9122       # dbquery_exporter address

### Note

Oracle return columnt in upper case. Propably NLS_LANG environment variable should be used
(i.e. `NLS_LANG=American_America.UTF8`).

### Template functions

* toLower - convert value to lower case
* toUpper - convert value to upper case
* trim - trim non-printable characters
* quote - quote " characters
* replaceSpaces - replace spaces by "_"
* removeSpaces - remove spaces
* keepAlfaNum - keep only A-Za-z0-9 characters
* keepAlfaNumUnderline - keep only A-Za-z0-9 and "_" characters
* keepAlfaNumUnderlineSpace - keep only A-Za-z0-9, "_" and space characters
* keepAlfaNumUnderlineU - keep only unicode letter, digits and "_"
* keepAlfaNumUnderlineSpaceU - keep only unicode letter, digits, space and "_"
* clean - keep only A-Za-z0-9 and "_", replace spaces to "_", trim, convert to lower case


# License
Copyright (c) 2017, Karol Będkowski.

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
