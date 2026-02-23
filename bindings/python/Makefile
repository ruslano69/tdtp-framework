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

.PHONY: all build-lib install-dev test bench clean help

all: build-lib install-dev

## build-lib: compile Go sources → libtdtp.so (+ libtdtp.h)
build-lib:
	@echo "→ Building $(LIB_OUT) (GOOS=$(GOOS) GOARCH=$(GOARCH))"
	cd $(REPO_ROOT) && \
	go build \
		-buildmode=c-shared \
		-o $(LIB_OUT) \
		./pkg/python/libtdtp/
	@# Header is generated alongside the .so
	@if [ -f $(REPO_ROOT)/libtdtp.h ]; then mv $(REPO_ROOT)/libtdtp.h $(HEADER_OUT); fi
	@echo "✓ $(LIB_OUT)"

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
