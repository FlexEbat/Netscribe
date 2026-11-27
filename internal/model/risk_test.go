package model

import (
	"strings"
	"testing"
	"time"
)

var riskNow = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func TestRateServicePorts(t *testing.T) {
	cases := []struct {
		name  string
		port  int
		proto string
		want  RiskSeverity // empty means no risk
		code  string
	}{
		{"telnet", 23, "tcp", RiskHigh, "telnet"},
		{"tftp", 69, "udp", RiskHigh, "tftp"},
		{"vnc", 5900, "tcp", RiskHigh, "vnc"},
		{"ftp", 21, "tcp", RiskMedium, "ftp"},
		{"smb", 445, "tcp", RiskMedium, "smb"},
		{"rdp", 3389, "tcp", RiskMedium, "rdp"},
		{"snmp", 161, "udp", RiskMedium, "snmp"},
		{"docker api", 2375, "tcp", RiskHigh, "docker_api"},
		{"redis", 6379, "tcp", RiskHigh, "redis"},
		{"mongodb", 27017, "tcp", RiskHigh, "mongodb"},
		{"elasticsearch", 9200, "tcp", RiskHigh, "elasticsearch"},
		{"memcached tcp", 11211, "tcp", RiskHigh, "memcached"},
		{"memcached udp", 11211, "udp", RiskHigh, "memcached"},
		{"mysql", 3306, "tcp", RiskMedium, "mysql"},
		{"postgresql", 5432, "tcp", RiskMedium, "postgresql"},
		{"mssql", 1433, "tcp", RiskMedium, "mssql"},
		{"couchdb", 5984, "tcp", RiskMedium, "couchdb"},
		{"nfs", 2049, "tcp", RiskMedium, "nfs"},
		{"rpcbind", 111, "tcp", RiskMedium, "rpcbind"},
		{"rpcbind udp", 111, "udp", RiskMedium, "rpcbind"},
		{"rsync", 873, "tcp", RiskMedium, "rsync"},
		{"mqtt", 1883, "tcp", RiskMedium, "mqtt"},
		{"msrpc", 135, "tcp", RiskMedium, "msrpc"},
		{"telnet over udp is not telnet", 23, "udp", "", ""},
		{"tftp over tcp is not tftp", 69, "tcp", "", ""},
		{"redis over udp is not redis", 6379, "udp", "", ""},
		{"mysql over udp is not mysql", 3306, "udp", "", ""},
		{"mqtt over udp is not mqtt", 1883, "udp", "", ""},
		{"docker with tls (2376) is not listed", 2376, "tcp", "", ""},
		{"mqtt with tls (8883) is not listed", 8883, "tcp", "", ""},
		{"https", 443, "tcp", "", ""},
		{"ssh", 22, "tcp", "", ""},
		{"port zero", 0, "tcp", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rateServiceAt(Service{Port: c.port, Proto: c.proto}, riskNow)
			if c.want == "" {
				if got != nil {
					t.Fatalf("rate(%d/%s) = %+v, want nil", c.port, c.proto, got)
				}
				return
			}
			if got == nil || got.Severity != c.want || got.Code != c.code || got.Message == "" || got.Why == "" {
				t.Fatalf("rate(%d/%s) = %+v, want severity %q, code %q, a message and a why", c.port, c.proto, got, c.want, c.code)
			}
		})
	}
}

// The seven rules of the first table keep their severity and their sentence.
func TestRateServiceKeepsTheOldRules(t *testing.T) {
	old := []struct {
		port    int
		proto   string
		sev     RiskSeverity
		message string
	}{
		{23, "tcp", RiskHigh, "Telnet sends passwords in clear text"},
		{69, "udp", RiskHigh, "TFTP has no authentication"},
		{5900, "tcp", RiskHigh, "VNC is often open without a strong password"},
		{21, "tcp", RiskMedium, "FTP sends passwords in clear text"},
		{445, "tcp", RiskMedium, "SMB is open, check that old versions are off"},
		{3389, "tcp", RiskMedium, "RDP is open, keep it off the open network"},
		{161, "udp", RiskMedium, "SNMP is open, check the community and the ACL"},
	}
	for _, o := range old {
		got := rateServiceAt(Service{Port: o.port, Proto: o.proto}, riskNow)
		if got == nil || got.Severity != o.sev || got.Message != o.message {
			t.Errorf("rate(%d/%s) = %+v, want %q %q", o.port, o.proto, got, o.sev, o.message)
		}
	}
}

