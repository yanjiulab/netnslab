package config

import (
	"strings"
	"testing"
)

func TestValidateNodeKind(t *testing.T) {
	cfg := &Config{
		Name: "t",
		Topology: Topology{
			Nodes: map[string]*Node{
				"a": {Kind: "host"},
				"b": {Kind: "switch"},
			},
			Links: []*Link{{Endpoints: []string{"a:eth1", "b:eth1"}}},
		},
	}
	err := validate(cfg)
	if err == nil {
		t.Fatal("expected error for invalid kind")
	}
	if !strings.Contains(err.Error(), "switch") {
		t.Fatalf("error should mention invalid kind: %v", err)
	}
}

func TestValidateAddressingOverlap(t *testing.T) {
	cfg := &Config{
		Name: "t",
		Addressing: Addressing{
			P2P: "10.0.0.0/8",
			LAN: "10.1.0.0/16",
		},
		Topology: Topology{
			Nodes: map[string]*Node{
				"a": {Kind: "host"},
				"b": {Kind: "host"},
			},
			Links: []*Link{{Endpoints: []string{"a:eth1", "b:eth1"}}},
		},
	}
	err := validate(cfg)
	if err == nil {
		t.Fatal("expected overlap error")
	}
	if !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("expected overlap in error: %v", err)
	}
}

func TestValidateEndpointRequiresIfaceName(t *testing.T) {
	cfg := &Config{
		Name: "t",
		Topology: Topology{
			Nodes: map[string]*Node{
				"a": {Kind: "host"},
				"b": {Kind: "host"},
			},
			Links: []*Link{{Endpoints: []string{"a", "b:eth1"}}},
		},
	}
	err := validate(cfg)
	if err == nil {
		t.Fatal("expected missing interface validation error")
	}
	if !strings.Contains(err.Error(), "interface name") {
		t.Fatalf("expected interface-name error, got: %v", err)
	}
}

func TestValidateDuplicateInterfaceOnSameNode(t *testing.T) {
	cfg := &Config{
		Name: "t",
		Topology: Topology{
			Nodes: map[string]*Node{
				"a": {Kind: "host"},
				"b": {Kind: "host"},
				"c": {Kind: "host"},
			},
			Links: []*Link{
				{Endpoints: []string{"a:eth1", "b:eth1"}},
				{Endpoints: []string{"a:eth1", "c:eth1"}},
			},
		},
	}
	err := validate(cfg)
	if err == nil {
		t.Fatal("expected duplicate interface validation error")
	}
	if !strings.Contains(err.Error(), "duplicate interface") {
		t.Fatalf("expected duplicate interface error, got: %v", err)
	}
}
