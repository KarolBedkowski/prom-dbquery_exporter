#
# Makefile
# , 2017-03-09 07:33
#

build:
	go build -v -o prom-dbquery_exporter 

run:
	go run -v *.go -log.level debug

# vim:ft=make
