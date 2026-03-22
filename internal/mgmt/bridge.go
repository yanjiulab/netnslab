package mgmt

import (
	"fmt"
	"net"

	"github.com/yourname/netnslab/internal/config"
	"github.com/yourname/netnslab/internal/netns"
)

// SetupMgmtBridge creates the host-side management bridge and connects
// per-node eth0 interfaces when mgmt is enabled.
func SetupMgmtBridge(cfg *config.Config) error {
	if !cfg.Mgmt.Enable {
		return nil
	}

	_, subnet, err := net.ParseCIDR(cfg.Mgmt.IPv4)
	if err != nil {
		return fmt.Errorf("invalid mgmt.ipv4: %w", err)
	}

	bridgeName := "netnslab-mgmt"

	// Create bridge and assign gateway IP (.1 in the subnet).
	if err := netns.CreateBridge(bridgeName); err != nil {
		return err
	}
	gwIP := firstIP(subnet).String() + "/" + fmt.Sprint(maskSize(subnet))
	if err := netns.AssignBridgeIP(bridgeName, gwIP); err != nil {
		return err
	}

	// Connect each node with a dedicated management interface eth0.
	hostIPs := hostIPs(subnet, len(cfg.Topology.Nodes))
	i := 0
	for nodeName := range cfg.Topology.Nodes {
		ip := hostIPs[i]
		i++
		if err := netns.CreateMgmtInterface(cfg.Name, nodeName, bridgeName, "eth0", ip); err != nil {
			return err
		}
	}

	return nil
}

// TeardownMgmtBridge removes per-node management veths and the host bridge.
func TeardownMgmtBridge(cfg *config.Config) error {
	if !cfg.Mgmt.Enable {
		return nil
	}

	bridgeName := "netnslab-mgmt"

	// Delete host-side management veth interfaces.
	for nodeName := range cfg.Topology.Nodes {
		hostIf := fmt.Sprintf("m-%s-%s", nodeName, "eth0")
		_ = netns.DeleteLink(hostIf)
	}

	// Delete the management bridge itself.
	if err := netns.DeleteLink(bridgeName); err != nil {
		return err
	}

	return nil
}

func firstIP(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	out := make(net.IP, len(ip))
	copy(out, ip)
	out[3]++
	return out
}

func hostIPs(n *net.IPNet, count int) []string {
	res := make([]string, 0, count)
	base := n.IP.To4()
	for i := 2; len(res) < count; i++ {
		ip := make(net.IP, len(base))
		copy(ip, base)
		ip[3] += byte(i)
		res = append(res, ip.String()+"/"+fmt.Sprint(maskSize(n)))
	}
	return res
}

func maskSize(n *net.IPNet) int {
	ones, _ := n.Mask.Size()
	return ones
}

