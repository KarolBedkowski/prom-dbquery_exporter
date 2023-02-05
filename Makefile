#
# Makefile
#
VERSION=dev
REVISION=`git describe --always`
DATE=`date +%Y%m%d%H%M%S`
USER=`whoami`
BRANCH=`git branch | grep '^\*' | cut -d ' ' -f 2`
LDFLAGS=" -w -s \
	-X github.com/prometheus/common/version.Version=$(VERSION) \
	-X github.com/prometheus/common/version.Revision=$(REVISION) \
	-X github.com/prometheus/common/version.BuildDate=$(DATE) \
	-X github.com/prometheus/common/version.BuildUser=$(USER) \
	-X github.com/prometheus/common/version.Branch=$(BRANCH)"

build:
	go build -v -o dbquery_exporter  --ldflags $(LDFLAGS)

build_arm64:
	GOARCH=arm64 \
		GOOS=linux \
		go build -v -o dbquery_exporter_arm64  --ldflags $(LDFLAGS)

run:
	go run -v main.go -log.level debug

# vim:ft=make
