package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// DefaultConfigPath is where `jabali-mcp init` writes panels.json and where the
// server looks when no config is given by the environment. It respects
// XDG_CONFIG_HOME via os.UserConfigDir (e.g. ~/.config/jabali-mcp/panels.json).
func DefaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "jabali-mcp", "panels.json")
}

// SavePanels writes the panels file (0600) creating its directory (0700).
func SavePanels(path string, cfgs []Config) error {
	if path == "" {
		return fmt.Errorf("no output path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	b, err := json.MarshalIndent(cfgs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// Registry holds one or more panel clients and a default selection.
//
// Single-panel (the common case) has one client named "default". Fleet mode
// (M3) loads several from a JSON file so a tool call can target a named panel.
type Registry struct {
	clients map[string]*Client
	def     string
}

// Options is the resolved runtime configuration.
type Options struct {
	Registry   *Registry
	AllowWrite bool // enable mutating tools at all
	DryRun     bool // force every write tool to preview instead of act
}

// Get returns the named panel client, or the default when name is empty.
func (r *Registry) Get(name string) (*Client, error) {
	if name == "" {
		name = r.def
	}
	c, ok := r.clients[name]
	if !ok {
		return nil, fmt.Errorf("unknown panel %q (known: %v)", name, r.Names())
	}
	return c, nil
}

// Names lists the configured panel names.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.clients))
	for n := range r.clients {
		out = append(out, n)
	}
	return out
}

// Multi reports whether more than one panel is configured (fleet mode).
func (r *Registry) Multi() bool { return len(r.clients) > 1 }

// Default is the default panel name.
func (r *Registry) Default() string { return r.def }

// LoadOptions builds the runtime config from the environment.
//
//	JABALI_PANEL_URL / JABALI_API_TOKEN / JABALI_PANEL_NAME / JABALI_CA_FILE
//	    single-panel config (the common case).
//	JABALI_PANELS_FILE
//	    path to a JSON array of Config objects for fleet mode; when set it takes
//	    precedence and the single-panel vars are ignored.
//	JABALI_MCP_ALLOW_WRITE = 1|true
//	    enable mutating tools; otherwise the server is read-only.
//	JABALI_MCP_DRY_RUN = 1|true
//	    force every write tool to preview the request instead of sending it.
func LoadOptions() (Options, error) {
	var opts Options
	opts.AllowWrite = envBool("JABALI_MCP_ALLOW_WRITE")
	opts.DryRun = envBool("JABALI_MCP_DRY_RUN")

	if f := os.Getenv("JABALI_PANELS_FILE"); f != "" {
		reg, err := loadPanelsFile(f)
		if err != nil {
			return opts, err
		}
		opts.Registry = reg
		return opts, nil
	}

	// No explicit config in the env: if there's no single-panel URL either, fall
	// back to the default panels.json written by `jabali-mcp init`, when present.
	// This lets a configured client be just `command: jabali-mcp`, no env.
	if os.Getenv("JABALI_PANEL_URL") == "" {
		if p := DefaultConfigPath(); p != "" {
			if _, err := os.Stat(p); err == nil {
				reg, err := loadPanelsFile(p)
				if err != nil {
					return opts, err
				}
				opts.Registry = reg
				return opts, nil
			}
		}
	}

	cfg := Config{
		Name:    os.Getenv("JABALI_PANEL_NAME"),
		BaseURL: os.Getenv("JABALI_PANEL_URL"),
		Token:   os.Getenv("JABALI_API_TOKEN"),
		CAFile:  os.Getenv("JABALI_CA_FILE"),
	}
	if cfg.BaseURL == "" {
		return opts, fmt.Errorf("no panel configured — run `jabali-mcp init` to set one up, " +
			"or set JABALI_PANEL_URL + JABALI_API_TOKEN")
	}
	c, err := New(cfg)
	if err != nil {
		return opts, err
	}
	opts.Registry = &Registry{clients: map[string]*Client{c.Name(): c}, def: c.Name()}
	return opts, nil
}

// NewRegistry builds a registry from one or more configs. The first entry is
// the default panel. Names must be unique.
func NewRegistry(cfgs []Config) (*Registry, error) {
	if len(cfgs) == 0 {
		return nil, fmt.Errorf("no panels configured")
	}
	reg := &Registry{clients: make(map[string]*Client, len(cfgs))}
	for i, cfg := range cfgs {
		c, err := New(cfg)
		if err != nil {
			return nil, fmt.Errorf("panel entry %d: %w", i, err)
		}
		if _, dup := reg.clients[c.Name()]; dup {
			return nil, fmt.Errorf("duplicate panel name %q", c.Name())
		}
		reg.clients[c.Name()] = c
		if reg.def == "" {
			reg.def = c.Name() // first entry is the default
		}
	}
	return reg, nil
}

func loadPanelsFile(path string) (*Registry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read panels file: %w", err)
	}
	var cfgs []Config
	if err := json.Unmarshal(raw, &cfgs); err != nil {
		return nil, fmt.Errorf("parse panels file %s: %w", path, err)
	}
	reg, err := NewRegistry(cfgs)
	if err != nil {
		return nil, fmt.Errorf("panels file %s: %w", path, err)
	}
	return reg, nil
}

func envBool(key string) bool {
	v := os.Getenv(key)
	if v == "" {
		return false
	}
	b, _ := strconv.ParseBool(v)
	return b
}
