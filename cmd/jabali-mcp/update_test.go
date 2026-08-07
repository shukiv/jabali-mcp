package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestUpdateDryRunBuildsGoInstall(t *testing.T) {
	var out bytes.Buffer
	ran := false
	run := func(_, _ []string, _, _ io.Writer) error { ran = true; return nil }

	if err := runUpdateIO("latest", true /*dry*/, &out, io.Discard, run); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if ran {
		t.Error("dry-run must not execute the command")
	}
	got := out.String()
	if !strings.Contains(got, "go install github.com/shukiv/jabali-mcp/cmd/jabali-mcp@latest") ||
		!strings.Contains(got, "GOPRIVATE=github.com/shukiv/*") {
		t.Errorf("unexpected dry-run output: %q", got)
	}
}

func TestUpdatePinsRef(t *testing.T) {
	var out bytes.Buffer
	if err := runUpdateIO("v1.2.3", true, &out, io.Discard, nil); err != nil {
		t.Fatalf("pin ref: %v", err)
	}
	if !strings.Contains(out.String(), "cmd/jabali-mcp@v1.2.3") {
		t.Errorf("expected the pinned ref in the command, got %q", out.String())
	}
}

func TestUpdateRejectsUnsafeRef(t *testing.T) {
	for _, bad := range []string{"", "latest extra", "a@b", "foo\tbar"} {
		if err := runUpdateIO(bad, true, io.Discard, io.Discard, nil); err == nil {
			t.Errorf("ref %q should be rejected", bad)
		}
	}
}
