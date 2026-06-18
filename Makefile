BINARY := dbf
PACKAGE := ./cmd/dbf
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
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

.PHONY: build install test test-unit test-integration clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) $(PACKAGE)

install: build
	$(INSTALL) -d "$(DESTDIR)$(BINDIR)"
	$(INSTALL) -m 0755 "$(BINARY)" "$(DESTDIR)$(BINDIR)/dbf"

test:
	go test ./...

test-unit:
	go test -race -count=1 ./...

test-integration:
	./test/integration/libvirt/run.sh

clean:
	go clean
	rm -f $(BINARY)