func TestRateServiceCertificate(t *testing.T) {
	cert := func(notAfter time.Time) *TLSCert { return &TLSCert{NotAfter: notAfter} }
	cases := []struct {
		name string
		sv   Service
		want string // the code, empty means no risk
	}{
		{"expired", Service{Port: 443, Proto: "tcp", TLS: cert(riskNow.Add(-time.Hour))}, "tls_expired"},
		{"still valid", Service{Port: 443, Proto: "tcp", TLS: cert(riskNow.Add(time.Hour))}, ""},
		{"no certificate read", Service{Port: 443, Proto: "tcp"}, ""},
		{"no end date", Service{Port: 443, Proto: "tcp", TLS: cert(time.Time{})}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rateServiceAt(c.sv, riskNow)
			switch {
			case c.want == "" && got != nil:
				t.Fatalf("got %+v, want nil", got)
			case c.want != "" && (got == nil || got.Code != c.want || got.Severity != RiskMedium):
				t.Fatalf("got %+v, want medium %q", got, c.want)
			}
		})
	}
}

func TestRateServiceSSHProtocolOne(t *testing.T) {
	cases := []struct {
		name   string
		banner string
		proto  string
		want   string
	}{
		{"protocol 1", "SSH-1.5-old", "tcp", "ssh_protocol_1"},
		{"protocol 1.99 still speaks 1", "SSH-1.99-OpenSSH_3.9", "tcp", "ssh_protocol_1"},
		{"protocol 2", "SSH-2.0-OpenSSH_9.6", "tcp", ""},
		{"the text is not at the start", "x SSH-1.5", "tcp", ""},
		{"lower case is not a banner", "ssh-1.5", "tcp", ""},
		{"not tcp", "SSH-1.5-old", "udp", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rateServiceAt(Service{Port: 22, Proto: c.proto, Banner: c.banner}, riskNow)
			switch {
			case c.want == "" && got != nil:
				t.Fatalf("got %+v, want nil", got)
			case c.want != "" && (got == nil || got.Code != c.want):
				t.Fatalf("got %+v, want %q", got, c.want)
			}
		})
	}
}

// A service with two matching rules shows the more severe one only, whatever the order of
// the rules.
func TestRateServiceKeepsTheMoreSevere(t *testing.T) {
	expired := &TLSCert{NotAfter: riskNow.Add(-time.Hour)}
	got := rateServiceAt(Service{Port: 6379, Proto: "tcp", TLS: expired}, riskNow)
	if got == nil || got.Code != "redis" || got.Severity != RiskHigh {
		t.Fatalf("redis with an expired certificate = %+v, want the high redis risk", got)
	}
	got = rateServiceAt(Service{Port: 3306, Proto: "tcp", TLS: expired}, riskNow)
	if got == nil || got.Severity != RiskMedium {
		t.Fatalf("mysql with an expired certificate = %+v, want one medium risk", got)
	}
}

func TestRiskTableIsConsistent(t *testing.T) {
	var all []Risk
	for _, r := range portRules {
		if len(r.ports) == 0 {
			t.Errorf("rule %q has no port", r.risk.Code)
		}
		all = append(all, r.risk)
	}
	all = append(all, riskCertExpired, riskSSH1)
	seen := map[string]bool{}
	for _, r := range all {
		if !isRiskCode(r.Code) {
			t.Errorf("code %q is not lower case letters, digits and _", r.Code)
		}
		if seen[r.Code] {
			t.Errorf("code %q is used twice", r.Code)
		}
		seen[r.Code] = true
		if r.Severity != RiskHigh && r.Severity != RiskMedium {
			t.Errorf("%s: severity %q", r.Code, r.Severity)
		}
		for name, s := range map[string]string{"message": r.Message, "why": r.Why} {
			if s == "" || len([]rune(s)) > 120 {
				t.Errorf("%s: %s is %d characters, want 1 to 120", r.Code, name, len([]rune(s)))
			}
		}
	}
	// No two rules claim one port and protocol, or the winner would depend on the order.
	owner := map[riskKey]string{}
	for _, r := range portRules {
		for _, k := range r.ports {
			if prev, ok := owner[k]; ok {
				t.Errorf("%d/%s belongs to %s and %s", k.port, k.proto, prev, r.risk.Code)
			}
			owner[k] = r.risk.Code
		}
	}
}

func isRiskCode(s string) bool {
	return s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyz0123456789_") == ""
}
