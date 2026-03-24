package netns

import (
	"fmt"

	"github.com/yanjiulab/netnslab/internal/config"
)

// netemArgs builds tc netem parameters (delay/jitter/loss only).
func netemArgs(n *config.LinkNetem) []string {
	if n == nil || !n.NetemActive() {
		return nil
	}
	var p []string
	if n.DelayMS > 0 || n.JitterMS > 0 {
		p = append(p, "delay", fmt.Sprintf("%dms", n.DelayMS))
		if n.JitterMS > 0 {
			p = append(p, fmt.Sprintf("%dms", n.JitterMS))
		}
	}
	if n.LossPercent > 0 {
		p = append(p, "loss", fmt.Sprintf("%g%%", n.LossPercent))
	}
	return p
}

// ApplyNetem installs tc netem as root qdisc on the interface inside the node netns.
func ApplyNetem(labName, nodeName, ifName string, n *config.LinkNetem) error {
	args := netemArgs(n)
	if len(args) == 0 {
		return nil
	}
	ns := NamespaceName(labName, nodeName)
	tcArgs := append([]string{"qdisc", "replace", "dev", ifName, "root", "netem"}, args...)
	return runInNetns(ns, "tc", tcArgs...)
}

// ClearNetem removes root qdisc on the interface (best-effort for destroy).
func ClearNetem(labName, nodeName, ifName string) {
	ns := NamespaceName(labName, nodeName)
	_ = runInNetns(ns, "tc", "qdisc", "del", "dev", ifName, "root")
}
