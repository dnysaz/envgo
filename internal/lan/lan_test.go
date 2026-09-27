package lan

import (
	"net"
	"testing"
)

func ipNet(cidr string) net.Addr {
	ip, _, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return &net.IPNet{IP: ip}
}

func TestPickV4(t *testing.T) {
	tests := []struct {
		name  string
		addrs []net.Addr
		want  string
	}{
		{
			name:  "private LAN address is accepted",
			addrs: []net.Addr{ipNet("192.168.1.42/24")},
			want:  "192.168.1.42",
		},
		{
			name:  "10.x private address is accepted",
			addrs: []net.Addr{ipNet("10.0.0.5/8")},
			want:  "10.0.0.5",
		},
		{
			name:  "loopback alone is rejected",
			addrs: []net.Addr{ipNet("127.0.0.1/8")},
			want:  "",
		},
		{
			name:  "link-local alone is rejected",
			addrs: []net.Addr{ipNet("169.254.10.3/16")},
			want:  "",
		},
		{
			name:  "ipv6 alone is rejected",
			addrs: []net.Addr{ipNet("fe80::1/64")},
			want:  "",
		},
		{
			name:  "skips link-local and picks the LAN address",
			addrs: []net.Addr{ipNet("169.254.10.3/16"), ipNet("192.168.1.42/24")},
			want:  "192.168.1.42",
		},
		{
			name:  "skips ipv6 and loopback",
			addrs: []net.Addr{ipNet("::1/128"), ipNet("127.0.0.1/8"), ipNet("172.16.3.9/20")},
			want:  "172.16.3.9",
		},
		{
			name:  "empty list yields nothing",
			addrs: nil,
			want:  "",
		},
		{
			name:  "IPAddr form is accepted",
			addrs: []net.Addr{&net.IPAddr{IP: net.ParseIP("192.168.0.77")}},
			want:  "192.168.0.77",
		},
		{
			name:  "unknown address types are ignored",
			addrs: []net.Addr{ipNet("127.0.0.1/8")},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pickV4(tt.addrs); got != tt.want {
				t.Errorf("pickV4() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLocalIPIsUsable(t *testing.T) {
	got := LocalIP()
	// A machine with no non-loopback IPv4 legitimately returns "", so only
	// validate the shape of whatever we do get back.
	if got == "" {
		t.Skip("no non-loopback IPv4 address on this host")
	}
	ip := net.ParseIP(got)
	if ip == nil {
		t.Fatalf("LocalIP() returned %q which is not an IP address", got)
	}
	if ip.To4() == nil {
		t.Fatalf("LocalIP() returned %q which is not IPv4", got)
	}
	if ip.IsLoopback() {
		t.Fatalf("LocalIP() returned the loopback address %q", got)
	}
	if ip.IsUnspecified() {
		t.Fatalf("LocalIP() returned the unspecified address %q", got)
	}
}
