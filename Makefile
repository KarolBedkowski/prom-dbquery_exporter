#
# Makefile
#
CURRENT_DIR = $(shell pwd)
VERSION = $(shell git describe --always)
REVISION = $(shell git rev-parse HEAD)
DATE = $(shell date +%Y%m%d%H%M%S)
USER = $(shell whoami)
# BRANCH=`git branch | grep '^\*' | cut -d ' ' -f 2`
BRANCH = $(shell git rev-parse --abbrev-ref HEAD)
LDFLAGS = " -w -s \
  -X github.com/prometheus/common/version.Version=$(VERSION) \
  -X github.com/prometheus/common/version.Revision=$(REVISION) \
  -X github.com/prometheus/common/version.BuildDate=$(DATE) \
  -X github.com/prometheus/common/version.BuildUser=$(USER) \
  -X github.com/prometheus/common/version.Branch=$(BRANCH)"

.PHONY: build
build: build_amd64_debug

.PHONY: build_release
build_release: build_amd64

.PHONY: build_amd64_debug
build_amd64_debug:
	go build -v -o dbquery_exporter \
		--tags pg,mssql,oracle,mysql,sqlite,tmpl_extra_func,debug \
		-gcflags=-trimpath=$(CURRENT_DIR) \
		-asmflags=-trimpath=$(CURRENT_DIR) \
		cli/main.go

.PHONY: build_amd64
build_amd64:
	CGO_ENABLED=0 \
		go build -v -o dbquery_exporter  \
			--tags pg,mssql,oracle,mysql,sqlite,tmpl_extra_func \
			--ldflags $(LDFLAGS) \
			-gcflags=-trimpath=$(CURRENT_DIR) \
			-asmflags=-trimpath=$(CURRENT_DIR) \
			cli/main.go

.PHONY: build_arm64_debug
build_arm64_debug:
	CGO_ENABLED=0 GOARCH=arm64 GOOS=linux \
		go build -v -o dbquery_exporter_arm64  \
			--ldflags $(LDFLAGS) \
			--tags debug,pg,tmpl_extra_func \
			-gcflags=-trimpath=$(CURRENT_DIR) \
			-asmflags=-trimpath=$(CURRENT_DIR) \
			cli/main.go

.PHONY: build_arm64
build_arm64:
	CGO_ENABLED=0 \
		GOARCH=arm64 \
		GOOS=linux \
		go build -v -o dbquery_exporter_arm64  \
			--tags pg,tmpl_extra_func \
			--ldflags $(LDFLAGS) \
			-gcflags=-trimpath=$(CURRENT_DIR) \
			-asmflags=-trimpath=$(CURRENT_DIR) \
			cli/main.go

.PHONY: run
run:
	go run --tags debug,pg,sqlite,tmpl_extra_func \
		-v cli/main.go -log.level debug \
		-parallel-scheduler  \
		-validate-output

.PHONY: check
check: lint

.PHONY: lint
lint:
	golangci-lint run || true
	#--fix
	# go install go.uber.org/nilaway/cmd/nilaway@latest
	nilaway ./... || true
	typos

.PHONY: format
format:
	# find internal cli -type d -exec wsl -fix prom-dbquery_exporter.app/{} ';'
	# find . -name '*.go' -type f -exec gofumpt -w {} ';'
	golangci-lint fmt

.PHONY: test
test:
	go test  --tags debug,pg,sqlite,mssql,mysql,oracle,tmpl_extra_func ./...

# vim:ft=make
