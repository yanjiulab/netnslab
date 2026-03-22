package netns

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// MgmtIfaceName is the management interface inside each node netns.
const MgmtIfaceName = "eth0"

type ipAddrIface struct {
	AddrInfo []struct {
		Family    string `json:"family"`
		Local     string `json:"local"`
		Prefixlen int    `json:"prefixlen"`
	} `json:"addr_info"`
}

// QueryIfaceIPv4 returns the first IPv4 address on dev in CIDR form (e.g. 10.0.0.1/24),
// or empty string if none or query failed.
func QueryIfaceIPv4(labName, nodeName, dev string) string {
	ns := NamespaceName(labName, nodeName)
	cmd := exec.Command("ip", "netns", "exec", ns, "ip", "-j", "addr", "show", "dev", dev)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}

	var list []ipAddrIface
	if err := json.Unmarshal(out, &list); err != nil || len(list) == 0 {
		return fallbackParseIPv4(string(out))
	}

	for _, ai := range list[0].AddrInfo {
		if ai.Family == "inet" && ai.Local != "" {
			return fmt.Sprintf("%s/%d", ai.Local, ai.Prefixlen)
		}
	}
	return ""
}

func fallbackParseIPv4(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "inet ") && !strings.Contains(line, "inet6") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "inet" && i+1 < len(fields) {
					return fields[i+1]
				}
			}
		}
	}
	return ""
}
