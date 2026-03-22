package netns

import (
	"fmt"
	"strings"

	"github.com/yourname/netnslab/internal/config"
)

// EnsureLoopbackUp brings the loopback interface up in the node namespace.
func EnsureLoopbackUp(labName, nodeName string) error {
	nsName := NamespaceName(labName, nodeName)
	return runInNetns(nsName, "ip", "link", "set", "lo", "up")
}

// ConfigureLinkInterface sets IP address and brings interface up inside the node namespace.
func ConfigureLinkInterface(labName, nodeName, ifName, cidr string) error {
	nsName := NamespaceName(labName, nodeName)
	if cidr != "" {
		if err := runInNetns(nsName, "ip", "addr", "add", cidr, "dev", ifName); err != nil {
			return err
		}
	}
	if err := runInNetns(nsName, "ip", "link", "set", ifName, "up"); err != nil {
		return err
	}
	return nil
}

// ConfigureSysctl applies sysctl settings in the given node namespace.
func ConfigureSysctl(labName, nodeName string, sysctls map[string]string) error {
	if len(sysctls) == 0 {
		return nil
	}
	nsName := NamespaceName(labName, nodeName)
	for k, v := range sysctls {
		if strings.TrimSpace(k) == "" {
			continue
		}
		arg := fmt.Sprintf("%s=%s", k, v)
		if err := runInNetns(nsName, "sysctl", "-w", arg); err != nil {
			return err
		}
	}
	return nil
}

// ConfigureNodeSysctl applies YAML sysctl plus defaults: routers get ip_forward=1 unless overridden.
func ConfigureNodeSysctl(labName, nodeName string, n *config.Node) error {
	m := make(map[string]string)
	for k, v := range n.Sysctl {
		m[k] = v
	}
	if n.Kind == "router" {
		if _, ok := m["net.ipv4.ip_forward"]; !ok {
			m["net.ipv4.ip_forward"] = "1"
		}
	}
	return ConfigureSysctl(labName, nodeName, m)
}

// hostOnly strips a /prefix suffix if present. ip-route "via" expects a host address, not CIDR.
func hostOnly(addr string) string {
	addr = strings.TrimSpace(addr)
	if i := strings.Index(addr, "/"); i >= 0 {
		return addr[:i]
	}
	return addr
}

// AddRoute installs a static route inside the given node namespace.
func AddRoute(labName, nodeName, destination, nextHop string) error {
	nsName := NamespaceName(labName, nodeName)
	nh := hostOnly(nextHop)
	if nh == "" {
		return fmt.Errorf("empty next-hop for route to %s", destination)
	}
	args := []string{"route", "add", destination, "via", nh}
	return runInNetns(nsName, "ip", args...)
}

// AddDefaultRoute sets default gateway for a node (hosts only in typical labs; not used for routers).
func AddDefaultRoute(labName, nodeName, gateway string) error {
	nsName := NamespaceName(labName, nodeName)
	gw := hostOnly(gateway)
	if gw == "" {
		return fmt.Errorf("empty gateway for default route on node %s", nodeName)
	}
	return runInNetns(nsName, "ip", "route", "replace", "default", "via", gw)
}

// AssignBridgeIP assigns an IP address to a bridge on the host.
func AssignBridgeIP(bridge, cidr string) error {
	if err := runIP("addr", "add", cidr, "dev", bridge); err != nil {
		return err
	}
	return nil
}

// CreateMgmtInterface creates a veth pair and connects a node eth0 to the host bridge.
func CreateMgmtInterface(labName, nodeName, bridgeName, ifName, cidr string) error {
	nsName := NamespaceName(labName, nodeName)

	hostIf := fmt.Sprintf("m-%s-%s", nodeName, ifName)
	if err := runIP("link", "add", hostIf, "type", "veth", "peer", "name", ifName); err != nil {
		return err
	}

	if err := runIP("link", "set", hostIf, "master", bridgeName); err != nil {
		return err
	}
	if err := runIP("link", "set", hostIf, "up"); err != nil {
		return err
	}

	if err := runIP("link", "set", ifName, "netns", nsName); err != nil {
		return err
	}
	if err := runInNetns(nsName, "ip", "addr", "add", cidr, "dev", ifName); err != nil {
		return err
	}
	if err := runInNetns(nsName, "ip", "link", "set", ifName, "up"); err != nil {
		return err
	}

	return nil
}



