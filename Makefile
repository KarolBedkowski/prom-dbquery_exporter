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

.PHONY: build

build:
	go build -v -o dbquery_exporter --tags debug cli/main.go

build_release:
	CGO_ENABLED=0 \
		go build -v -o dbquery_exporter  --ldflags $(LDFLAGS) cli/main.go


.PHONY: build_arm64

build_arm64_debug:
	CGO_ENABLED=0 GOARCH=arm64 GOOS=linux \
		go build -v -o dbquery_exporter_arm64  \
			--ldflags $(LDFLAGS) \
			--tags debug \
			cli/main.go

build_arm64:
	CGO_ENABLED=0 \
		GOARCH=arm64 \
		GOOS=linux \
		go build -v -o dbquery_exporter_arm64  --ldflags $(LDFLAGS) cli/main.go

.PHONY: run

run:
	go run --tags debug -v cli/main.go -log.level debug


.PHONY: check
lint:
	golangci-lint run
	#--fix
	# go install go.uber.org/nilaway/cmd/nilaway@latest
	nilaway ./...


.PHONY: format
format:
	find internal cli -type d -exec wsl -fix ./{} ';'
	find . -name '*.go' -type f -exec gofumpt -w {} ';'

# vim:ft=make
