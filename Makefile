.PHONY: build build-go build-python build-web clean install-deps test run-web help

help:
	@echo "etcd-extract Build Commands"
	@echo ""
	@echo "CLI Tools:"
	@echo "  make build          - Build Go standalone executable (default, recommended)"
	@echo "  make build-go       - Build Go executable (static binary, no deps)"
	@echo "  make build-python   - Build Python executable (requires PyInstaller)"
	@echo ""
	@echo "Web Interface:"
	@echo "  make build-web      - Build web server executable"
	@echo "  make run-web        - Build and run web server on port 8080"
	@echo ""
	@echo "Maintenance:"
	@echo "  make install-deps   - Install Python dependencies"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make test           - Test the built executable"
	@echo ""
	@echo "Recommended: Use 'make build' for CLI or 'make run-web' for web GUI"
	@echo ""

build: build-go

build-go:
	@./build-go.sh

build-python:
	@./build-executable.sh

install-deps:
	@./install-deps.sh

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf build/ dist/ *.spec __pycache__/
	@echo "✓ Clean complete"

test: dist/etcd-extract
	@echo "Testing standalone executable..."
	@./dist/etcd-extract --help > /dev/null && echo "✓ Executable works!" || echo "✗ Executable test failed"

dist/etcd-extract:
	@echo "Executable not found. Run 'make build' first."
	@exit 1

build-web:
	@echo "Building web server..."
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/etcd-extract-web web-server.go
	@echo "✓ Web server built: dist/etcd-extract-web"

run-web: build-web
	@echo "Starting web server on http://localhost:8080"
	@./dist/etcd-extract-web
