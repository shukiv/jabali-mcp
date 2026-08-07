BIN      ?= jabali-mcp
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -X main.version=$(VERSION)

.PHONY: build gen test vet tidy clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/jabali-mcp

gen:
	go run ./cmd/gen-tools -spec openapi/openapi.yaml -curation openapi/tools.yaml -out internal/tools/generated.go
	go run ./cmd/gen-tools -spec openapi/openapi.yaml -curation openapi/admin-tools.yaml -out internal/tools/generated_admin.go -group Admin

test:
	go test -race ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BIN)
