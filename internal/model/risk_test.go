package model

import "testing"

func TestRateService(t *testing.T) {
	cases := []struct {
		name  string
		port  int
		proto string
		want  RiskSeverity // empty means no risk
	}{
		{"telnet", 23, "tcp", RiskHigh},
		{"tftp", 69, "udp", RiskHigh},
		{"vnc", 5900, "tcp", RiskHigh},
		{"ftp", 21, "tcp", RiskMedium},
		{"smb", 445, "tcp", RiskMedium},
		{"rdp", 3389, "tcp", RiskMedium},
		{"snmp", 161, "udp", RiskMedium},
		{"telnet over udp is not telnet", 23, "udp", ""},
		{"tftp over tcp is not tftp", 69, "tcp", ""},
		{"https", 443, "tcp", ""},
		{"ssh", 22, "tcp", ""},
		{"port zero", 0, "tcp", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RateService(c.port, c.proto)
			if c.want == "" {
				if got != nil {
					t.Fatalf("RateService(%d, %q) = %+v, want nil", c.port, c.proto, got)
				}
				return
			}
			if got == nil || got.Severity != c.want || got.Message == "" {
				t.Fatalf("RateService(%d, %q) = %+v, want severity %q and a message", c.port, c.proto, got, c.want)
			}
		})
	}
}
