package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavePanelsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panels.json")
	in := []Config{
		{Name: "a", BaseURL: "https://a:8443/api/v1", Token: "jat_a"},
		{Name: "b", BaseURL: "https://b:8443/api/v1", Token: "jat_b"},
	}
	if err := SavePanels(path, in); err != nil {
		t.Fatalf("SavePanels: %v", err)
	}
	reg, err := loadPanelsFile(path)
	if err != nil {
		t.Fatalf("loadPanelsFile: %v", err)
	}
	if reg.Default() != "a" {
		t.Errorf("default panel = %q, want a", reg.Default())
	}
	if !reg.Multi() || len(reg.Names()) != 2 {
		t.Errorf("expected 2 panels, got %v", reg.Names())
	}
	if _, err := reg.Get("b"); err != nil {
		t.Errorf("panel b should resolve: %v", err)
	}
}

func TestLoadOptionsFromPanelsFileEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panels.json")
	if err := SavePanels(path, []Config{{Name: "only", BaseURL: "https://x/api/v1", Token: "jat_x"}}); err != nil {
		t.Fatal(err)
	}
	// Isolate env so the test doesn't pick up a developer's real config.
	for _, k := range []string{"JABALI_PANEL_URL", "JABALI_API_TOKEN", "JABALI_MCP_ALLOW_WRITE", "JABALI_MCP_DRY_RUN"} {
		t.Setenv(k, "")
	}
	t.Setenv("JABALI_PANELS_FILE", path)

	opts, err := LoadOptions()
	if err != nil {
		t.Fatalf("LoadOptions: %v", err)
	}
	if c, err := opts.Registry.Get(""); err != nil || c.Name() != "only" {
		t.Errorf("expected default panel 'only', got %v (err %v)", c, err)
	}
}

func TestLoadOptionsNoConfigIsFriendlyError(t *testing.T) {
	for _, k := range []string{"JABALI_PANELS_FILE", "JABALI_PANEL_URL", "JABALI_API_TOKEN"} {
		t.Setenv(k, "")
	}
	// Force the default-path probe to miss by pointing config at an empty dir.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := os.Stat(DefaultConfigPath()); err == nil {
		t.Skip("unexpected pre-existing default config")
	}

	_, err := LoadOptions()
	if err == nil || !strings.Contains(err.Error(), "jabali-mcp init") {
		t.Errorf("expected a friendly 'run jabali-mcp init' error, got %v", err)
	}
}
