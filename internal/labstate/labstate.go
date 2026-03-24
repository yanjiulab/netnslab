package labstate

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/yanjiulab/netnslab/internal/config"
)

// Persisted models a deployed lab for runtime introspection (show, etc.).
type Persisted struct {
	Name    string            `json:"name"`
	Nodes   map[string]string `json:"nodes"` // nodeName -> kind
	Links   []PersistedLink   `json:"links"`
	Routing struct {
		AutoStatic bool `json:"auto_static"`
	} `json:"routing"`
	Mgmt struct {
		Enable bool   `json:"enable"`
		IPv4   string `json:"ipv4,omitempty"`
	} `json:"mgmt"`
}

// PersistedLink is one edge with resolved endpoint names "node:ifname".
type PersistedLink struct {
	Endpoints [2]string         `json:"endpoints"`
	Netem     *config.LinkNetem `json:"netem,omitempty"`
}

// FromConfig builds persisted state after topology build and addressing.
func FromConfig(cfg *config.Config) *Persisted {
	p := &Persisted{
		Name:  cfg.Name,
		Nodes: make(map[string]string),
		Links: make([]PersistedLink, 0, len(cfg.Topology.Links)),
	}
	for n, node := range cfg.Topology.Nodes {
		p.Nodes[n] = node.Kind
	}
	for _, l := range cfg.Topology.Links {
		if len(l.Endpoints) != 2 {
			continue
		}
		pl := PersistedLink{Endpoints: [2]string{l.Endpoints[0], l.Endpoints[1]}}
		if l.Netem != nil && l.Netem.NetemActive() {
			nm := *l.Netem
			pl.Netem = &nm
		}
		p.Links = append(p.Links, pl)
	}
	p.Routing.AutoStatic = cfg.Routing.AutoStatic
	p.Mgmt.Enable = cfg.Mgmt.Enable
	p.Mgmt.IPv4 = cfg.Mgmt.IPv4
	return p
}

// Save writes lab state to path (directory must exist or be creatable).
func Save(path string, p *Persisted) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Load reads lab state from path.
func Load(path string) (*Persisted, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Persisted
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse lab state: %w", err)
	}
	return &p, nil
}
