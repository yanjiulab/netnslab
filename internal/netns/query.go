package netns

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
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

type IfaceMeta struct {
	Mac       string
	MTU       int
	RxPackets uint64
	RxBytes   uint64
	RxErrors  uint64
	RxDropped uint64
	TxPackets uint64
	TxBytes   uint64
	TxErrors  uint64
	TxDropped uint64
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

// QueryIfaceUp returns whether dev is up in the node netns, plus best-effort operstate.
func QueryIfaceUp(labName, nodeName, dev string) (bool, string) {
	ns := NamespaceName(labName, nodeName)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", ns, "ip", "-j", "link", "show", "dev", dev)
	out, err := cmd.CombinedOutput()
	if err == nil {
		var list []struct {
			IfName    string `json:"ifname"`
			OperState string `json:"operstate"`
		}
		if jerr := json.Unmarshal(out, &list); jerr == nil && len(list) > 0 {
			state := strings.TrimSpace(list[0].OperState)
			return strings.EqualFold(state, "UP"), state
		}
	}

	// Fallback to human output parsing.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	cmd2 := exec.CommandContext(ctx2, "ip", "netns", "exec", ns, "ip", "link", "show", "dev", dev)
	out2, _ := cmd2.CombinedOutput()
	text := string(out2)
	if strings.Contains(text, "state UP") {
		return true, "UP"
	}
	return false, ""
}

// QueryRoutes returns ip route show output as plain lines (best-effort).
func QueryRoutes(labName, nodeName string) []string {
	ns := NamespaceName(labName, nodeName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", ns, "ip", "route", "show")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// QueryTcQdisc returns tc qdisc show -s output (best-effort, truncated).
func QueryTcQdisc(labName, nodeName, dev string) string {
	ns := NamespaceName(labName, nodeName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", ns, "tc", "-s", "qdisc", "show", "dev", dev, "root")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(out))
	if len(text) > 1200 {
		return text[:1200] + "...(truncated)"
	}
	return text
}

// QueryIfaceMeta returns interface MAC/MTU and basic RX/TX counters (best-effort).
func QueryIfaceMeta(labName, nodeName, dev string) IfaceMeta {
	ns := NamespaceName(labName, nodeName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use -s to force kernel RX/TX counters into JSON output.
	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", ns, "ip", "-s", "-j", "link", "show", "dev", dev)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return IfaceMeta{}
	}
	var list []struct {
		Address string `json:"address"`
		MTU     int    `json:"mtu"`
		Stats64 struct {
			Rx struct {
				Packets uint64 `json:"packets"`
				Bytes   uint64 `json:"bytes"`
				Errors  uint64 `json:"errors"`
				Dropped uint64 `json:"dropped"`
			} `json:"rx"`
			Tx struct {
				Packets uint64 `json:"packets"`
				Bytes   uint64 `json:"bytes"`
				Errors  uint64 `json:"errors"`
				Dropped uint64 `json:"dropped"`
			} `json:"tx"`
		} `json:"stats64"`
		Stats struct {
			Rx struct {
				Packets uint64 `json:"packets"`
				Bytes   uint64 `json:"bytes"`
				Errors  uint64 `json:"errors"`
				Dropped uint64 `json:"dropped"`
			} `json:"rx"`
			Tx struct {
				Packets uint64 `json:"packets"`
				Bytes   uint64 `json:"bytes"`
				Errors  uint64 `json:"errors"`
				Dropped uint64 `json:"dropped"`
			} `json:"tx"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(out, &list); err == nil && len(list) > 0 {
		v := list[0]
		rxPackets := v.Stats64.Rx.Packets
		rxBytes := v.Stats64.Rx.Bytes
		rxErrors := v.Stats64.Rx.Errors
		rxDropped := v.Stats64.Rx.Dropped
		txPackets := v.Stats64.Tx.Packets
		txBytes := v.Stats64.Tx.Bytes
		txErrors := v.Stats64.Tx.Errors
		txDropped := v.Stats64.Tx.Dropped

		// Some iproute2 versions expose counters under "stats" instead of "stats64".
		if rxPackets == 0 && txPackets == 0 {
			rxPackets = v.Stats.Rx.Packets
			rxBytes = v.Stats.Rx.Bytes
			rxErrors = v.Stats.Rx.Errors
			rxDropped = v.Stats.Rx.Dropped
			txPackets = v.Stats.Tx.Packets
			txBytes = v.Stats.Tx.Bytes
			txErrors = v.Stats.Tx.Errors
			txDropped = v.Stats.Tx.Dropped
		}
		return IfaceMeta{
			Mac:       strings.TrimSpace(v.Address),
			MTU:       v.MTU,
			RxPackets: rxPackets,
			RxBytes:   rxBytes,
			RxErrors:  rxErrors,
			RxDropped: rxDropped,
			TxPackets: txPackets,
			TxBytes:   txBytes,
			TxErrors:  txErrors,
			TxDropped: txDropped,
		}
	}
	return IfaceMeta{}
}

// QueryNeighbors returns "ip neigh show" lines (best-effort).
func QueryNeighbors(labName, nodeName string) []string {
	ns := NamespaceName(labName, nodeName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", ns, "ip", "neigh", "show")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
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

