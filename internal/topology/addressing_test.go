package topology

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/yourname/netnslab/internal/config"
)

func TestAssignInterfacesAndAllocateAddresses(t *testing.T) {
	cfg := &config.Config{
		Name: "lab1",
		Addressing: config.Addressing{
			P2P: "10.1.0.0/16",
			LAN: "10.2.0.0/16",
		},
		Topology: config.Topology{
			Nodes: map[string]*config.Node{
				"r1": {Kind: "router"},
				"r2": {Kind: "router"},
				"h1": {Kind: "host"},
			},
			Links: []*config.Link{
				{Endpoints: []string{"r1:eth1", "r2:eth1"}}, // router-router: P2P
				{Endpoints: []string{"h1", "r1"}},           // host-router: LAN
			},
		},
	}

	if _, err := Build(cfg); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if err := AllocateAddresses(cfg); err != nil {
		t.Fatalf("AllocateAddresses failed: %v", err)
	}

	if cfg.Topology.Nodes["r1"].Interfaces["eth1"].IP == "" {
		t.Fatalf("expected r1 eth1 to have an IP")
	}
	if cfg.Topology.Nodes["r2"].Interfaces["eth1"].IP == "" {
		t.Fatalf("expected r2 eth1 to have an IP")
	}
	if len(cfg.Topology.Nodes["h1"].Interfaces) == 0 {
		t.Fatalf("expected h1 to have at least one interface")
	}
}

func TestBridgePortHasNoIP(t *testing.T) {
	cfg := &config.Config{
		Name: "lab-br",
		Addressing: config.Addressing{
			LAN: "10.2.0.0/16",
		},
		Topology: config.Topology{
			Nodes: map[string]*config.Node{
				"h1":   {Kind: "host"},
				"br1":  {Kind: "bridge"},
				"r1":   {Kind: "router"},
			},
			Links: []*config.Link{
				{Endpoints: []string{"h1:eth1", "br1:eth1"}},
				{Endpoints: []string{"r1:eth1", "br1:eth2"}},
			},
		},
	}

	if _, err := Build(cfg); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if err := AllocateAddresses(cfg); err != nil {
		t.Fatalf("AllocateAddresses failed: %v", err)
	}

	for _, ifName := range []string{"eth1", "eth2"} {
		if cfg.Topology.Nodes["br1"].Interfaces[ifName].IP != "" {
			t.Fatalf("bridge br1 %s should have no IP, got %q", ifName, cfg.Topology.Nodes["br1"].Interfaces[ifName].IP)
		}
	}
	if cfg.Topology.Nodes["h1"].Interfaces["eth1"].IP == "" {
		t.Fatal("host h1 should have an IP")
	}
	if cfg.Topology.Nodes["r1"].Interfaces["eth1"].IP == "" {
		t.Fatal("router r1 should have an IP")
	}
}

func TestDuplicateSubnetPerNodeRejected(t *testing.T) {
	cfg := &config.Config{
		Name: "lab-dup",
		Topology: config.Topology{
			Nodes: map[string]*config.Node{
				"r1": {Kind: "router"},
			},
			Links: []*config.Link{
				{
					Endpoints: []string{"r1:eth1", "r1:eth2"},
					IPv4:      []string{"10.0.0.1/24", "10.0.0.2/24"},
				},
			},
		},
	}

	if _, err := Build(cfg); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if err := AllocateAddresses(cfg); err == nil {
		t.Fatal("expected error for two interfaces on same node in same subnet")
	}
}

func TestBridgeSegmentRouterGetsFirstLANHost(t *testing.T) {
	cfg := &config.Config{
		Name: "lab-order",
		Addressing: config.Addressing{
			LAN: "10.2.0.0/16",
		},
		Topology: config.Topology{
			Nodes: map[string]*config.Node{
				"h1":  {Kind: "host"},
				"br1": {Kind: "bridge"},
				"r1":  {Kind: "router"},
			},
			Links: []*config.Link{
				{Endpoints: []string{"h1:eth1", "br1:eth1"}},
				{Endpoints: []string{"r1:eth1", "br1:eth2"}},
			},
		},
	}

	if _, err := Build(cfg); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if err := AllocateAddresses(cfg); err != nil {
		t.Fatalf("AllocateAddresses failed: %v", err)
	}

	ipR := cfg.Topology.Nodes["r1"].Interfaces["eth1"].IP
	ipH := cfg.Topology.Nodes["h1"].Interfaces["eth1"].IP
	_, netR, err := net.ParseCIDR(ipR)
	if err != nil {
		t.Fatal(err)
	}
	_, netH, err := net.ParseCIDR(ipH)
	if err != nil {
		t.Fatal(err)
	}
	if netR.String() != netH.String() {
		t.Fatalf("h1 and r1 should share subnet: %s vs %s", ipR, ipH)
	}
	rIP, _, _ := net.ParseCIDR(ipR)
	hIP, _, _ := net.ParseCIDR(ipH)
	if rIP == nil || hIP == nil {
		t.Fatal("parse IP")
	}
	// Same /24 from pool: first usable .1 -> router, second .2 -> host
	if rIP.To4()[3] != 1 || hIP.To4()[3] != 2 {
		t.Fatalf("expected router first host (.1) and host second (.2), got r1=%v h1=%v", rIP, hIP)
	}
}

func TestSubnetAllocatorNoOverlap(t *testing.T) {
	p2p, err := newSubnetAllocator("10.1.0.0/16", 30)
	if err != nil || p2p == nil {
		t.Fatalf("p2p allocator: %v", err)
	}
	lan, err := newSubnetAllocator("10.2.0.0/16", 24)
	if err != nil || lan == nil {
		t.Fatalf("lan allocator: %v", err)
	}

	var ranges [][2]uint32 // [start, end) exclusive end
	for i := 0; i < 100; i++ {
		_, sn, err := p2p.Next()
		if err != nil {
			t.Fatalf("p2p Next %d: %v", i, err)
		}
		start, end := subnetRange(sn)
		ranges = append(ranges, [2]uint32{start, end})
	}
	for i := 0; i < 50; i++ {
		_, sn, err := lan.Next()
		if err != nil {
			t.Fatalf("lan Next %d: %v", i, err)
		}
		start, end := subnetRange(sn)
		ranges = append(ranges, [2]uint32{start, end})
	}
	for i := 0; i < len(ranges); i++ {
		for j := i + 1; j < len(ranges); j++ {
			if rangesOverlap(ranges[i], ranges[j]) {
				t.Fatalf("overlap between range %d %x-%x and %d %x-%x", i, ranges[i][0], ranges[i][1], j, ranges[j][0], ranges[j][1])
			}
		}
	}
}

func subnetRange(sn *net.IPNet) (start, end uint32) {
	ones, bits := sn.Mask.Size()
	if bits != 32 {
		return 0, 0
	}
	nw := sn.IP.Mask(sn.Mask).To4()
	start = binary.BigEndian.Uint32(nw)
	block := uint32(1) << uint(32-ones)
	end = start + block
	return start, end
}

func rangesOverlap(a, b [2]uint32) bool {
	return a[0] < b[1] && b[0] < a[1]
}

