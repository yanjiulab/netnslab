package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateNetem(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "lab.yaml")
	content := `name: t
topology:
  nodes:
    a: { kind: host }
    b: { kind: host }
  links:
    - endpoints: ["a:eth1", "b:eth1"]
      netem:
        delay_ms: 10
        loss_percent: 1
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err != nil {
		t.Fatal(err)
	}
}

func TestValidateNetemEmptyRejected(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "lab.yaml")
	content := `name: t
topology:
  nodes:
    a: { kind: host }
    b: { kind: host }
  links:
    - endpoints: ["a:eth1", "b:eth1"]
      netem: {}
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err == nil {
		t.Fatal("expected error for empty netem")
	}
}
