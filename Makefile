BUILD_DIR=bin
BUILD_VERSION ?= $(shell head -1 .version)
GIT_HASH ?= $(shell git rev-parse --short=8 HEAD)

.PHONY: build
build:
	@mkdir -p ${BUILD_DIR}
	@CGO_ENABLED=0 go build --ldflags "-X 'main.version=$(BUILD_VERSION)-$(GIT_HASH)'" -o ${BUILD_DIR}/app .

.PHONY: clean
clean:
	@rm -fr ${BUILD_DIR}

.PHONY: all
all: tidy fmt lint build unittest

.PHONY: lint
lint:
	@echo "[golangci-lint] Running golangci-lint..."
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run --timeout=5m 2>&1
	@echo "[staticcheck] Running staticcheck..."
	@go run honnef.co/go/tools/cmd/staticcheck@latest
	@echo "------------------------------------[Done]"

.PHONY: fmt
fmt:
	@echo "[fmt] Format go project..."
	@gofmt -s -w . 2>&1
	@echo "------------------------------------[Done]"

.PHONY: tidy
tidy:
	@go mod tidy

.PHONY: run
run: build
	@${BUILD_DIR}/app