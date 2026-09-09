package discover

import (
	"net"
	"testing"
)

// TestExpand covers the address arithmetic, which is the part that would fail
// silently: an off-by-one here would skip a panel or probe a broadcast
// address, and the sweep would just come back empty.
func TestExpand(t *testing.T) {
	tests := []struct {
		cidr  string
		count int
		first string
		last  string
	}{
		{"10.0.0.0/24", 254, "10.0.0.1", "10.0.0.254"},
		{"192.168.1.0/24", 254, "192.168.1.1", "192.168.1.254"},
		{"10.0.0.0/30", 2, "10.0.0.1", "10.0.0.2"},
		// A network whose address does not sit on the boundary must
		// still expand from the boundary.
		{"10.0.0.112/24", 254, "10.0.0.1", "10.0.0.254"},
		// Carrying across the third octet.
		{"10.0.0.0/23", 510, "10.0.0.1", "10.0.1.254"},
	}

	for _, tc := range tests {
		t.Run(tc.cidr, func(t *testing.T) {
			_, n, err := net.ParseCIDR(tc.cidr)
			if err != nil {
				t.Fatalf("ParseCIDR: %v", err)
			}
			// ParseCIDR masks the address, so restore the host part
			// for the off-boundary case.
			if ip, _, _ := net.ParseCIDR(tc.cidr); ip != nil {
				n.IP = ip
			}

			got := expand(n)
			if len(got) != tc.count {
				t.Fatalf("got %d hosts, want %d", len(got), tc.count)
			}
			if got[0] != tc.first {
				t.Errorf("first = %s, want %s", got[0], tc.first)
			}
			if got[len(got)-1] != tc.last {
				t.Errorf("last = %s, want %s", got[len(got)-1], tc.last)
			}
		})
	}
}

// TestExpandSkipsTheEdges pins the two addresses that must never be probed.
// Neither is a host, and the broadcast address in particular would send the
// probe to every machine on the network.
func TestExpandSkipsTheEdges(t *testing.T) {
	_, n, err := net.ParseCIDR("10.0.0.0/24")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}

	for _, host := range expand(n) {
		if host == "10.0.0.0" {
			t.Error("the network address was included")
		}
		if host == "10.0.0.255" {
			t.Error("the broadcast address was included")
		}
	}
}

// TestLocalHostsFiltersWideNetworks is what keeps the sweep quick and polite.
// A developer machine carries a container bridge on a /16 and a VPN on a /32,
// and expanding either would be useless: the first is 65534 addresses, the
// second has no other host on it.
func TestLocalHostsFiltersWideNetworks(t *testing.T) {
	hosts, err := localHosts()
	if err != nil {
		t.Fatalf("localHosts: %v", err)
	}

	// Whatever this machine has, the result must be a plausible number of
	// LAN addresses rather than a container bridge expanded in full.
	if len(hosts) > 4096 {
		t.Errorf("expanded to %d hosts; a network that wide should have been skipped", len(hosts))
	}
	for _, h := range hosts {
		ip := net.ParseIP(h)
		if ip == nil {
			t.Errorf("%q is not an address", h)
			continue
		}
		if ip.IsLoopback() {
			t.Errorf("loopback address %s was included", h)
		}
	}
}
