package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
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
// `go install …@<ref>`, the standard self-update path for a Go tool.
func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	ref := fs.String("ref", "latest", "version to install: latest, a tag (v1.2.3), or a commit")
	dry := fs.Bool("dry-run", false, "print the command instead of running it")
	check := fs.Bool("check", false, "only report the latest released version, do not install")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *check {
		return runCheckIO(os.Stdout, execRunner)
	}
	return runUpdateIO(*ref, *dry, os.Stdout, os.Stderr, execRunner)
}

// runCheckIO reports the newest release tag without installing, via
// `git ls-remote --tags` over anonymous HTTPS (the repo is public).
func runCheckIO(stdout io.Writer, run runner) error {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return fmt.Errorf("`git` is not on PATH")
	}
	var buf strings.Builder
	argv := []string{gitBin, "ls-remote", "--tags", "--refs", "https://github.com/shukiv/jabali-mcp.git", "v*"}
	if err := run(argv, os.Environ(), &buf, io.Discard); err != nil {
		return fmt.Errorf("list release tags: %w", err)
	}
	latest := latestTag(buf.String())
	if latest == "" {
		return fmt.Errorf("no release tags found")
	}
	// The version default has no "v" prefix while tags do; compare normalized.
	if strings.TrimPrefix(latest, "v") == strings.TrimPrefix(version, "v") {
		fmt.Fprintf(stdout, "jabali-mcp %s is up to date\n", version)
	} else {
		fmt.Fprintf(stdout, "current %s, latest %s — run `jabali-mcp update` to install\n", version, latest)
	}
	return nil
}

// latestTag picks the highest semver-ish vX.Y.Z tag from ls-remote output.
func latestTag(lsRemote string) string {
	var tags []string
	for _, line := range strings.Split(lsRemote, "\n") {
		if i := strings.LastIndex(line, "refs/tags/"); i >= 0 {
			if t := strings.TrimSpace(line[i+len("refs/tags/"):]); strings.HasPrefix(t, "v") {
				tags = append(tags, t)
			}
		}
	}
	if len(tags) == 0 {
		return ""
	}
	sort.Slice(tags, func(i, j int) bool { return semverLess(tags[i], tags[j]) })
	return tags[len(tags)-1]
}

// semverLess compares vX.Y.Z tags numerically, falling back to string order
// for malformed parts.
func semverLess(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		var na, nb int
		if _, err := fmt.Sscanf(pa[i], "%d", &na); err != nil {
			return a < b
		}
		if _, err := fmt.Sscanf(pb[i], "%d", &nb); err != nil {
			return a < b
		}
		if na != nb {
			return na < nb
		}
	}
	return len(pa) < len(pb)
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

	if dry {
		fmt.Fprintln(stdout, strings.Join(argv, " "))
		return nil
	}

	fmt.Fprintf(stderr, "jabali-mcp %s → installing %s …\n", version, target)
	if err := run(argv, os.Environ(), stdout, stderr); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}
	fmt.Fprintln(stderr, "done — the new binary is in your Go bin (`go env GOBIN`, else `$(go env GOPATH)/bin`). "+
		"Restart your MCP client to pick it up.")
	return nil
}
