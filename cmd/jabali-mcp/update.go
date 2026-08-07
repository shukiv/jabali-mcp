package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// modulePath is this module's import path; `update` reinstalls its command.
const modulePath = "github.com/shukiv/jabali-mcp"

// runner runs an external command with an environment, streaming its output.
type runner func(argv, env []string, stdout, stderr io.Writer) error

func execRunner(argv, env []string, stdout, stderr io.Writer) error {
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // argv is built from constants + a validated ref
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// runUpdate is `jabali-mcp update`: reinstall the binary from source via
// `go install …@<ref>`, the standard self-update path for a Go tool. (The repo
// is private, so GOPRIVATE is set for the child; the user needs Go + git access.)
func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	ref := fs.String("ref", "latest", "version to install: latest, a tag (v1.2.3), or a commit")
	dry := fs.Bool("dry-run", false, "print the command instead of running it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runUpdateIO(*ref, *dry, os.Stdout, os.Stderr, execRunner)
}

func runUpdateIO(ref string, dry bool, stdout, stderr io.Writer, run runner) error {
	// Reject anything that isn't a plain version/ref token so it can't smuggle
	// extra go-install arguments.
	if ref == "" || strings.ContainsAny(ref, " \t\n@") {
		return fmt.Errorf("invalid --ref %q", ref)
	}
	target := modulePath + "/cmd/jabali-mcp@" + ref

	goBin, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("`go` is not on PATH — install Go 1.25+, or update from a source checkout with `git pull && make build`")
	}
	argv := []string{goBin, "install", target}
	env := append(os.Environ(), "GOPRIVATE=github.com/shukiv/*")

	if dry {
		fmt.Fprintf(stdout, "GOPRIVATE=github.com/shukiv/* %s\n", strings.Join(argv, " "))
		return nil
	}

	fmt.Fprintf(stderr, "jabali-mcp %s → installing %s …\n", version, target)
	if err := run(argv, env, stdout, stderr); err != nil {
		return fmt.Errorf("go install failed: %w\n"+
			"  (private repo — ensure git can authenticate to github.com/shukiv, e.g. SSH or a token)", err)
	}
	fmt.Fprintln(stderr, "done — the new binary is in your Go bin (`go env GOBIN`, else `$(go env GOPATH)/bin`). "+
		"Restart your MCP client to pick it up.")
	return nil
}
