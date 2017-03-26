#
# Makefile
#
VERSION=dev
REVISION=`git describe --always`
DATE=`date +%Y%m%d%H%M%S`
USER=`whoami`
BRANCH=`git branch | grep '^\*' | cut -d ' ' -f 2`
LDFLAGS="\
	-X github.com/prometheus/common/version.Version='$(VERSION)' \
	-X github.com/prometheus/common/version.Revision='$(REVISION)' \
	-X github.com/prometheus/common/version.BuildDate='$(DATE)' \
	-X github.com/prometheus/common/version.BuildUser='$(USER)' \
	-X github.com/prometheus/common/version.Branch='$(BRANCH)'"

build:
	go build -v -o prom-dbquery_exporter  --ldflags $(LDFLAGS)

run:
	go run -v *.go -log.level debug

# vim:ft=make
