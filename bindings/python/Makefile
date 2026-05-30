# Makefile for tdtp Python bindings
#
# Prerequisites: Go toolchain with CGO enabled, Python 3.9+
#
# Typical dev workflow:
#   make build-lib   → compile libtdtp.so from Go sources
#   make install-dev → pip install -e . (editable mode)
#   make test        → run all tests
#   make bench       → run benchmark comparison J vs D

REPO_ROOT   := $(shell git -C ../.. rev-parse --show-toplevel 2>/dev/null || echo "../..")
LIB_SRC     := $(REPO_ROOT)/pkg/python/libtdtp
LIB_OUT     := $(CURDIR)/tdtp/libtdtp.so
HEADER_OUT  := $(CURDIR)/tdtp/libtdtp.h

GOOS        := $(shell go env GOOS)
GOARCH      := $(shell go env GOARCH)

ifeq ($(GOOS),windows)
    LIB_OUT := $(CURDIR)/tdtp/libtdtp.dll
else ifeq ($(GOOS),darwin)
    LIB_OUT := $(CURDIR)/tdtp/libtdtp.dylib
endif

.PHONY: all build-lib build-lib-full install-dev test bench clean help sync-version

all: build-lib install-dev

## build-lib: compile without zstd/processors (no extra deps needed)
##            Supports: J_ReadFile (plain), J_WriteFile, J_FilterRows, J_Diff
##            Runs sync-version first so the .so and pyproject.toml never drift.
build-lib: sync-version
	@echo "→ Building $(LIB_OUT) basic (GOOS=$(GOOS) GOARCH=$(GOARCH))"
	cd $(LIB_SRC) && \
	GOWORK=off go build \
		-buildmode=c-shared \
		-o $(LIB_OUT) .
	@if [ -f $(LIB_SRC)/libtdtp.h ]; then mv $(LIB_SRC)/libtdtp.h $(HEADER_OUT); fi
	@echo "✓ $(LIB_OUT) [basic]"

## build-lib-full: compile with zstd + processors support (-tags compress)
##                 Requires: go mod download (needs internet on first run)
##                 Supports all J_* functions including compress/decompress/processors
##                 Runs sync-version first so the .so and pyproject.toml never drift.
build-lib-full: sync-version
	@echo "→ Building $(LIB_OUT) full (GOOS=$(GOOS) GOARCH=$(GOARCH))"
	cd $(LIB_SRC) && \
	GOWORK=off go build \
		-tags compress \
		-buildmode=c-shared \
		-o $(LIB_OUT) .
	@if [ -f $(LIB_SRC)/libtdtp.h ]; then mv $(LIB_SRC)/libtdtp.h $(HEADER_OUT); fi
	@echo "✓ $(LIB_OUT) [full]"

## sync-version: copy version from pkg/core/version/version.go into pyproject.toml
##               Single source of truth lives in Go; this keeps build metadata in sync.
sync-version:
	@ver=$$(grep -oE 'Version = "[0-9]+\.[0-9]+\.[0-9]+[^"]*"' $(REPO_ROOT)/pkg/core/version/version.go | grep -oE '[0-9]+\.[0-9]+\.[0-9]+[^"]*'); \
	if [ -z "$$ver" ]; then echo "✗ could not read version from Go source"; exit 1; fi; \
	sed -i.bak -E 's/^version = "[^"]*"/version = "'$$ver'"/' pyproject.toml && rm -f pyproject.toml.bak; \
	echo "✓ pyproject.toml version → $$ver (from pkg/core/version)"

## install-dev: install package in editable mode (requires build-lib first)
install-dev:
	pip install -e ".[dev]"

## test: run all pytest tests (skips TODO tests, reports coverage)
test:
	pytest tests/ -v

## test-j: run only JSON API tests
test-j:
	pytest tests/test_api_j.py -v

## test-d: run only Direct API tests
test-d:
	pytest tests/test_api_d.py -v

## bench: compare J_* vs D_* performance (requires pytest-benchmark)
bench:
	pytest tests/ --benchmark-only --benchmark-group-by=name -v

## clean: remove compiled artifacts
clean:
	rm -f $(LIB_OUT) $(HEADER_OUT)
	rm -rf __pycache__ tdtp/__pycache__ tests/__pycache__
	rm -rf .pytest_cache

## help: show this message
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
