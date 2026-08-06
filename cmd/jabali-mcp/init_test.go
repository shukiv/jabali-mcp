package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitWizardWritesVerifiedPanels(t *testing.T) {
	// Fake panel: 200 on GET /domains means the token is accepted.
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/domains" && r.Header.Get("Authorization") == "Bearer jat_good" {
			hits++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	out := filepath.Join(t.TempDir(), "panels.json")
	answers := strings.Join([]string{
		"prod",     // name
		ts.URL,     // url
		"jat_good", // token
		"n",        // add another? no
	}, "\n") + "\n"

	var buf bytes.Buffer
	if err := runInitIO(out, false /*verify on*/, bufio.NewReader(strings.NewReader(answers)), &buf); err != nil {
		t.Fatalf("runInitIO: %v", err)
	}
	if hits != 1 {
		t.Errorf("expected the wizard to verify the token once, got %d panel hits", hits)
	}
	if !strings.Contains(buf.String(), "token accepted") {
		t.Errorf("expected a verification success line, got:\n%s", buf.String())
	}

	// File written, 0600, correct content.
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("panels.json perms = %o, want 600", info.Mode().Perm())
	}
	raw, _ := os.ReadFile(out)
	var got []struct {
		Name, URL, Token string
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse written file: %v", err)
	}
	if len(got) != 1 || got[0].Name != "prod" || got[0].URL != ts.URL || got[0].Token != "jat_good" {
		t.Errorf("unexpected written content: %+v", got)
	}
}

func TestInitWizardSavesOnVerifyFailure(t *testing.T) {
	// Unreachable panel: verify fails, but the panel is still saved (with a warning).
	out := filepath.Join(t.TempDir(), "panels.json")
	answers := "prod\nhttps://127.0.0.1:1/api/v1\njat_x\nn\n"

	var buf bytes.Buffer
	if err := runInitIO(out, false, bufio.NewReader(strings.NewReader(answers)), &buf); err != nil {
		t.Fatalf("runInitIO: %v", err)
	}
	if !strings.Contains(buf.String(), "saving it anyway") {
		t.Errorf("expected a save-anyway warning, got:\n%s", buf.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("panels.json should still be written on verify failure: %v", err)
	}
}
