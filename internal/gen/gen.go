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
	Minimum     *int              `yaml:"minimum"`
	Maximum     *int              `yaml:"maximum"`
	MinLength   *int              `yaml:"minLength"`
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
	Enum     []string // string enum constraint
	Min      *int     // integer minimum
	Max      *int     // integer maximum
	MinLen   *int     // string minLength
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
	QueryParams []field
	BodyFields  []field
}

// Generate returns the formatted Go source for a tools file. group names the
// registration functions: "" → registerRead/registerWrite (the tenant surface);
// "Admin" → registerAdminRead/registerAdminWrite (the admin surface).
func Generate(specPath, curationPath, group string) ([]byte, error) {
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

	src := emit(filepath.Base(specPath), filepath.Base(curationPath), group, models)
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
		switch p.In {
		case "path":
			jsonName := pathParamName(path, p.Name)
			m.PathParams = append(m.PathParams, field{
				JSONName: jsonName,
				GoName:   camel(jsonName),
				GoType:   "string",
				Required: true,
				Desc:     "the " + strings.TrimSuffix(jsonName, "_id") + "'s ULID",
			})
		case "query":
			m.QueryParams = append(m.QueryParams, field{
				JSONName: p.Name,
				GoName:   camel(p.Name),
				GoType:   goType(p.Schema, p.Required),
				Required: p.Required,
				Desc:     firstNonEmpty(propDesc(p.Schema), strings.ReplaceAll(p.Name, "_", " ")),
				Enum:     p.Schema.Enum,
				Min:      p.Schema.Minimum,
				Max:      p.Schema.Maximum,
				MinLen:   p.Schema.MinLength,
			})
		}
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
					Desc:   firstNonEmpty(propDesc(ps), strings.ReplaceAll(n, "_", " ")),
					Enum:   ps.Enum,
					Min:    ps.Minimum,
					Max:    ps.Maximum,
					MinLen: ps.MinLength,
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
			// Hyphenated segments (e.g. database-users) must yield a valid Go
			// identifier and a snake_case JSON name.
			return strings.ReplaceAll(singular(segs[i-1]), "-", "_") + "_id"
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

func emit(specFile, curFile, group string, models []toolModel) []byte {
	// net/url is needed when a tool escapes a path parameter or builds a query
	// string; strconv when a query parameter is a non-string type.
	usesURL, usesStrconv := false, false
	for _, m := range models {
		if len(m.PathParams) > 0 || len(m.QueryParams) > 0 {
			usesURL = true
		}
		for _, q := range m.QueryParams {
			if q.GoType != "string" {
				usesStrconv = true
			}
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by cmd/gen-tools from %s + %s. DO NOT EDIT.\n\n", specFile, curFile)
	b.WriteString("package tools\n\n")
	b.WriteString("import (\n\t\"context\"\n\t\"net/http\"\n")
	if usesURL {
		b.WriteString("\t\"net/url\"\n")
	}
	if usesStrconv {
		b.WriteString("\t\"strconv\"\n")
	}
	b.WriteString("\n\t\"github.com/modelcontextprotocol/go-sdk/mcp\"\n\n")
	b.WriteString("\t\"github.com/shukiv/jabali-mcp/internal/client\"\n)\n\n")

	writeGroup(&b, "register"+group+"Read", models, "read")
	writeGroup(&b, "register"+group+"Write", models, "write")
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
	if m.Group == "write" {
		b.WriteString("\t\t\tdryRunArg\n")
	}
	if m.Destructive {
		b.WriteString("\t\t\tconfirmArg\n")
	}
	for _, f := range m.PathParams {
		fmt.Fprintf(b, "\t\t\t%s %s `json:%q jsonschema:%q`\n", f.GoName, f.GoType, f.JSONName, f.Desc)
	}
	for _, f := range m.QueryParams {
		tag := f.JSONName
		if !f.Required {
			tag += ",omitempty"
		}
		fmt.Fprintf(b, "\t\t\t%s %s `json:%q jsonschema:%q`\n", f.GoName, f.GoType, tag, f.Desc)
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

	emitValidate(b, m)
	fmt.Fprintf(b, "\t\t\t\tpath := %s\n", pathExpr(m))
	if m.Paginated {
		b.WriteString("\t\t\t\tpath += listQuery(in.Page, in.PageSize, in.Q)\n")
	}
	if len(m.QueryParams) > 0 {
		b.WriteString("\t\t\t\tq := url.Values{}\n")
		for _, f := range m.QueryParams {
			emitQueryAssign(b, f)
		}
		b.WriteString("\t\t\t\tif enc := q.Encode(); enc != \"\" {\n\t\t\t\t\tpath += \"?\" + enc\n\t\t\t\t}\n")
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

// emitValidate emits the value-constraint checks the spec declares (enum,
// minLength, minimum/maximum). Type and required-presence are already enforced
// by the SDK from the inferred schema; this adds the value constraints and fails
// fast with a precise message before any request is sent. Optional fields are
// checked only when the caller supplied a value.
func emitValidate(b *strings.Builder, m toolModel) {
	fields := append(append([]field{}, m.BodyFields...), m.QueryParams...)
	for _, f := range fields {
		if len(f.Enum) == 0 && f.Min == nil && f.Max == nil && f.MinLen == nil {
			continue
		}
		guarded := !f.Required
		indent := "\t\t\t\t"
		if guarded {
			switch f.GoType {
			case "string":
				fmt.Fprintf(b, "%sif in.%s != \"\" {\n", indent, f.GoName)
			case "int":
				fmt.Fprintf(b, "%sif in.%s != 0 {\n", indent, f.GoName)
			default:
				guarded = false
			}
		}
		body := indent
		if guarded {
			body = indent + "\t"
		}
		if len(f.Enum) > 0 {
			fmt.Fprintf(b, "%sif r := vEnum(%q, in.%s, []string{%s}); r != nil {\n%s\treturn r, nil, nil\n%s}\n",
				body, f.JSONName, f.GoName, quoteList(f.Enum), body, body)
		}
		if f.MinLen != nil {
			fmt.Fprintf(b, "%sif r := vMinLen(%q, in.%s, %d); r != nil {\n%s\treturn r, nil, nil\n%s}\n",
				body, f.JSONName, f.GoName, *f.MinLen, body, body)
		}
		if f.Min != nil {
			fmt.Fprintf(b, "%sif r := vMin(%q, in.%s, %d); r != nil {\n%s\treturn r, nil, nil\n%s}\n",
				body, f.JSONName, f.GoName, *f.Min, body, body)
		}
		if f.Max != nil {
			fmt.Fprintf(b, "%sif r := vMax(%q, in.%s, %d); r != nil {\n%s\treturn r, nil, nil\n%s}\n",
				body, f.JSONName, f.GoName, *f.Max, body, body)
		}
		if guarded {
			fmt.Fprintf(b, "%s}\n", indent)
		}
	}
}

func quoteList(vals []string) string {
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return strings.Join(parts, ", ")
}

// emitQueryAssign adds a query parameter to url.Values, converting to string and
// (for optional fields) skipping the zero value.
func emitQueryAssign(b *strings.Builder, f field) {
	var val string
	switch f.GoType {
	case "int":
		val = "strconv.Itoa(in." + f.GoName + ")"
	case "float64":
		val = "strconv.FormatFloat(in." + f.GoName + ", 'f', -1, 64)"
	case "bool":
		val = "strconv.FormatBool(in." + f.GoName + ")"
	case "*bool":
		fmt.Fprintf(b, "\t\t\t\tif in.%s != nil { q.Set(%q, strconv.FormatBool(*in.%s)) }\n", f.GoName, f.JSONName, f.GoName)
		return
	default:
		val = "in." + f.GoName
	}
	if f.Required {
		fmt.Fprintf(b, "\t\t\t\tq.Set(%q, %s)\n", f.JSONName, val)
		return
	}
	switch f.GoType {
	case "string":
		fmt.Fprintf(b, "\t\t\t\tif in.%s != \"\" { q.Set(%q, %s) }\n", f.GoName, f.JSONName, val)
	case "int", "float64":
		fmt.Fprintf(b, "\t\t\t\tif in.%s != 0 { q.Set(%q, %s) }\n", f.GoName, f.JSONName, val)
	default:
		fmt.Fprintf(b, "\t\t\t\tq.Set(%q, %s)\n", f.JSONName, val)
	}
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
