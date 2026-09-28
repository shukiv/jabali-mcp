package gen

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSingular(t *testing.T) {
	cases := map[string]string{
		// plain "+s" plurals: strip only the trailing "s"
		"domains":    "domain",
		"databases":  "database", // regression: the old "ses" rule produced "databas"
		"backups":    "backup",
		"responses":  "response",
		"settings":   "setting",
		"ssh-keys":   "ssh-key",
		"api-tokens": "api-token",
		// sibilant "+es" plurals: strip "es"
		"mailboxes": "mailbox",
		"addresses": "address",
		"classes":   "class",
		"boxes":     "box",
		"churches":  "church",
		"brushes":   "brush",
		// no plural suffix: unchanged
		"ssl":  "ssl",
		"cron": "cron",
	}
	for in, want := range cases {
		if got := singular(in); got != want {
			t.Errorf("singular(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPathParamName(t *testing.T) {
	cases := []struct {
		path, specName, want string
	}{
		{"/databases/{id}", "id", "database_id"}, // regression: was "databas_id"
		{"/domains/{id}", "id", "domain_id"},
		{"/domains/{id}/ssl", "id", "domain_id"},
		{"/database-users/{id}", "id", "database_user_id"}, // hyphen → snake_case
		{"/mailboxes/{id}", "id", "mailbox_id"},
		{"/dns/records/{id}", "id", "record_id"},
		{"/me/backups/{id}/manifest", "id", "backup_id"},
		{"/files", "path", "path"}, // no placeholder match: fall back to the spec name
	}
	for _, c := range cases {
		if got := pathParamName(c.path, c.specName); got != c.want {
			t.Errorf("pathParamName(%q, %q) = %q, want %q", c.path, c.specName, got, c.want)
		}
	}
}

func TestGoType(t *testing.T) {
	cases := []struct {
		name     string
		s        schema
		required bool
		want     string
	}{
		{"required int", schema{Type: "integer"}, true, "int"},
		{"optional int", schema{Type: "integer"}, false, "*int"}, // nil = omit; 0 is meaningful
		{"required bool", schema{Type: "boolean"}, true, "bool"},
		{"optional bool", schema{Type: "boolean"}, false, "*bool"},
		{"number", schema{Type: "number"}, false, "float64"},
		{"string", schema{Type: "string"}, false, "string"},
	}
	for _, c := range cases {
		if got := goType(c.s, c.required); got != c.want {
			t.Errorf("%s: goType(%+v, %v) = %q, want %q", c.name, c.s, c.required, got, c.want)
		}
	}
}

// TestCuratedOpsArePanelRoutes is the guard against a tool that 404s: every
// curated op must be a route the panel serves. openapi/panel-routes.txt is
// pinned from the panel's embedded spec by `make panel-routes`.
func TestCuratedOpsArePanelRoutes(t *testing.T) {
	if err := CheckPanelRoutes("../../openapi/panel-routes.txt",
		"../../openapi/tools.yaml", "../../openapi/admin-tools.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestPanelRoutes(t *testing.T) {
	spec := `openapi: 3.0.3
paths:
  /mailboxes/{mbid}/forwarders:
    parameters: [ { in: path, name: mbid, required: true, schema: { type: string } } ]
    post: { summary: create }
  /users:
    get: { summary: list }
    post: { summary: create }
  /nic/update:
    servers: [ { url: "https://{panel-hostname}:8443" } ]
    get: { summary: dyndns }
`
	p := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(p, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := PanelRoutes(p)
	if err != nil {
		t.Fatal(err)
	}
	// Parameter names normalise to {}; the path-level parameters key is not a
	// method; /nic/update is mounted outside /api/v1 and is left out.
	want := []string{"GET /users", "POST /mailboxes/{}/forwarders", "POST /users"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PanelRoutes = %q, want %q", got, want)
	}
}

func TestCheckPanelRoutesNamesMissingOps(t *testing.T) {
	dir := t.TempDir()
	routes := filepath.Join(dir, "panel-routes.txt")
	cur := filepath.Join(dir, "tools.yaml")
	if err := os.WriteFile(routes, []byte("# header\nGET /users\nPOST /mailboxes/{}/forwarders\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cur, []byte(`tools:
  - { op: "GET /users",                 name: ok_list,      group: read }
  - { op: "POST /mailboxes/{id}/forwarders", name: ok_create, group: write }
  - { op: "GET /admin/users",           name: dead_list,    group: read }
  - { op: "PUT /mailboxes/{id}/password", name: dead_set,   group: write }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := CheckPanelRoutes(routes, cur)
	if err == nil {
		t.Fatal("CheckPanelRoutes passed with two ops the panel does not serve")
	}
	for _, want := range []string{"GET /admin/users (dead_list)", "PUT /mailboxes/{id}/password (dead_set)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
	for _, ok := range []string{"ok_list", "ok_create"} {
		if strings.Contains(err.Error(), ok) {
			t.Errorf("error %q names %s, which the panel serves", err, ok)
		}
	}
}
