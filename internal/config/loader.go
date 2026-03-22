package config

import (
	"errors"
	"fmt"
	"net"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadConfig reads and validates a lab configuration from the given path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal YAML %q: %w", path, err)
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	var merr MultiError

	if cfg.Name == "" {
		merr.Add(&FieldError{Field: "name", Message: "must not be empty"})
	}

	if cfg.Topology.Nodes == nil || len(cfg.Topology.Nodes) == 0 {
		merr.Add(&FieldError{Field: "topology.nodes", Message: "must contain at least one node"})
	}
	if cfg.Topology.Links == nil || len(cfg.Topology.Links) == 0 {
		merr.Add(&FieldError{Field: "topology.links", Message: "must contain at least one link"})
	}

	// Validate that all link endpoints reference existing nodes.
	for i, link := range cfg.Topology.Links {
		if len(link.Endpoints) != 2 {
			merr.Add(&FieldError{
				Field:   fmt.Sprintf("topology.links[%d].endpoints", i),
				Message: "must contain exactly two endpoints",
			})
			continue
		}
		for j, ep := range link.Endpoints {
			nodeName, _ := splitEndpoint(ep)
			if _, ok := cfg.Topology.Nodes[nodeName]; !ok {
				merr.Add(&FieldError{
					Field:   fmt.Sprintf("topology.links[%d].endpoints[%d]", i, j),
					Message: fmt.Sprintf("references unknown node %q", nodeName),
				})
			}
		}
		if link.Netem != nil {
			nm := link.Netem
			if nm.DelayMS < 0 {
				merr.Add(&FieldError{Field: fmt.Sprintf("topology.links[%d].netem.delay_ms", i), Message: "must be >= 0"})
			}
			if nm.JitterMS < 0 {
				merr.Add(&FieldError{Field: fmt.Sprintf("topology.links[%d].netem.jitter_ms", i), Message: "must be >= 0"})
			}
			if nm.LossPercent < 0 || nm.LossPercent > 100 {
				merr.Add(&FieldError{Field: fmt.Sprintf("topology.links[%d].netem.loss_percent", i), Message: "must be between 0 and 100"})
			}
			if !nm.NetemActive() {
				merr.Add(&FieldError{Field: fmt.Sprintf("topology.links[%d].netem", i), Message: "must set at least one of delay_ms, jitter_ms, loss_percent"})
			}
		}
	}

	// Validate mgmt.
	if cfg.Mgmt.Enable {
		if cfg.Mgmt.IPv4 == "" {
			merr.Add(&FieldError{Field: "mgmt.ipv4", Message: "must be set when mgmt.enable is true"})
		} else if _, _, err := net.ParseCIDR(cfg.Mgmt.IPv4); err != nil {
			merr.Add(&FieldError{Field: "mgmt.ipv4", Message: "must be a valid IPv4 CIDR"})
		}
	}

	// Validate addressing pools (basic CIDR check; overlap detection can be added later).
	if cfg.Addressing.P2P != "" {
		if _, _, err := net.ParseCIDR(cfg.Addressing.P2P); err != nil {
			merr.Add(&FieldError{Field: "addressing.p2p", Message: "must be a valid IPv4 CIDR"})
		}
	}
	if cfg.Addressing.LAN != "" {
		if _, _, err := net.ParseCIDR(cfg.Addressing.LAN); err != nil {
			merr.Add(&FieldError{Field: "addressing.lan", Message: "must be a valid IPv4 CIDR"})
		}
	}
	if cfg.Addressing.Loopback != "" {
		if _, _, err := net.ParseCIDR(cfg.Addressing.Loopback); err != nil {
			merr.Add(&FieldError{Field: "addressing.loopback", Message: "must be a valid IPv4 CIDR"})
		}
	}

	return merr.NilOrError()
}

// splitEndpoint splits an endpoint string like "r1:eth1" or "r1" into node and interface name.
// The interface part may be empty if the user did not specify it.
func splitEndpoint(ep string) (node string, ifName string) {
	for i := 0; i < len(ep); i++ {
		if ep[i] == ':' {
			return ep[:i], ep[i+1:]
		}
	}
	return ep, ""
}

// SplitEndpointPublic exposes splitEndpoint for other packages that need
// to parse endpoint strings while keeping the core logic in one place.
func SplitEndpointPublic(ep string) (string, string) {
	return splitEndpoint(ep)
}

// IsValidationError reports whether an error is produced by configuration validation.
func IsValidationError(err error) bool {
	var fe *FieldError
	if errors.As(err, &fe) {
		return true
	}
	var me *MultiError
	if errors.As(err, &me) {
		return true
	}
	return false
}

