package gen

import "testing"

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
