package topology

import (
	"sort"

	"github.com/yourname/netnslab/internal/config"
)

// BridgePortIfaces returns sorted data-plane interface names on a bridge node
// (from topology links), excluding anything not explicitly wired.
func BridgePortIfaces(cfg *config.Config, bridgeNodeName string) []string {
	seen := make(map[string]struct{})
	for _, link := range cfg.Topology.Links {
		for _, ep := range link.Endpoints {
			n, ifn := config.SplitEndpointPublic(ep)
			if n != bridgeNodeName || ifn == "" {
				continue
			}
			seen[ifn] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for ifn := range seen {
		out = append(out, ifn)
	}
	sort.Strings(out)
	return out
}
