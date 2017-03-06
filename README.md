# DBQuery exporter

The DBQuery exporter allows get metrics from sql databases by user-defined sql-s.

Metrics templates uses golang text/template format.


## Building and running

### Dependency

* github.com/prometheus/client_golang/
* github.com/prometheus/common
* gopkg.in/yaml.v2
* github.com/chop-dbhi/sql-agent


### Local Build

    go build
    ./prom-dbquery_exporter

Configure `dbquery.yml` file, and visit`http://localhost:9122/query?query=<query_name>`
See sql-agent for connection configuration.

#### Prometheus config

    scrape_configs:

        [...]

     - job_name: dbquery_scrape
         static_configs:
         - targets:
             - testq1
         params:
           database: [testdb]
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
             - testdbpgsql
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


## License
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

