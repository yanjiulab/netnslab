package topology

import (
	"sort"
	"strings"

	"github.com/yanjiulab/netnslab/internal/config"
)

// HostDefaultGateways returns default gateway IPv4 addresses (no /mask) for each host.
// - On a bridge segment: first router on that bridge (by node name) is the gateway.
// - On a direct host–router link (no bridge): the router's address on that link.
// Routers are not included. If a host would get two gateways, the first rule wins.
func HostDefaultGateways(cfg *config.Config) map[string]string {
	out := make(map[string]string)

	// 1) Bridge-attached hosts: share the segment default router.
	bridgeNames := make([]string, 0)
	for name, n := range cfg.Topology.Nodes {
		if n.Kind == "bridge" {
			bridgeNames = append(bridgeNames, name)
		}
	}
	sort.Strings(bridgeNames)

	for _, br := range bridgeNames {
		eps := endpointsFacingBridge(cfg, br)
		type ep struct {
			node, ifName, kind, ip string
		}
		var routers, hosts []ep
		for _, e := range eps {
			n := cfg.Topology.Nodes[e.node]
			if n == nil {
				continue
			}
			ip := ""
			if n.Interfaces != nil && n.Interfaces[e.ifName] != nil {
				ip = strings.TrimSpace(n.Interfaces[e.ifName].IP)
			}
			if ip == "" {
				continue
			}
			switch n.Kind {
			case "router":
				routers = append(routers, ep{node: e.node, ifName: e.ifName, kind: n.Kind, ip: ip})
			case "host":
				hosts = append(hosts, ep{node: e.node, ifName: e.ifName, kind: n.Kind, ip: ip})
			}
		}
		if len(routers) == 0 || len(hosts) == 0 {
			continue
		}
		sort.Slice(routers, func(i, j int) bool { return routers[i].node < routers[j].node })
		gw := hostOnlyForRoute(routers[0].ip)
		if gw == "" {
			continue
		}
		for _, h := range hosts {
			if _, ok := out[h.node]; ok {
				continue
			}
			out[h.node] = gw
		}
	}

	// 2) Direct host–router links (no bridge on the link).
	for _, link := range cfg.Topology.Links {
		if len(link.Endpoints) != 2 {
			continue
		}
		n0, i0 := config.SplitEndpointPublic(link.Endpoints[0])
		n1, i1 := config.SplitEndpointPublic(link.Endpoints[1])
		node0 := cfg.Topology.Nodes[n0]
		node1 := cfg.Topology.Nodes[n1]
		if node0 == nil || node1 == nil {
			continue
		}
		if node0.Kind == "bridge" || node1.Kind == "bridge" {
			continue
		}

		k0, k1 := node0.Kind, node1.Kind
		var hostName, routerNode, routerIf string
		switch {
		case k0 == "host" && k1 == "router":
			hostName, routerNode, routerIf = n0, n1, i1
		case k0 == "router" && k1 == "host":
			hostName, routerNode, routerIf = n1, n0, i0
		default:
			continue
		}
		if _, ok := out[hostName]; ok {
			continue
		}
		rn := cfg.Topology.Nodes[routerNode]
		if rn == nil || rn.Interfaces == nil || rn.Interfaces[routerIf] == nil {
			continue
		}
		ip := strings.TrimSpace(rn.Interfaces[routerIf].IP)
		gw := hostOnlyForRoute(ip)
		if gw == "" {
			continue
		}
		out[hostName] = gw
	}

	return out
}

type bridgeFacing struct {
	node, ifName string
}

func endpointsFacingBridge(cfg *config.Config, bridgeName string) []bridgeFacing {
	var out []bridgeFacing
	seen := make(map[string]struct{})
	for _, link := range cfg.Topology.Links {
		if len(link.Endpoints) != 2 {
			continue
		}
		n0, i0 := config.SplitEndpointPublic(link.Endpoints[0])
		n1, i1 := config.SplitEndpointPublic(link.Endpoints[1])
		if n0 == bridgeName && n1 != bridgeName {
			k := n1 + "\x00" + i1
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				out = append(out, bridgeFacing{node: n1, ifName: i1})
			}
		}
		if n1 == bridgeName && n0 != bridgeName {
			k := n0 + "\x00" + i0
			if _, ok := seen[k]; !ok {
				seen[k] = struct{}{}
				out = append(out, bridgeFacing{node: n0, ifName: i0})
			}
		}
	}
	return out
}

func hostOnlyForRoute(cidr string) string {
	cidr = strings.TrimSpace(cidr)
	if i := strings.Index(cidr, "/"); i >= 0 {
		return cidr[:i]
	}
	return cidr
}
