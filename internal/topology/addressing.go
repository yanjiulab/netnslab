package topology

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"

	"github.com/yourname/netnslab/internal/config"
)

// assignInterfaces ensures every link endpoint has an interface name and
// wires basic Interface runtime information on nodes.
func assignInterfaces(cfg *config.Config) error {
	// Per-node counter for automatically generated interface names.
	ifCounters := make(map[string]int)

	for _, link := range cfg.Topology.Links {
		if len(link.Endpoints) != 2 {
			return fmt.Errorf("link: expected 2 endpoints, got %d", len(link.Endpoints))
		}

		nodeA, ifA := config.SplitEndpointPublic(link.Endpoints[0])
		nodeB, ifB := config.SplitEndpointPublic(link.Endpoints[1])

		if ifA == "" {
			ifCounters[nodeA]++
			ifA = fmt.Sprintf("eth%d", ifCounters[nodeA])
		}
		if ifB == "" {
			ifCounters[nodeB]++
			ifB = fmt.Sprintf("eth%d", ifCounters[nodeB])
		}

		link.Endpoints[0] = nodeA + ":" + ifA
		link.Endpoints[1] = nodeB + ":" + ifB

		aNode := cfg.Topology.Nodes[nodeA]
		bNode := cfg.Topology.Nodes[nodeB]

		if aNode.Interfaces == nil {
			aNode.Interfaces = make(map[string]*config.Interface)
		}
		if bNode.Interfaces == nil {
			bNode.Interfaces = make(map[string]*config.Interface)
		}

		if aNode.Interfaces[ifA] == nil {
			aNode.Interfaces[ifA] = &config.Interface{
				Node:     nodeA,
				PeerNode: nodeB,
			}
		}
		if bNode.Interfaces[ifB] == nil {
			bNode.Interfaces[ifB] = &config.Interface{
				Node:     nodeB,
				PeerNode: nodeA,
			}
		}
	}

	return nil
}

func isBridge(n *config.Node) bool {
	return n != nil && n.Kind == "bridge"
}

// ifaceRef identifies one endpoint on a link.
type ifaceRef struct {
	Node   string
	IfName string
}

func (r ifaceRef) key() string {
	return r.Node + "\x00" + r.IfName
}

// AllocateAddresses assigns IPv4 only where needed.
// - If a link has ipv4: [a, b] (two entries), no automatic allocation is done for that link;
//   values apply only to non-bridge endpoints (bridge ports stay L2-only).
// - Otherwise, a global plan runs: bridge LAN segments first, then direct host-host / router-router (P2P),
//   then direct host-router (LAN, router first usable, host next).
func AllocateAddresses(cfg *config.Config) error {
	p2pAlloc, err := newSubnetAllocator(cfg.Addressing.P2P, 30)
	if err != nil {
		return err
	}
	lanAlloc, err := newSubnetAllocator(cfg.Addressing.LAN, 24)
	if err != nil {
		return err
	}

	for li, link := range cfg.Topology.Links {
		if len(link.Endpoints) != 2 {
			return fmt.Errorf("topology.links[%d]: expected 2 endpoints, got %d", li, len(link.Endpoints))
		}
		if len(link.IPv4) != 0 && len(link.IPv4) != 2 {
			return fmt.Errorf("topology.links[%d]: ipv4 must be empty or have exactly 2 entries", li)
		}
	}

	// --- Manual links (user specified IPs): no auto allocation for these links.
	for li, link := range cfg.Topology.Links {
		if len(link.IPv4) != 2 {
			continue
		}
		if err := allocateManualPlanned(link, cfg); err != nil {
			return fmt.Errorf("topology.links[%d]: %w", li, err)
		}
	}

	// --- Auto: group by bridge (one LAN subnet per bridge, non-bridge ports only).
	if err := allocateAutoBridgeSegments(cfg, lanAlloc); err != nil {
		return err
	}

	// --- Auto: direct links (no bridge endpoint), both ends still without L3 address.
	if err := allocateAutoDirectLinks(cfg, p2pAlloc, lanAlloc); err != nil {
		return err
	}

	setPeerIPsForL3Links(cfg)
	return validateNoDuplicateSubnetPerNode(cfg)
}

