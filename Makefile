BIN      ?= jabali-mcp
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -X main.version=$(VERSION)

.PHONY: build test vet tidy clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/jabali-mcp

test:
	go test -race ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BIN)
