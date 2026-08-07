package main

import "testing"

func TestLatestTag(t *testing.T) {
	lsRemote := "" +
		"abc\trefs/tags/v0.1.0\n" +
		"def\trefs/tags/v0.10.2\n" +
		"ghi\trefs/tags/v0.2.0\n" +
		"jkl\trefs/tags/v0.9.9\n"
	if got := latestTag(lsRemote); got != "v0.10.2" {
		t.Errorf("latestTag: want v0.10.2 (numeric compare, not lexicographic), got %q", got)
	}
	if got := latestTag(""); got != "" {
		t.Errorf("latestTag on empty input: want empty, got %q", got)
	}
}

func TestSemverLess(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "v0.10.0", true},
		{"v0.10.0", "v0.2.0", false},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0", "v1.0.1", true},
	}
	for _, c := range cases {
		if got := semverLess(c.a, c.b); got != c.want {
			t.Errorf("semverLess(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