// allocateManualPlanned applies user-provided addresses without assigning IPs to bridge ports.
func allocateManualPlanned(link *config.Link, cfg *config.Config) error {
	var refs []ifaceRef
	var cidrs []string
	for i := 0; i < 2; i++ {
		nodeName, ifName := config.SplitEndpointPublic(link.Endpoints[i])
		n := cfg.Topology.Nodes[nodeName]
		if isBridge(n) {
			cfg.Topology.Nodes[nodeName].Interfaces[ifName].IP = ""
			cfg.Topology.Nodes[nodeName].Interfaces[ifName].PeerIP = ""
			continue
		}
		refs = append(refs, ifaceRef{Node: nodeName, IfName: ifName})
		cidrs = append(cidrs, link.IPv4[i])
	}

	if len(refs) == 0 {
		// bridge-bridge: pure L2
		link.Subnet = ""
		return nil
	}

	var nets []*net.IPNet
	for i, cidr := range cidrs {
		ip, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("invalid ipv4 for %s:%s %q: %w", refs[i].Node, refs[i].IfName, cidr, err)
		}
		ip = ip.To4()
		if ip == nil {
			return fmt.Errorf("only IPv4 supported for %s:%s", refs[i].Node, refs[i].IfName)
		}
		nets = append(nets, ipnet)
	}

	// Single L3 endpoint: accept one CIDR; subnet is that network.
	if len(refs) == 1 {
		ip, _, err := net.ParseCIDR(cidrs[0])
		if err != nil {
			return err
		}
		ip = ip.To4()
		ones, _ := nets[0].Mask.Size()
		mask := net.CIDRMask(ones, 32)
		cfg.Topology.Nodes[refs[0].Node].Interfaces[refs[0].IfName].IP = formatCIDR(ip, mask)
		link.Subnet = nets[0].String()
		return nil
	}

	// Two L3 endpoints: same mask and same subnet.
	onesA, bitsA := nets[0].Mask.Size()
	onesB, bitsB := nets[1].Mask.Size()
	if onesA != onesB || bitsA != bitsB {
		return fmt.Errorf("endpoints must use the same netmask (got /%d and /%d)", onesA, onesB)
	}
	if nets[0].String() != nets[1].String() {
		return fmt.Errorf("addresses are not in the same IPv4 subnet (%s vs %s)", nets[0].String(), nets[1].String())
	}
	ipA, _, _ := net.ParseCIDR(cidrs[0])
	ipB, _, _ := net.ParseCIDR(cidrs[1])
	ipA = ipA.To4()
	ipB = ipB.To4()
	if !nets[0].Contains(ipB) || !nets[1].Contains(ipA) {
		return fmt.Errorf("addresses do not belong to the same L3 subnet")
	}

	link.Subnet = nets[0].String()
	mask := net.CIDRMask(onesA, bitsA)
	cfg.Topology.Nodes[refs[0].Node].Interfaces[refs[0].IfName].IP = formatCIDR(ipA, mask)
	cfg.Topology.Nodes[refs[1].Node].Interfaces[refs[1].IfName].IP = formatCIDR(ipB, mask)
	return nil
}

