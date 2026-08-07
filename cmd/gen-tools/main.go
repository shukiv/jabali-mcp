// Command gen-tools writes internal/tools/generated.go from the vendored
// OpenAPI spec and the curation file. It is a thin CLI over internal/gen.
//
// Run via `go generate ./...` or `make gen`.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/shukiv/jabali-mcp/internal/gen"
)

func main() {
	spec := flag.String("spec", "openapi/openapi.yaml", "path to the OpenAPI spec")
	curation := flag.String("curation", "openapi/tools.yaml", "path to the curation file")
	out := flag.String("out", "internal/tools/generated.go", "output Go file")
	group := flag.String("group", "", `registration group: "" for tenant, "Admin" for admin`)
	docs := flag.Bool("docs", false, "emit the markdown tool reference instead of Go source (-curation is the tenant file; the admin file is derived from its directory)")
	adminCuration := flag.String("admin-curation", "openapi/admin-tools.yaml", "path to the admin curation file (docs mode)")
	flag.Parse()

	if *docs {
		md, err := gen.GenerateDocs(*spec, *curation, *adminCuration)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gen-tools:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*out, md, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "gen-tools:", err)
			os.Exit(1)
		}
		fmt.Printf("gen-tools: wrote %s\n", *out)
		return
	}

	src, err := gen.Generate(*spec, *curation, *group)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-tools:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen-tools:", err)
		os.Exit(1)
	}
	fmt.Printf("gen-tools: wrote %s\n", *out)
}
