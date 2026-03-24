package topology

import (
	"testing"

	"github.com/yanjiulab/netnslab/internal/config"
)

func TestHostDefaultGatewaysBridge(t *testing.T) {
	cfg := &config.Config{
		Topology: config.Topology{
			Nodes: map[string]*config.Node{
				"h1":  {Kind: "host"},
				"r1":  {Kind: "router"},
				"br1": {Kind: "bridge"},
			},
			Links: []*config.Link{
				{Endpoints: []string{"h1:eth1", "br1:eth1"}},
				{Endpoints: []string{"r1:eth2", "br1:eth2"}},
			},
		},
	}
	if _, err := Build(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Topology.Nodes["r1"].Interfaces["eth2"].IP = "10.0.0.1/24"
	cfg.Topology.Nodes["h1"].Interfaces["eth1"].IP = "10.0.0.2/24"

	gw := HostDefaultGateways(cfg)
	if gw["h1"] != "10.0.0.1" {
		t.Fatalf("want h1 gw 10.0.0.1, got %q", gw["h1"])
	}
}

func TestHostDefaultGatewaysDirect(t *testing.T) {
	cfg := &config.Config{
		Topology: config.Topology{
			Nodes: map[string]*config.Node{
				"h1": {Kind: "host"},
				"r1": {Kind: "router"},
			},
			Links: []*config.Link{
				{Endpoints: []string{"h1:eth1", "r1:eth1"}},
			},
		},
	}
	if _, err := Build(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Topology.Nodes["r1"].Interfaces["eth1"].IP = "10.1.0.1/30"
	cfg.Topology.Nodes["h1"].Interfaces["eth1"].IP = "10.1.0.2/30"

	gw := HostDefaultGateways(cfg)
	if gw["h1"] != "10.1.0.1" {
		t.Fatalf("want h1 gw 10.1.0.1, got %q", gw["h1"])
	}
}
