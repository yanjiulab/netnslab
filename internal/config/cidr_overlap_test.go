package config

import (
	"net"
	"testing"
)

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIPv4CIDROverlap(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"10.0.0.0/24", "10.0.1.0/24", false},
		{"10.0.0.0/16", "10.0.255.0/24", true},
		{"10.1.0.0/16", "10.2.0.0/16", false},
		{"192.168.0.0/24", "192.168.0.0/24", true},
	}
	for _, tt := range tests {
		a := mustCIDR(t, tt.a)
		b := mustCIDR(t, tt.b)
		if got := ipv4CIDROverlap(a, b); got != tt.want {
			t.Errorf("ipv4CIDROverlap(%q,%q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}
