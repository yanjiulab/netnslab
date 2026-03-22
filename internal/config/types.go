package config

import (
	"fmt"
	"strings"
)

// Config represents the root of the lab configuration YAML.
type Config struct {
	Name       string     `yaml:"name"`
	Routing    Routing    `yaml:"routing"`
	Addressing Addressing `yaml:"addressing"`
	Mgmt       Mgmt       `yaml:"mgmt"`
	Topology   Topology   `yaml:"topology"`
}

// Routing holds routing related configuration.
type Routing struct {
	AutoStatic bool `yaml:"auto_static"`
}

// Addressing defines address pools for automatic allocation.
type Addressing struct {
	P2P      string `yaml:"p2p"`
	LAN      string `yaml:"lan"`
	Loopback string `yaml:"loopback"`
}

// Mgmt defines management network settings.
type Mgmt struct {
	Enable bool   `yaml:"enable"`
	IPv4   string `yaml:"ipv4"`
}

// Topology describes nodes and links in the lab.
type Topology struct {
	Nodes map[string]*Node `yaml:"nodes"`
	Links []*Link          `yaml:"links"`
}

// Node defines configuration for a single node.
type Node struct {
	Kind   string            `yaml:"kind"`
	Exec   string            `yaml:"exec"`
	Sysctl map[string]string `yaml:"sysctl"`
	Env    map[string]string `yaml:"env"`

	// Runtime fields (not part of YAML).
	Interfaces map[string]*Interface `yaml:"-"`
}

// LinkNetem configures egress impairment (tc netem) on both link endpoints.
type LinkNetem struct {
	DelayMS     int     `yaml:"delay_ms" json:"delay_ms,omitempty"`
	JitterMS    int     `yaml:"jitter_ms" json:"jitter_ms,omitempty"`
	LossPercent float64 `yaml:"loss_percent" json:"loss_percent,omitempty"`
}

// Link defines a connection between two endpoints.
type Link struct {
	Endpoints []string `yaml:"endpoints"`
	IPv4      []string `yaml:"ipv4"`
	Netem     *LinkNetem `yaml:"netem,omitempty"`

	// Runtime fields (not part of YAML).
	Subnet string `yaml:"-"`
}

// NetemActive reports whether any netem parameter is set for tc.
func (n *LinkNetem) NetemActive() bool {
	if n == nil {
		return false
	}
	return n.DelayMS > 0 || n.JitterMS > 0 || n.LossPercent > 0
}

// NetemSummary returns a short human-readable summary for show output.
func (n *LinkNetem) NetemSummary() string {
	if n == nil || !n.NetemActive() {
		return "-"
	}
	var parts []string
	if n.DelayMS > 0 || n.JitterMS > 0 {
		if n.JitterMS > 0 {
			parts = append(parts, fmt.Sprintf("delay=%dms jitter=%dms", n.DelayMS, n.JitterMS))
		} else {
			parts = append(parts, fmt.Sprintf("delay=%dms", n.DelayMS))
		}
	}
	if n.LossPercent > 0 {
		parts = append(parts, fmt.Sprintf("loss=%g%%", n.LossPercent))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

// Interface keeps runtime information about a node interface.
type Interface struct {
	IP       string `yaml:"-"`
	Node     string `yaml:"-"`
	PeerNode string `yaml:"-"`
	PeerIP   string `yaml:"-"`
}

