package topology

import (
	"fmt"

	"github.com/yanjiulab/netnslab/internal/config"
)

// BuiltTopology is an in-memory representation of the lab topology,
// including resolved interface names and addressing information.
type BuiltTopology struct {
	Config *config.Config
}

// Build processes the raw config topology, assigns interface names where
// missing and prepares runtime structures on nodes and links.
func Build(cfg *config.Config) (*BuiltTopology, error) {
	for nodeName, n := range cfg.Topology.Nodes {
		if n.Interfaces == nil {
			n.Interfaces = make(map[string]*config.Interface)
		}
		// Ensure Kind is set to something meaningful.
		if n.Kind == "" {
			return nil, fmt.Errorf("node %q: kind must not be empty", nodeName)
		}
	}

	// Assign interface names when omitted and set up basic interface/runtime info.
	if err := assignInterfaces(cfg); err != nil {
		return nil, err
	}

	// IP addressing is handled in addressing.go via AllocateAddresses.

	return &BuiltTopology{Config: cfg}, nil
}