// allocateAutoBridgeSegments: all ports attached to the same bridge share one /24 from LAN;
// routers (sorted by name, then ifname) get the lowest usable hosts, then hosts in order.
func allocateAutoBridgeSegments(cfg *config.Config, lanAlloc *subnetAllocator) error {
	// bridgeName -> list of non-bridge iface refs (dedup)
	segments := make(map[string][]ifaceRef)

	for _, link := range cfg.Topology.Links {
		if len(link.IPv4) != 0 {
			continue
		}
		n0, i0 := config.SplitEndpointPublic(link.Endpoints[0])
		n1, i1 := config.SplitEndpointPublic(link.Endpoints[1])
		node0 := cfg.Topology.Nodes[n0]
		node1 := cfg.Topology.Nodes[n1]
		b0 := isBridge(node0)
		b1 := isBridge(node1)
		if b0 && b1 {
			link.Subnet = ""
			continue
		}
		if !b0 && !b1 {
			continue
		}
		if lanAlloc == nil {
			return fmt.Errorf("addressing.lan is required for links to a bridge (link %s-%s)", n0, n1)
		}
		var brName string
		var ep ifaceRef
		if b0 {
			brName = n0
			ep = ifaceRef{Node: n1, IfName: i1}
		} else {
			brName = n1
			ep = ifaceRef{Node: n0, IfName: i0}
		}
		segments[brName] = append(segments[brName], ep)
	}

	for brName, eps := range segments {
		uniq := dedupeIfaceRefs(eps)
		if len(uniq) == 0 {
			continue
		}
		sortBridgeLANEndpoints(cfg, uniq)

		_, subnet, err := lanAlloc.Next()
		if err != nil {
			return fmt.Errorf("bridge %s: allocate LAN subnet: %w", brName, err)
		}
		subnetStr := subnet.String()

		hostBits := hostCapacity(subnet)
		if len(uniq) > hostBits {
			return fmt.Errorf("bridge %s: need %d host addresses in %s but only %d usable", brName, len(uniq), subnetStr, hostBits)
		}

		for idx, ref := range uniq {
			ip, err := nthUsableHost(subnet, idx)
			if err != nil {
				return fmt.Errorf("bridge %s: %w", brName, err)
			}
			ones, _ := subnet.Mask.Size()
			cidr := formatCIDR(ip, net.CIDRMask(ones, 32))
			cfg.Topology.Nodes[ref.Node].Interfaces[ref.IfName].IP = cidr
		}

		// Set link.Subnet on every auto bridge incident link for this bridge.
		for _, link := range cfg.Topology.Links {
			if len(link.IPv4) != 0 {
				continue
			}
			n0, _ := config.SplitEndpointPublic(link.Endpoints[0])
			n1, _ := config.SplitEndpointPublic(link.Endpoints[1])
			if n0 == brName || n1 == brName {
				link.Subnet = subnetStr
			}
		}
	}

	return nil
}

func dedupeIfaceRefs(in []ifaceRef) []ifaceRef {
	seen := make(map[string]struct{})
	var out []ifaceRef
	for _, r := range in {
		k := r.key()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, r)
	}
	return out
}

// sortBridgeLANEndpoints: routers first (by node, ifname), then hosts, then other kinds.
func sortBridgeLANEndpoints(cfg *config.Config, refs []ifaceRef) {
	sort.SliceStable(refs, func(i, j int) bool {
		pi := kindPriority(cfg.Topology.Nodes[refs[i].Node].Kind)
		pj := kindPriority(cfg.Topology.Nodes[refs[j].Node].Kind)
		if pi != pj {
			return pi < pj
		}
		if refs[i].Node != refs[j].Node {
			return refs[i].Node < refs[j].Node
		}
		return refs[i].IfName < refs[j].IfName
	})
}

func kindPriority(kind string) int {
	switch kind {
	case "router":
		return 0
	case "host":
		return 1
	default:
		return 2
	}
}

