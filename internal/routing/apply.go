package routing

import (
	"fmt"

	"github.com/yanjiulab/netnslab/internal/config"
	"github.com/yanjiulab/netnslab/internal/netns"
)

// ApplyRoutes installs the computed static routes into router namespaces.
func ApplyRoutes(cfg *config.Config, routes map[string][]Route) error {
	labName := cfg.Name
	for routerName, rs := range routes {
		for _, r := range rs {
			if err := netns.AddRoute(labName, routerName, r.Destination, r.NextHop); err != nil {
				return fmt.Errorf("add route on router %s: %w", routerName, err)
			}
		}
	}
	return nil
}
