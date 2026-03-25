package config

import (
	"bytes"
	"fmt"
	"net"
)

// ipv4NetBounds returns the first and last IPv4 address in n's subnet (inclusive).
func ipv4NetBounds(n *net.IPNet) (start, end net.IP, ok bool) {
	ip := n.IP.To4()
	if ip == nil {
		return nil, nil, false
	}
	mask := net.IP(n.Mask).To4()
	if mask == nil {
		return nil, nil, false
	}
	start = make(net.IP, 4)
	for i := 0; i < 4; i++ {
		start[i] = ip[i] & mask[i]
	}
	hostBits := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		hostBits[i] = ^mask[i]
	}
	end = make(net.IP, 4)
	for i := 0; i < 4; i++ {
		end[i] = start[i] | hostBits[i]
	}
	return start, end, true
}

// ipv4CIDROverlap reports whether two IPv4 CIDRs share at least one address.
func ipv4CIDROverlap(a, b *net.IPNet) bool {
	as, ae, okA := ipv4NetBounds(a)
	bs, be, okB := ipv4NetBounds(b)
	if !okA || !okB {
		return false
	}
	return bytes.Compare(as, be) <= 0 && bytes.Compare(bs, ae) <= 0
}

type namedIPv4Net struct {
	field string
	net   *net.IPNet
}

func appendIPv4Pool(list []namedIPv4Net, field, cidr string) []namedIPv4Net {
	if cidr == "" {
		return list
	}
	_, ipn, err := net.ParseCIDR(cidr)
	if err != nil || ipn.IP.To4() == nil {
		return list
	}
	return append(list, namedIPv4Net{field: field, net: ipn})
}

func validateIPv4PoolOverlap(cfg *Config, merr *MultiError) {
	list := appendIPv4Pool(nil, "addressing.p2p", cfg.Addressing.P2P)
	list = appendIPv4Pool(list, "addressing.lan", cfg.Addressing.LAN)
	list = appendIPv4Pool(list, "addressing.loopback", cfg.Addressing.Loopback)
	if cfg.Mgmt.Enable {
		list = appendIPv4Pool(list, "mgmt.ipv4", cfg.Mgmt.IPv4)
	}
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if ipv4CIDROverlap(list[i].net, list[j].net) {
				merr.Add(&FieldError{
					Field: "addressing",
					Message: fmt.Sprintf("IPv4 ranges %q and %q overlap",
						list[i].field, list[j].field),
				})
			}
		}
	}
}
