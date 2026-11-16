BINARY_NAME=alo
BUILD_DIR=bin
VERSION=0.1.0-alpha
DATE=$(shell date +%Y-%m-%d)
LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildDate=$(DATE)"

.PHONY: all build test clean install docs docs-check

all: test build

build:
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/alo
	@echo "✔ Binario compilado en $(BUILD_DIR)/$(BINARY_NAME)"

test:
	@go test -v -race ./...

docs:
	@go run ./cmd/docgen

docs-check:
	@go run ./cmd/docgen --check

clean:
	@rm -rf $(BUILD_DIR)

install: build
	@mkdir -p $(HOME)/.local/bin
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(HOME)/.local/bin/$(BINARY_NAME)
	@ln -sf $(HOME)/.local/bin/$(BINARY_NAME) $(HOME)/.local/bin/allostasis
	@ln -sf $(HOME)/.local/bin/$(BINARY_NAME) $(HOME)/.local/bin/allo
	@echo "✔ Instalado en ~/.local/bin/alo y symlinks allostasis, allo"
