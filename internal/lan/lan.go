// Package lan discovers the machine's non-loopback IPv4 address so that an
// envgo dev server can be shared with other devices on the same local network.
package lan

import (
	"net"
)

// LocalIP returns the first non-loopback, non-link-local IPv4 address assigned
// to an up interface, as a string (e.g. "192.168.1.42"). It returns "" when no
// suitable address is found (for example on a machine with no network
// interface or only a loopback).
func LocalIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		if best := pickV4(addrs); best != "" {
			return best
		}
	}
	return ""
}

// pickV4 returns the first usable IPv4 address from an interface's address list,
// or "" if there is none. Split out from LocalIP so the selection rules can be
// tested without needing a machine with the right hardware.
func pickV4(addrs []net.Addr) string {
	best := ""
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			continue
		}
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		// Loopback is this machine, link-local (169.254.x.x) is not routable from
		// a phone on the same Wi-Fi in practice, and multicast is not an address
		// you can serve on.
		if v4.IsLoopback() || v4.IsLinkLocalUnicast() || v4.IsLinkLocalMulticast() || v4.IsMulticast() {
			continue
		}
		if !v4.IsGlobalUnicast() {
			continue
		}
		if best == "" {
			best = v4.String()
		}
	}
	return best
}