// allocateAutoDirectLinks: host-host and router-router -> P2P /30; host-router -> LAN /24 (router first).
func allocateAutoDirectLinks(cfg *config.Config, p2pAlloc, lanAlloc *subnetAllocator) error {
	for li, link := range cfg.Topology.Links {
		if len(link.IPv4) != 0 {
			continue
		}
		n0, i0 := config.SplitEndpointPublic(link.Endpoints[0])
		n1, i1 := config.SplitEndpointPublic(link.Endpoints[1])
		node0 := cfg.Topology.Nodes[n0]
		node1 := cfg.Topology.Nodes[n1]

		if isBridge(node0) || isBridge(node1) {
			continue
		}

		if ifaceHasIP(cfg, n0, i0) || ifaceHasIP(cfg, n1, i1) {
			continue
		}

		k0, k1 := node0.Kind, node1.Kind

		// host-host or router-router -> P2P pool
		if (k0 == "host" && k1 == "host") || (k0 == "router" && k1 == "router") {
			if p2pAlloc == nil {
				return fmt.Errorf("topology.links[%d]: addressing.p2p is required for %s-%s link", li, k0, k1)
			}
			_, subnet, err := p2pAlloc.Next()
			if err != nil {
				return fmt.Errorf("topology.links[%d]: %w", li, err)
			}
			link.Subnet = subnet.String()
			a, b, err := firstTwoHosts(subnet)
			if err != nil {
				return fmt.Errorf("topology.links[%d]: %w", li, err)
			}
			// Stable order: lexicographic by node name
			first, second := ifaceRef{n0, i0}, ifaceRef{n1, i1}
			if n1 < n0 {
				first, second = second, first
				a, b = b, a
			}
			cfg.Topology.Nodes[first.Node].Interfaces[first.IfName].IP = a
			cfg.Topology.Nodes[second.Node].Interfaces[second.IfName].IP = b
			continue
		}

		// host-router (either order) -> single LAN /24, router gets first usable, host second
		if (k0 == "router" && k1 == "host") || (k0 == "host" && k1 == "router") {
			if lanAlloc == nil {
				return fmt.Errorf("topology.links[%d]: addressing.lan is required for host-router link", li)
			}
			_, subnet, err := lanAlloc.Next()
			if err != nil {
				return fmt.Errorf("topology.links[%d]: %w", li, err)
			}
			link.Subnet = subnet.String()
			ipRouter, err := nthUsableHost(subnet, 0)
			if err != nil {
				return fmt.Errorf("topology.links[%d]: %w", li, err)
			}
			ipHost, err := nthUsableHost(subnet, 1)
			if err != nil {
				return fmt.Errorf("topology.links[%d]: %w", li, err)
			}
			ones, _ := subnet.Mask.Size()
			mask := net.CIDRMask(ones, 32)
			var refR, refH ifaceRef
			if k0 == "router" {
				refR, refH = ifaceRef{n0, i0}, ifaceRef{n1, i1}
			} else {
				refR, refH = ifaceRef{n1, i1}, ifaceRef{n0, i0}
			}
			cfg.Topology.Nodes[refR.Node].Interfaces[refR.IfName].IP = formatCIDR(ipRouter, mask)
			cfg.Topology.Nodes[refH.Node].Interfaces[refH.IfName].IP = formatCIDR(ipHost, mask)
			continue
		}

		return fmt.Errorf("topology.links[%d]: unsupported auto addressing for %s-%s (no bridge)", li, k0, k1)
	}

	return nil
}

func ifaceHasIP(cfg *config.Config, node, ifName string) bool {
	if n := cfg.Topology.Nodes[node]; n != nil && n.Interfaces != nil {
		if iface := n.Interfaces[ifName]; iface != nil && iface.IP != "" {
			return true
		}
	}
	return false
}

// setPeerIPsForL3Links sets PeerIP only when both ends have L3 addresses (no bridge).
func setPeerIPsForL3Links(cfg *config.Config) {
	for _, link := range cfg.Topology.Links {
		n0, i0 := config.SplitEndpointPublic(link.Endpoints[0])
		n1, i1 := config.SplitEndpointPublic(link.Endpoints[1])
		node0 := cfg.Topology.Nodes[n0]
		node1 := cfg.Topology.Nodes[n1]
		if isBridge(node0) || isBridge(node1) {
			if !isBridge(node0) {
				node0.Interfaces[i0].PeerIP = ""
			}
			if !isBridge(node1) {
				node1.Interfaces[i1].PeerIP = ""
			}
			continue
		}
		ip0 := node0.Interfaces[i0].IP
		ip1 := node1.Interfaces[i1].IP
		if ip0 != "" && ip1 != "" {
			node0.Interfaces[i0].PeerIP = ip1
			node1.Interfaces[i1].PeerIP = ip0
		}
	}
}

func formatCIDR(ip net.IP, mask net.IPMask) string {
	ones, _ := mask.Size()
	return fmt.Sprintf("%s/%d", ip.To4().String(), ones)
}

func validateNoDuplicateSubnetPerNode(cfg *config.Config) error {
	for nodeName, n := range cfg.Topology.Nodes {
		seen := make(map[string]string)
		for ifName, iface := range n.Interfaces {
			if iface.IP == "" {
				continue
			}
			_, ipnet, err := net.ParseCIDR(iface.IP)
			if err != nil {
				return fmt.Errorf("node %s interface %s: invalid IP %q: %w", nodeName, ifName, iface.IP, err)
			}
			key := ipnet.String()
			if prev, ok := seen[key]; ok {
				return fmt.Errorf("node %s: interfaces %s and %s are in the same subnet %s", nodeName, prev, ifName, key)
			}
			seen[key] = ifName
		}
	}
	return nil
}

