BINARY := dbf
PACKAGE := ./cmd/dbf
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DATADIR ?= $(PREFIX)/share/debianform
GOVULNCHECK_VERSION ?= v1.4.0
DEBIAN_VERSION ?= 13
TARGET ?=
INTEGRATION_TARGET = $(if $(strip $(TARGET)),$(TARGET),debian-$(DEBIAN_VERSION))
DESTDIR ?=
INSTALL ?= install
VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

VERSION_PACKAGE := github.com/mofelee/debianform/internal/version
LDFLAGS := -s -w \
	-X $(VERSION_PACKAGE).Version=$(VERSION) \
	-X $(VERSION_PACKAGE).Commit=$(COMMIT) \
	-X $(VERSION_PACKAGE).Date=$(BUILD_DATE)

.PHONY: build install test test-unit docs-check vulncheck update-golden test-integration test-integration-case test-integration-layout test-integration-source-build clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PACKAGE)

install: build
	$(INSTALL) -d "$(DESTDIR)$(BINDIR)" "$(DESTDIR)$(DATADIR)/docs" "$(DESTDIR)$(DATADIR)/examples"
	$(INSTALL) -m 0755 "$(BINARY)" "$(DESTDIR)$(BINDIR)/dbf"
	$(INSTALL) -m 0644 README.md "$(DESTDIR)$(DATADIR)/README.md"
	$(INSTALL) -m 0644 README.zh-CN.md "$(DESTDIR)$(DATADIR)/README.zh-CN.md"
	$(INSTALL) -m 0644 CHANGELOG.md "$(DESTDIR)$(DATADIR)/CHANGELOG.md"
	$(INSTALL) -m 0644 CHANGELOG.zh-CN.md "$(DESTDIR)$(DATADIR)/CHANGELOG.zh-CN.md"
	$(INSTALL) -m 0644 SECURITY.md "$(DESTDIR)$(DATADIR)/SECURITY.md"
	$(INSTALL) -m 0644 SECURITY.zh-CN.md "$(DESTDIR)$(DATADIR)/SECURITY.zh-CN.md"
	cp -R docs/. "$(DESTDIR)$(DATADIR)/docs/"
	cp -R examples/. "$(DESTDIR)$(DATADIR)/examples/"

test:
	go test ./...

test-unit:
	go test -race -count=1 ./...

docs-check:
	python3 scripts/check-docs.py

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

update-golden:
	UPDATE_GOLDEN=1 go test ./...

test-integration:
	DBF_INTEGRATION_TARGET="$(INTEGRATION_TARGET)" ./test/integration/libvirt/run.sh

test-integration-case:
	@test -n "$(CASE)" || (echo "CASE is required, for example: make test-integration-case CASE=files" >&2; exit 1)
	DBF_INTEGRATION_TARGET="$(INTEGRATION_TARGET)" DBF_INTEGRATION_CASE="$(CASE)" ./test/integration/libvirt/run.sh

test-integration-layout:
	./test/integration/libvirt/validate-cases.sh

test-integration-source-build:
	go test -count=1 ./test/integration/sourcebuild

clean:
	go clean
	rm -f $(BINARY)
