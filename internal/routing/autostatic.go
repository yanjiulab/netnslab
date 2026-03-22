package routing

import (
	"fmt"

	"github.com/yourname/netnslab/internal/config"
)

// Route represents a static route to be installed on a router.
type Route struct {
	Destination string
	NextHop     string
	OutIface    string
}

// ComputeStaticRoutes implements a BFS-based static routing algorithm over router nodes.
// It returns routes per router name.
func ComputeStaticRoutes(cfg *config.Config) (map[string][]Route, error) {
	g := BuildGraph(cfg)
	if len(g.Routers) == 0 {
		return map[string][]Route{}, nil
	}

	routes := make(map[string][]Route)

	// Pre-collect all subnets.
	allSubnets := make([]string, 0, len(g.Subnets))
	for cidr := range g.Subnets {
		allSubnets = append(allSubnets, cidr)
	}

	for routerName := range g.Routers {
		routes[routerName] = []Route{}

		// Directly connected subnets for this router.
		direct := g.RouterIfaces[routerName]

		for _, cidr := range allSubnets {
			// Skip directly connected subnets.
			if _, ok := direct[cidr]; ok {
				continue
			}

			owners := g.SubnetOwners[cidr]
			if len(owners) == 0 {
				continue
			}
			destRouter := owners[0]
			if destRouter == routerName {
				continue
			}

			nextHopRouter, err := bfsNextHop(g, routerName, destRouter)
			if err != nil {
				return nil, fmt.Errorf("compute route from %s to %s (%s): %w", routerName, destRouter, cidr, err)
			}

			nextHopIP, outIface, err := findNextHopIP(cfg, routerName, nextHopRouter)
			if err != nil {
				return nil, fmt.Errorf("find next-hop IP from %s to %s: %w", routerName, destRouter, err)
			}

			routes[routerName] = append(routes[routerName], Route{
				Destination: cidr,
				NextHop:     nextHopIP,
				OutIface:    outIface,
			})
		}
	}

	return routes, nil
}

// bfsNextHop finds the next-hop router from src to dst using BFS.
func bfsNextHop(g *Graph, src, dst string) (string, error) {
	if src == dst {
		return "", fmt.Errorf("src and dst are the same router")
	}

	type item struct {
		router string
		prev   string
	}

	queue := []item{{router: src, prev: ""}}
	visited := map[string]bool{src: true}
	parent := map[string]string{}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.router == dst {
			break
		}

		for _, nb := range g.Adjacency[cur.router] {
			if visited[nb] {
				continue
			}
			visited[nb] = true
			parent[nb] = cur.router
			queue = append(queue, item{router: nb, prev: cur.router})
		}
	}

	if !visited[dst] {
		return "", fmt.Errorf("no path from %s to %s", src, dst)
	}

	// Reconstruct path dst -> src.
	cur := dst
	var prev string
	for {
		p, ok := parent[cur]
		if !ok {
			break
		}
		prev = cur
		cur = p
		if cur == src {
			return prev, nil
		}
	}

	return "", fmt.Errorf("failed to determine next hop from %s to %s", src, dst)
}

// findNextHopIP finds the next-hop IP and outgoing interface from srcRouter
// towards nextRouter based on point-to-point links between them.
func findNextHopIP(cfg *config.Config, srcRouter, nextRouter string) (string, string, error) {
	for _, link := range cfg.Topology.Links {
		if len(link.Endpoints) != 2 {
			continue
		}
		n1, if1 := config.SplitEndpointPublic(link.Endpoints[0])
		n2, if2 := config.SplitEndpointPublic(link.Endpoints[1])
		if (n1 == srcRouter && n2 == nextRouter) || (n1 == nextRouter && n2 == srcRouter) {
			iface1 := cfg.Topology.Nodes[n1].Interfaces[if1]
			iface2 := cfg.Topology.Nodes[n2].Interfaces[if2]
			if n1 == srcRouter {
				return iface1.PeerIP, if1, nil
			}
			return iface2.PeerIP, if2, nil
		}
	}
	return "", "", fmt.Errorf("no direct link between %s and %s", srcRouter, nextRouter)
}

