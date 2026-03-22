package netns

import (
	"fmt"
	"strings"
)

// LinuxBridgeName is the in-netns bridge device for kind=bridge nodes.
const LinuxBridgeName = "br0"

// SetupLinuxBridgeInNode creates br0 (if needed) and attaches data ports.
// Management (eth0) and loopback are not enslaved.
func SetupLinuxBridgeInNode(labName, nodeName string, ports []string) error {
	ns := NamespaceName(labName, nodeName)

	err := runInNetns(ns, "ip", "link", "add", LinuxBridgeName, "type", "bridge", "stp_state", "0")
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "exists") {
		return fmt.Errorf("create bridge in %s: %w", nodeName, err)
	}
	// If br0 already existed, try to disable STP for faster forwarding.
	_ = runInNetns(ns, "ip", "link", "set", LinuxBridgeName, "type", "bridge", "stp_state", "0")

	for _, p := range ports {
		if p == "" || p == MgmtIfaceName || p == "lo" || p == LinuxBridgeName {
			continue
		}
		if err := runInNetns(ns, "ip", "link", "set", p, "master", LinuxBridgeName); err != nil {
			return fmt.Errorf("node %s: enslave %s to %s: %w", nodeName, p, LinuxBridgeName, err)
		}
		if err := runInNetns(ns, "ip", "link", "set", p, "up"); err != nil {
			return fmt.Errorf("node %s: set %s up: %w", nodeName, p, err)
		}
	}

	if err := runInNetns(ns, "ip", "link", "set", LinuxBridgeName, "up"); err != nil {
		return fmt.Errorf("node %s: bring %s up: %w", nodeName, LinuxBridgeName, err)
	}
	return nil
}
