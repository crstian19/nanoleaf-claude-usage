package discover

import (
	"fmt"
	"net"
)

// localHosts lists every IPv4 address worth probing on the machine's own
// networks.
//
// The filtering matters as much as the listing. A developer machine is full
// of interfaces that would waste the sweep or make it rude: container bridges
// carry a /16 each, a VPN hands out a /32, and loopback has nothing on it. So
// only up interfaces with a prefix narrow enough to sweep quickly are
// expanded, which on a normal machine leaves exactly the LAN.
func localHosts() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("discover: list interfaces: %w", err)
	}

	seen := map[string]bool{}
	var hosts []string

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			ones, bits := ipnet.Mask.Size()
			// A prefix wider than smallestNetwork is too big to
			// sweep; one narrower than /30 has no other host on it.
			if bits != 32 || ones < smallestNetwork || ones > 30 {
				continue
			}

			for _, h := range expand(ipnet) {
				if !seen[h] {
					seen[h] = true
					hosts = append(hosts, h)
				}
			}
		}
	}
	return hosts, nil
}

// expand lists the usable host addresses of a network, leaving out the
// network and broadcast addresses.
func expand(n *net.IPNet) []string {
	base := n.IP.Mask(n.Mask).To4()
	if base == nil {
		return nil
	}
	ones, _ := n.Mask.Size()
	count := 1 << (32 - ones)

	out := make([]string, 0, count)
	for i := 1; i < count-1; i++ {
		ip := make(net.IP, 4)
		copy(ip, base)
		// Add i to the base address, then split it back into octets.
		// The masks are explicit rather than relying on the
		// conversion to truncate: only the top octet is narrow enough
		// for that to be correct, and a reader should not have to work
		// out which.
		v := uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
		v += uint32(i) //nolint:gosec // i is bounded by the host count of a /22 at widest
		ip[0] = byte(v >> 24 & 0xff)
		ip[1] = byte(v >> 16 & 0xff)
		ip[2] = byte(v >> 8 & 0xff)
		ip[3] = byte(v & 0xff)
		out = append(out, ip.String())
	}
	return out
}
