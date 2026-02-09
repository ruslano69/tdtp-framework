.PHONY: all build install test clean example

# Default target
all: build

# Build Go shared library
build:
	@echo "Building Go shared library..."
	cd libtdtp && go build -buildmode=c-shared -o ../tdtp/libtdtp.so main.go
	@echo "✅ Build complete: tdtp/libtdtp.so"

# Install Python package (development mode)
install: build
	@echo "Installing Python package..."
	pip install -e .
	@echo "✅ Installation complete"

# Run example
example: build
	@echo "Running example..."
	@echo "You need to provide a TDTP file:"
	@echo "  python example.py <path-to-tdtp-file>"
	@echo ""
	@echo "Create test file with tdtpcli:"
	@echo "  tdtpcli --export users --output test.tdtp.xml"

# Test Python module
test: build
	@echo "Testing Python module..."
	python -c "import tdtp; print('TDTP version:', tdtp.__version__)"
	@echo "✅ Import test passed"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f tdtp/libtdtp.so tdtp/libtdtp.dylib tdtp/libtdtp.dll
	rm -f libtdtp/libtdtp.h libtdtp/libtdtp.so libtdtp/libtdtp.dylib
	rm -rf build/ dist/ *.egg-info
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	@echo "✅ Clean complete"

# Build for different platforms
build-linux:
	cd libtdtp && CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
		go build -buildmode=c-shared -o ../tdtp/libtdtp.so main.go

build-macos:
	cd libtdtp && CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 \
		go build -buildmode=c-shared -o ../tdtp/libtdtp.dylib main.go

build-windows:
	cd libtdtp && CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
		go build -buildmode=c-shared -o ../tdtp/libtdtp.dll main.go

# Help
help:
	@echo "TDTP Python Module - Build System"
	@echo ""
	@echo "Targets:"
	@echo "  make build       - Build Go shared library"
	@echo "  make install     - Build and install Python package (dev mode)"
	@echo "  make test        - Test Python module import"
	@echo "  make example     - Show how to run example"
	@echo "  make clean       - Remove build artifacts"
	@echo "  make build-linux - Build for Linux"
	@echo "  make build-macos - Build for macOS"
	@echo "  make help        - Show this help"
