BINARY     := lumen
CMD_DIR    := ./cmd/lumen
BUILD_DIR  := ./bin
GOFLAGS    :=

.PHONY: all build run test clean fmt vet install tidy

all: build

## build: compile the binary into ./bin/lumen
build:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD_DIR)

## run: build then run, e.g. `make run ARGS="./internal/harvest/harvest.go"`
run: build
	$(BUILD_DIR)/$(BINARY) $(ARGS)

## test: run the full test suite
test:
	go test ./...

## vet: run go vet
vet:
	go vet ./...

## fmt: gofmt all source files
fmt:
	gofmt -l -w .

## tidy: tidy go.mod/go.sum
tidy:
	go mod tidy

## install: build then install into $(GOBIN) or $(GOPATH)/bin
install:
	go install $(CMD_DIR)

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR)
