// Package gen generates the MCP tool registrations for internal/tools from the
// vendored OpenAPI spec (openapi/openapi.yaml) and the curation file
// (openapi/tools.yaml).
//
// Generate joins each curation entry with its operation in the spec (failing if
// an entry names an operation the spec lacks — the drift guard), then emits Go
// source that reuses the helpers in internal/tools (runRead/runWrite/reqSpec/…).
// cmd/gen-tools is a thin CLI over Generate; a golden test regenerates and diffs
// so the committed generated.go can never drift from the spec.
package gen

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ---- OpenAPI (minimal subset) ----

type oapiSpec struct {
	Paths      map[string]map[string]operation `yaml:"paths"`
	Components struct {
		Schemas map[string]schema `yaml:"schemas"`
	} `yaml:"components"`
}

type operation struct {
	Tags        []string     `yaml:"tags"`
	Summary     string       `yaml:"summary"`
	Parameters  []parameter  `yaml:"parameters"`
	RequestBody *requestBody `yaml:"requestBody"`
}

type parameter struct {
	In       string `yaml:"in"`
	Name     string `yaml:"name"`
	Required bool   `yaml:"required"`
	Schema   schema `yaml:"schema"`
}

type requestBody struct {
	Required bool `yaml:"required"`
	Content  map[string]struct {
		Schema schema `yaml:"schema"`
	} `yaml:"content"`
}

type schema struct {
	Ref         string            `yaml:"$ref"`
	Type        string            `yaml:"type"`
	Format      string            `yaml:"format"`
	Description string            `yaml:"description"`
	Example     any               `yaml:"example"`
	Enum        []string          `yaml:"enum"`
	Required    []string          `yaml:"required"`
	Properties  map[string]schema `yaml:"properties"`
}

// ---- Curation ----

type curation struct {
	Tools []curatedTool `yaml:"tools"`
}

type curatedTool struct {
	Op          string `yaml:"op"`
	Name        string `yaml:"name"`
	Group       string `yaml:"group"`
	Destructive bool   `yaml:"destructive"`
	Paginated   bool   `yaml:"paginated"`
}

// ---- Tool model ----

type field struct {
	JSONName string
	GoName   string
	GoType   string
	Required bool
	Desc     string
}

type toolModel struct {
	Name        string
	Summary     string
	Method      string
	Path        string
	Group       string
	Destructive bool
	Paginated   bool
	PathParams  []field
	BodyFields  []field
}

// Generate returns the formatted Go source for internal/tools/generated.go.
func Generate(specPath, curationPath string) ([]byte, error) {
	spec, err := loadSpec(specPath)
	if err != nil {
		return nil, err
	}
	cur, err := loadCuration(curationPath)
	if err != nil {
		return nil, err
	}

	models := make([]toolModel, 0, len(cur.Tools))
	for _, ct := range cur.Tools {
		m, err := buildModel(spec, ct)
		if err != nil {
			return nil, err
		}
		models = append(models, m)
	}

	src := emit(filepath.Base(specPath), filepath.Base(curationPath), models)
	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("gofmt generated source: %w\n%s", err, src)
	}
	return formatted, nil
}

func loadSpec(path string) (*oapiSpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s oapiSpec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	return &s, nil
}

func loadCuration(path string) (*curation, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c curation
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse curation: %w", err)
	}
	return &c, nil
}

