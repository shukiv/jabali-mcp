package tools_test

import (
	"os"
	"testing"

	"github.com/shukiv/jabali-mcp/internal/gen"
)

// TestGeneratedIsUpToDate fails if internal/tools/generated.go differs from what
// the generator produces for the current spec + curation. This is the drift
// guard: change openapi/openapi.yaml or openapi/tools.yaml and you must run
// `make gen` (or `go generate ./...`) before committing.
func TestGeneratedIsUpToDate(t *testing.T) {
	want, err := gen.Generate("../../openapi/openapi.yaml", "../../openapi/tools.yaml")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	got, err := os.ReadFile("generated.go")
	if err != nil {
		t.Fatalf("read generated.go: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("internal/tools/generated.go is stale — run `make gen` (or `go generate ./...`) and commit the result")
	}
}
