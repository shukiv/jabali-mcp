BIN      ?= jabali-mcp
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -X main.version=$(VERSION)

.PHONY: build gen docs test vet tidy clean smoke panel-routes

# Panel checkout and commit that openapi/panel-routes.txt is read from. The
# spec is read from the commit (git show), not the working tree, which may sit
# on a feature branch.
PANEL_REPO ?= ../jabali2
PANEL_REF  ?= origin/main

# Zero-mutation live smoke against the configured panel. Run before tagging;
# exits non-zero on any failure (never pipe it — that masks the exit code).
smoke: build
	go run ./scripts/smoke ./$(BIN)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/jabali-mcp

gen:
	go run ./cmd/gen-tools -spec openapi/openapi.yaml -curation openapi/tools.yaml -out internal/tools/generated.go
	go run ./cmd/gen-tools -spec openapi/openapi.yaml -curation openapi/admin-tools.yaml -out internal/tools/generated_admin.go -group Admin
	$(MAKE) docs

# Refresh the pin of routes the panel really serves. TestCuratedOpsArePanelRoutes
# fails when a curated op is missing from it (the op would 404).
panel-routes:
	git -C $(PANEL_REPO) show '$(PANEL_REF):panel-api/internal/api/openapi.yaml' | \
		go run ./cmd/gen-tools -panel-routes -spec /dev/stdin -source "$$(git -C $(PANEL_REPO) rev-parse --short '$(PANEL_REF)')" -out openapi/panel-routes.txt

docs:
	go run ./cmd/gen-tools -docs -spec openapi/openapi.yaml -curation openapi/tools.yaml -admin-curation openapi/admin-tools.yaml -out docs/TOOLS.md

test:
	go test -race ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BIN)
