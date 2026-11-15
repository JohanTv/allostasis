BINARY_NAME=alo
WS_BINARY_NAME=alo-ws
TRACKER_BINARY_NAME=plane-tracker
BUILD_DIR=bin
VERSION=0.1.0-alpha
DATE=$(shell date +%Y-%m-%d)
LDFLAGS=-ldflags "-s -w -X main.Version=$(VERSION) -X main.BuildDate=$(DATE)"

.PHONY: all build test clean install

all: test build

build:
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/alo
	@CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(WS_BINARY_NAME) ./cmd/alo-ws
	@CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(TRACKER_BINARY_NAME) ./cmd/plane-tracker
	@echo "✔ Binarios compilados en $(BUILD_DIR)/$(BINARY_NAME), $(BUILD_DIR)/$(WS_BINARY_NAME) y $(BUILD_DIR)/$(TRACKER_BINARY_NAME)"

test:
	@go test -v -race ./...

clean:
	@rm -rf $(BUILD_DIR)

install: build
	@mkdir -p $(HOME)/.local/bin
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(HOME)/.local/bin/$(BINARY_NAME)
	@cp $(BUILD_DIR)/$(WS_BINARY_NAME) $(HOME)/.local/bin/$(WS_BINARY_NAME)
	@cp $(BUILD_DIR)/$(TRACKER_BINARY_NAME) $(HOME)/.local/bin/plane-tracker-bin
	@ln -sf $(HOME)/.local/bin/$(BINARY_NAME) $(HOME)/.local/bin/allostasis
	@echo "✔ Instalado en ~/.local/bin/alo, ~/.local/bin/alo-ws, ~/.local/bin/plane-tracker-bin"