// subnetAllocator linearly allocates subnets of a given prefix from a pool.
type subnetAllocator struct {
	base   *net.IPNet
	prefix int
	next   uint64
}

func newSubnetAllocator(cidr string, prefix int) (*subnetAllocator, error) {
	if cidr == "" {
		return nil, nil
	}
	_, base, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("parse CIDR %q: %w", cidr, err)
	}
	return &subnetAllocator{
		base:   base,
		prefix: prefix,
		next:   0,
	}, nil
}

// Next returns the next non-overlapping child subnet inside the pool.
// P2P uses prefix 30 (4 addresses per slice); LAN uses prefix 24 (256 addresses per slice).
// Slices are laid out contiguously from the pool network address without gaps or overlap.
func (a *subnetAllocator) Next() (net.IP, *net.IPNet, error) {
	if a == nil {
		return nil, nil, fmt.Errorf("subnet allocator is nil")
	}

	parentOnes, bits := a.base.Mask.Size()
	if bits != 32 {
		return nil, nil, fmt.Errorf("only IPv4 pools are supported")
	}
	if a.prefix < parentOnes || a.prefix > bits {
		return nil, nil, fmt.Errorf("child prefix /%d invalid for pool %s (pool is /%d)", a.prefix, a.base.String(), parentOnes)
	}

	poolNW := a.base.IP.Mask(a.base.Mask).To4()
	if poolNW == nil {
		return nil, nil, fmt.Errorf("invalid pool network")
	}
	poolStart := binary.BigEndian.Uint32(poolNW)
	poolSpan := uint64(1) << uint(32-parentOnes)
	poolEnd := uint64(poolStart) + poolSpan // exclusive

	childBlock := uint64(1) << uint(32-a.prefix)
	if childBlock == 0 || poolSpan < childBlock {
		return nil, nil, fmt.Errorf("pool %s cannot hold a /%d subnet", a.base.String(), a.prefix)
	}

	maxSlots := poolSpan / childBlock
	if a.next >= maxSlots {
		return nil, nil, fmt.Errorf("address pool %s exhausted", a.base.String())
	}

	start := uint64(poolStart) + a.next*childBlock
	a.next++

	if start+childBlock > poolEnd {
		return nil, nil, fmt.Errorf("address pool %s exhausted (bounds)", a.base.String())
	}

	ip := make(net.IP, 4)
	binary.BigEndian.PutUint32(ip, uint32(start))
	subnet := &net.IPNet{
		IP:   ip,
		Mask: net.CIDRMask(a.prefix, 32),
	}
	return ip, subnet, nil
}

func firstTwoHosts(n *net.IPNet) (string, string, error) {
	ip0, err := nthUsableHost(n, 0)
	if err != nil {
		return "", "", err
	}
	ip1, err := nthUsableHost(n, 1)
	if err != nil {
		return "", "", err
	}
	ones, _ := n.Mask.Size()
	mask := net.CIDRMask(ones, 32)
	return formatCIDR(ip0, mask), formatCIDR(ip1, mask), nil
}

// nthUsableHost returns the n-th usable host address in the subnet (n=0 is first usable).
func nthUsableHost(ipnet *net.IPNet, n int) (net.IP, error) {
	ip := ipnet.IP.To4()
	if ip == nil {
		return nil, fmt.Errorf("only IPv4 subnets are supported")
	}
	ones, bits := ipnet.Mask.Size()
	nbits := bits - ones
	if nbits < 2 {
		return nil, fmt.Errorf("subnet too small for host addressing")
	}
	maxHosts := (1 << uint(nbits)) - 2
	if n < 0 || n >= maxHosts {
		return nil, fmt.Errorf("host index %d out of range (max %d)", n, maxHosts-1)
	}
	nw := ip.Mask(ipnet.Mask).To4()
	if nw == nil {
		return nil, fmt.Errorf("invalid network")
	}
	network := binary.BigEndian.Uint32(nw)
	host := network + uint32(n+1)
	out := make(net.IP, 4)
	binary.BigEndian.PutUint32(out, host)
	return out, nil
}

func hostCapacity(ipnet *net.IPNet) int {
	ones, bits := ipnet.Mask.Size()
	nbits := bits - ones
	if nbits < 2 {
		return 0
	}
	return (1 << uint(nbits)) - 2
}