func buildModel(spec *oapiSpec, ct curatedTool) (toolModel, error) {
	var m toolModel
	parts := strings.Fields(ct.Op)
	if len(parts) != 2 {
		return m, fmt.Errorf("tool %q: op %q must be \"METHOD /path\"", ct.Name, ct.Op)
	}
	method, path := parts[0], parts[1]
	methods, ok := spec.Paths[path]
	if !ok {
		return m, fmt.Errorf("tool %q: path %q not in spec (drift)", ct.Name, path)
	}
	op, ok := methods[strings.ToLower(method)]
	if !ok {
		return m, fmt.Errorf("tool %q: %s not defined on %q in spec (drift)", ct.Name, method, path)
	}
	if ct.Group != "read" && ct.Group != "write" {
		return m, fmt.Errorf("tool %q: group must be read|write, got %q", ct.Name, ct.Group)
	}

	m = toolModel{
		Name:        ct.Name,
		Summary:     firstNonEmpty(op.Summary, ct.Name),
		Method:      method,
		Path:        path,
		Group:       ct.Group,
		Destructive: ct.Destructive,
		Paginated:   ct.Paginated,
	}

	for _, p := range op.Parameters {
		if p.In != "path" {
			continue
		}
		jsonName := pathParamName(path, p.Name)
		m.PathParams = append(m.PathParams, field{
			JSONName: jsonName,
			GoName:   camel(jsonName),
			GoType:   "string",
			Required: true,
			Desc:     "the " + strings.TrimSuffix(jsonName, "_id") + "'s ULID",
		})
	}

	if op.RequestBody != nil {
		if c, ok := op.RequestBody.Content["application/json"]; ok {
			bs := resolve(spec, c.Schema)
			names := make([]string, 0, len(bs.Properties))
			for n := range bs.Properties {
				names = append(names, n)
			}
			sort.Strings(names) // deterministic output
			reqSet := map[string]bool{}
			for _, r := range bs.Required {
				reqSet[r] = true
			}
			for _, n := range names {
				ps := bs.Properties[n]
				m.BodyFields = append(m.BodyFields, field{
					JSONName: n,
					GoName:   camel(n),
					GoType:   goType(ps, reqSet[n]),
					Required: reqSet[n],
					// The MCP SDK requires a non-empty jsonschema description on
					// every field; fall back to the property name.
					Desc: firstNonEmpty(propDesc(ps), strings.ReplaceAll(n, "_", " ")),
				})
			}
		}
	}
	return m, nil
}

func resolve(spec *oapiSpec, s schema) schema {
	if s.Ref == "" {
		return s
	}
	name := s.Ref[strings.LastIndex(s.Ref, "/")+1:]
	if r, ok := spec.Components.Schemas[name]; ok {
		return r
	}
	return s
}

func goType(s schema, required bool) string {
	switch s.Type {
	case "integer":
		return "int"
	case "number":
		return "float64"
	case "boolean":
		if required {
			return "bool"
		}
		return "*bool" // optional bool: nil = omit (false is meaningful)
	default:
		return "string"
	}
}

func propDesc(s schema) string {
	if s.Description != "" {
		return s.Description
	}
	if len(s.Enum) > 0 {
		return "one of: " + strings.Join(s.Enum, ", ")
	}
	if s.Example != nil {
		return fmt.Sprintf("e.g. %v", s.Example)
	}
	return ""
}

func pathParamName(path, specName string) string {
	segs := strings.Split(strings.Trim(path, "/"), "/")
	placeholder := "{" + specName + "}"
	for i, s := range segs {
		if s == placeholder && i > 0 {
			return singular(segs[i-1]) + "_id"
		}
	}
	return specName
}

func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ses"), strings.HasSuffix(s, "xes"), strings.HasSuffix(s, "zes"),
		strings.HasSuffix(s, "ches"), strings.HasSuffix(s, "shes"):
		return strings.TrimSuffix(s, "es")
	case strings.HasSuffix(s, "s"):
		return strings.TrimSuffix(s, "s")
	default:
		return s
	}
}

