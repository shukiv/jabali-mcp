package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shukiv/jabali-mcp/internal/client"
)

// runInit is the interactive setup wizard: `jabali-mcp init`. It walks the
// operator through one or more panels, verifies each token against the panel,
// writes a 0600 panels.json, and prints the client-registration snippet.
func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	out := fs.String("output", client.DefaultConfigPath(), "where to write panels.json")
	noVerify := fs.Bool("no-verify", false, "skip testing each token against the panel")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runInitIO(*out, *noVerify, bufio.NewReader(os.Stdin), os.Stderr)
}

// runInitIO is the testable core: prompts on w, reads answers from r.
func runInitIO(out string, noVerify bool, r *bufio.Reader, w io.Writer) error {
	if out == "" {
		return fmt.Errorf("could not determine a config path; pass --output <file>")
	}
	fmt.Fprintln(w, "jabali-mcp setup — configure one or more Jabali panels.")
	fmt.Fprintln(w, "Mint a token in the panel under API Tokens (a non-admin token is safest).")
	fmt.Fprintln(w)

	var cfgs []client.Config
	seen := map[string]bool{}
	for i := 1; ; i++ {
		fmt.Fprintf(w, "Panel #%d\n", i)
		name := promptDefault(r, w, "  Name", defaultName(i, seen))
		if seen[name] {
			fmt.Fprintf(w, "  ! name %q already used; try another\n", name)
			i--
			continue
		}
		url := promptRequired(r, w, "  Panel URL (e.g. https://host:8443/api/v1)")
		token := promptRequired(r, w, "  API token (jat_…)")
		cfg := client.Config{Name: name, BaseURL: url, Token: token}

		if !noVerify {
			if err := verifyPanel(cfg); err != nil {
				fmt.Fprintf(w, "  ! %v — saving it anyway\n", err)
			} else {
				fmt.Fprintln(w, "  ✓ token accepted by the panel")
			}
		}
		cfgs = append(cfgs, cfg)
		seen[name] = true

		if !promptYesNo(r, w, "Add another panel?", false) {
			break
		}
	}

	if err := client.SavePanels(out, cfgs); err != nil {
		return err
	}
	fmt.Fprintf(w, "\nWrote %d panel(s) to %s (0600)\n", len(cfgs), out)
	printSnippet(w, out)
	return nil
}

// verifyPanel checks the token against the panel with a cheap read.
func verifyPanel(cfg client.Config) error {
	c, err := client.New(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, status, err := c.Do(ctx, http.MethodGet, "/domains", nil)
	if err != nil {
		return fmt.Errorf("could not reach the panel: %v", err)
	}
	switch {
	case status == 401 || status == 403:
		return fmt.Errorf("panel rejected the token (HTTP %d)", status)
	case status >= 400:
		return fmt.Errorf("panel returned HTTP %d", status)
	}
	return nil
}

func printSnippet(w io.Writer, path string) {
	fmt.Fprintln(w, "\nRegister it with your MCP client, e.g. Claude Code:")
	fmt.Fprintf(w, "  claude mcp add jabali --env JABALI_PANELS_FILE=%s -- jabali-mcp\n", path)
	if path == client.DefaultConfigPath() {
		fmt.Fprintln(w, "\n(That path is the default, so `-- jabali-mcp` with no env also works.)")
	}
	fmt.Fprintln(w, "In a fleet, each tool accepts a \"panel\" argument to pick one; omit it for the first.")
}

// --- prompt helpers (prompt to w, read a line from r) ---

func promptDefault(r *bufio.Reader, w io.Writer, label, def string) string {
	fmt.Fprintf(w, "%s [%s]: ", label, def)
	s := readLine(r)
	if s == "" {
		return def
	}
	return s
}

func promptRequired(r *bufio.Reader, w io.Writer, label string) string {
	for {
		fmt.Fprintf(w, "%s: ", label)
		if s := readLine(r); s != "" {
			return s
		}
		fmt.Fprintln(w, "  (required)")
	}
}

func promptYesNo(r *bufio.Reader, w io.Writer, label string, def bool) bool {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	fmt.Fprintf(w, "%s [%s]: ", label, hint)
	s := strings.ToLower(readLine(r))
	if s == "" {
		return def
	}
	return s == "y" || s == "yes"
}

func readLine(r *bufio.Reader) string {
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	return strings.TrimSpace(line)
}

func defaultName(i int, seen map[string]bool) string {
	if i == 1 && !seen["default"] {
		return "default"
	}
	return fmt.Sprintf("panel-%d", i)
}
