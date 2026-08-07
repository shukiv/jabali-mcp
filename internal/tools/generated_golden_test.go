package tools_test

import (
	"os"
	"testing"

	"github.com/shukiv/jabali-mcp/internal/gen"
)

// TestGeneratedIsUpToDate fails if a generated tools file differs from what the
// generator produces for the current spec + curation. This is the drift guard:
// change any of openapi/*.yaml and you must run `make gen` (or `go generate
// ./...`) before committing.
func TestGeneratedIsUpToDate(t *testing.T) {
	cases := []struct {
		curation string
		group    string
		file     string
	}{
		{"../../openapi/tools.yaml", "", "generated.go"},
		{"../../openapi/admin-tools.yaml", "Admin", "generated_admin.go"},
	}
	for _, c := range cases {
		want, err := gen.Generate("../../openapi/openapi.yaml", c.curation, c.group)
		if err != nil {
			t.Fatalf("generate %s: %v", c.file, err)
		}
		got, err := os.ReadFile(c.file)
		if err != nil {
			t.Fatalf("read %s: %v", c.file, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s is stale — run `make gen` (or `go generate ./...`) and commit the result", c.file)
		}
	}
}
