package routing

import (
	"net"

	"github.com/yourname/netnslab/internal/config"
)

// Graph holds router adjacency information and subnet ownership.
type Graph struct {
	Routers      map[string]*config.Node
	Adjacency    map[string][]string      // router -> neighboring routers
	SubnetOwners map[string][]string      // subnet CIDR -> routers that have it directly connected
	Subnets      map[string]*net.IPNet    // subnet CIDR -> parsed net
	RouterIfaces map[string]map[string]string // router -> subnet -> interface name
}

// BuildGraph builds a routing graph from the lab configuration.
func BuildGraph(cfg *config.Config) *Graph {
	g := &Graph{
		Routers:      make(map[string]*config.Node),
		Adjacency:    make(map[string][]string),
		SubnetOwners: make(map[string][]string),
		Subnets:      make(map[string]*net.IPNet),
		RouterIfaces: make(map[string]map[string]string),
	}

	for name, n := range cfg.Topology.Nodes {
		if n.Kind == "router" {
			g.Routers[name] = n
			g.RouterIfaces[name] = make(map[string]string)
		}
	}

	// Build adjacency between routers.
	for _, link := range cfg.Topology.Links {
		if len(link.Endpoints) != 2 {
			continue
		}
		r1, _ := config.SplitEndpointPublic(link.Endpoints[0])
		r2, _ := config.SplitEndpointPublic(link.Endpoints[1])
		n1, ok1 := g.Routers[r1]
		n2, ok2 := g.Routers[r2]
		if !ok1 || !ok2 {
			continue
		}
		g.Adjacency[r1] = appendIfMissing(g.Adjacency[r1], r2)
		g.Adjacency[r2] = appendIfMissing(g.Adjacency[r2], r1)

		// Record directly connected subnets from router interfaces.
		for ifName, iface := range n1.Interfaces {
			_, n, err := net.ParseCIDR(iface.IP)
			if err != nil {
				continue
			}
			cidr := n.String()
			g.Subnets[cidr] = n
			g.SubnetOwners[cidr] = appendIfMissing(g.SubnetOwners[cidr], r1)
			g.RouterIfaces[r1][cidr] = ifName
		}
		for ifName, iface := range n2.Interfaces {
			_, n, err := net.ParseCIDR(iface.IP)
			if err != nil {
				continue
			}
			cidr := n.String()
			g.Subnets[cidr] = n
			g.SubnetOwners[cidr] = appendIfMissing(g.SubnetOwners[cidr], r2)
			g.RouterIfaces[r2][cidr] = ifName
		}
	}

	return g
}

func appendIfMissing(slice []string, v string) []string {
	for _, s := range slice {
		if s == v {
			return slice
		}
	}
	return append(slice, v)
}

