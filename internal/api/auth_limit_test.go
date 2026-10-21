package api

import "testing"

func TestLimitKeyGroupsAnIPv6BlockAndKeepsIPv4(t *testing.T) {
	tests := []struct{ ip, want string }{
		{"192.168.1.10", "192.168.1.10"},
		{"2001:db8:1:2:aaaa:bbbb:cccc:dddd", "2001:db8:1:2::/64"},
		{"2001:db8:1:2:1111:2222:3333:4444", "2001:db8:1:2::/64"},
		{"2001:db8:1:3::1", "2001:db8:1:3::/64"},
		{"::ffff:192.168.1.10", "::ffff:192.168.1.10"},
		{"not an address", "not an address"},
	}
	for _, tt := range tests {
		if got := limitKey(tt.ip); got != tt.want {
			t.Errorf("limitKey(%q) = %q, want %q", tt.ip, got, tt.want)
		}
	}
}