func camel(snake string) string {
	var b strings.Builder
	for _, part := range strings.Split(snake, "_") {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- Emit ----

func emit(specFile, curFile string, models []toolModel) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by cmd/gen-tools from %s + %s. DO NOT EDIT.\n\n", specFile, curFile)
	b.WriteString("package tools\n\n")
	b.WriteString("import (\n\t\"context\"\n\t\"net/http\"\n\t\"net/url\"\n\n")
	b.WriteString("\t\"github.com/modelcontextprotocol/go-sdk/mcp\"\n\n")
	b.WriteString("\t\"github.com/shukiv/jabali-mcp/internal/client\"\n)\n\n")

	writeGroup(&b, "registerRead", models, "read")
	writeGroup(&b, "registerWrite", models, "write")
	return []byte(b.String())
}

func writeGroup(b *strings.Builder, fn string, models []toolModel, group string) {
	fmt.Fprintf(b, "func %s(s *mcp.Server, reg *client.Registry) {\n", fn)
	for _, m := range models {
		if m.Group != group {
			continue
		}
		emitTool(b, m)
	}
	b.WriteString("}\n\n")
}

func emitTool(b *strings.Builder, m toolModel) {
	typeName := camel(m.Name) + "In"
	method := "http.Method" + camel(strings.ToLower(m.Method))

	fmt.Fprintf(b, "\t{\n\t\ttype %s struct {\n\t\t\tpanelArg\n", typeName)
	if m.Destructive {
		b.WriteString("\t\t\tconfirmArg\n")
	}
	for _, f := range m.PathParams {
		fmt.Fprintf(b, "\t\t\t%s %s `json:%q jsonschema:%q`\n", f.GoName, f.GoType, f.JSONName, f.Desc)
	}
	if m.Paginated {
		b.WriteString("\t\t\tPage int `json:\"page,omitempty\" jsonschema:\"page number (1-based)\"`\n")
		b.WriteString("\t\t\tPageSize int `json:\"page_size,omitempty\" jsonschema:\"results per page\"`\n")
		b.WriteString("\t\t\tQ string `json:\"q,omitempty\" jsonschema:\"search filter\"`\n")
	}
	for _, f := range m.BodyFields {
		tag := f.JSONName
		if !f.Required {
			tag += ",omitempty"
		}
		fmt.Fprintf(b, "\t\t\t%s %s `json:%q jsonschema:%q`\n", f.GoName, f.GoType, tag, f.Desc)
	}
	b.WriteString("\t\t}\n")

	anno := "roAnno()"
	if m.Group == "write" {
		if m.Destructive {
			anno = "destructiveAnno()"
		} else {
			anno = "additiveAnno()"
		}
	}
	fmt.Fprintf(b, "\t\tmcp.AddTool(s, &mcp.Tool{Name: %q, Description: %q, Annotations: %s},\n",
		m.Name, m.Summary, anno)
	fmt.Fprintf(b, "\t\t\tfunc(ctx context.Context, _ *mcp.CallToolRequest, in %s) (*mcp.CallToolResult, any, error) {\n", typeName)

	fmt.Fprintf(b, "\t\t\t\tpath := %s\n", pathExpr(m))
	if m.Paginated {
		b.WriteString("\t\t\t\tpath += listQuery(in.Page, in.PageSize, in.Q)\n")
	}

	bodyExpr := "nil"
	if len(m.BodyFields) > 0 {
		b.WriteString("\t\t\t\tbody := map[string]any{}\n")
		for _, f := range m.BodyFields {
			emitBodyAssign(b, f)
		}
		bodyExpr = "body"
	}

	switch {
	case m.Group == "read":
		fmt.Fprintf(b, "\t\t\t\treturn runRead(ctx, reg, in, reqSpec{%s, path, %s})\n", method, bodyExpr)
	case m.Destructive:
		fmt.Fprintf(b, "\t\t\t\tpreview := %q + path\n", "Destructive, irreversible — "+m.Summary+". Target: ")
		fmt.Fprintf(b, "\t\t\t\treturn runWrite(ctx, reg, in, true, preview, reqSpec{%s, path, %s})\n", method, bodyExpr)
	default:
		fmt.Fprintf(b, "\t\t\t\treturn runWrite(ctx, reg, in, false, \"\", reqSpec{%s, path, %s})\n", method, bodyExpr)
	}
	b.WriteString("\t\t\t})\n\t}\n")
}

func emitBodyAssign(b *strings.Builder, f field) {
	if f.Required {
		fmt.Fprintf(b, "\t\t\t\tbody[%q] = in.%s\n", f.JSONName, f.GoName)
		return
	}
	switch f.GoType {
	case "string":
		fmt.Fprintf(b, "\t\t\t\tif in.%s != \"\" { body[%q] = in.%s }\n", f.GoName, f.JSONName, f.GoName)
	case "int", "float64":
		fmt.Fprintf(b, "\t\t\t\tif in.%s != 0 { body[%q] = in.%s }\n", f.GoName, f.JSONName, f.GoName)
	case "*bool":
		fmt.Fprintf(b, "\t\t\t\tif in.%s != nil { body[%q] = *in.%s }\n", f.GoName, f.JSONName, f.GoName)
	default:
		fmt.Fprintf(b, "\t\t\t\tbody[%q] = in.%s\n", f.JSONName, f.GoName)
	}
}

func pathExpr(m toolModel) string {
	segs := strings.Split(strings.Trim(m.Path, "/"), "/")
	ph := map[string]string{}
	for _, p := range m.PathParams {
		ph[p.JSONName] = p.GoName
	}
	var parts []string
	lit := "/"
	for _, s := range segs {
		if strings.HasPrefix(s, "{") && strings.HasSuffix(s, "}") {
			specName := strings.TrimSuffix(strings.TrimPrefix(s, "{"), "}")
			goName := ph[pathParamName(m.Path, specName)]
			if lit != "" {
				parts = append(parts, fmt.Sprintf("%q", lit))
			}
			parts = append(parts, "url.PathEscape(in."+goName+")")
			lit = "/"
		} else {
			lit += s + "/"
		}
	}
	lit = strings.TrimSuffix(lit, "/")
	if lit != "" || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%q", lit))
	}
	return strings.Join(parts, " + ")
}
