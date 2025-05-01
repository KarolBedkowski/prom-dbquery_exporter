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
	go build -v -o dbquery_exporter \
		--tags pg,mssql,oracle,mysql,sqlite,tmpl_extra_func \
		cli/main.go

build_release:
	CGO_ENABLED=0 \
		go build -v -o dbquery_exporter  \
			--tags pg,mssql,oracle,mysql,sqlite,tmpl_extra_func \
			--ldflags $(LDFLAGS) \
			cli/main.go


.PHONY: build_arm64

build_arm64_debug:
	CGO_ENABLED=0 GOARCH=arm64 GOOS=linux \
		go build -v -o dbquery_exporter_arm64  \
			--ldflags $(LDFLAGS) \
			--tags debug,pg,tmpl_extra_func \
			cli/main.go

build_arm64:
	CGO_ENABLED=0 \
		GOARCH=arm64 \
		GOOS=linux \
		go build -v -o dbquery_exporter_arm64  \
			--tags pg,tmpl_extra_func \
			--ldflags $(LDFLAGS) \
			cli/main.go

.PHONY: run

run:
	go run --tags debug,pg,sqlite,tmpl_extra_func \
		-v cli/main.go -log.level debug \
		-parallel-scheduler  \
		-validate-output

.PHONY: check
lint:
	golangci-lint run || true
	#--fix
	# go install go.uber.org/nilaway/cmd/nilaway@latest
	nilaway ./... || true
	typos


.PHONY: format
format:
	# find internal cli -type d -exec wsl -fix prom-dbquery_exporter.app/{} ';'
	find . -name '*.go' -type f -exec gofumpt -w {} ';'

.PHONY: test
test:
	go test  --tags debug,pg,sqlite,mssql,mysql,oracle,tmpl_extra_func ./...

# vim:ft=make
