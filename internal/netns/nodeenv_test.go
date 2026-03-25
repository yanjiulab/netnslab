package netns

import (
	"slices"
	"testing"
)

func TestMergeEnviron(t *testing.T) {
	base := []string{"PATH=/usr/bin", "HOME=/root", "FOO=host"}
	over := map[string]string{"FOO": "node", "BAR": "1"}
	got := MergeEnviron(base, over)
	if !slices.Contains(got, "FOO=node") {
		t.Fatalf("expected FOO overridden to node, got %v", got)
	}
	if !slices.Contains(got, "BAR=1") {
		t.Fatalf("expected BAR=1, got %v", got)
	}
	if !slices.Contains(got, "PATH=/usr/bin") {
		t.Fatalf("expected PATH preserved, got %v", got)
	}
	for _, e := range got {
		if e == "FOO=host" {
			t.Fatalf("old FOO should be removed, got %v", got)
		}
	}
}
