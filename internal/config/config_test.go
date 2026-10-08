package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// valid returns a config that passes Validate.
func valid() Config {
	c := defaults()
	c.Scan.Targets = []string{"192.168.1.0/24"}
	return c
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want error // nil means the config is valid
	}{
		{"valid defaults with a private target", func(*Config) {}, nil},
		{"no targets", func(c *Config) { c.Scan.Targets = nil }, nil},
		{"target is not a CIDR", func(c *Config) { c.Scan.Targets = []string{"192.168.1.0"} }, errTargetCIDR},
		{"target is not a CIDR at all", func(c *Config) { c.Scan.Targets = []string{"lan"} }, errTargetCIDR},
		{"IPv6 target", func(c *Config) { c.Scan.Targets = []string{"fd00::/64"} }, errTargetCIDR},
		{"target wider than /16", func(c *Config) { c.Scan.Targets = []string{"10.0.0.0/8"} }, errTargetWide},
		{"target exactly /16", func(c *Config) { c.Scan.Targets = []string{"10.1.0.0/16"} }, nil},
		{"172.16 range is private", func(c *Config) { c.Scan.Targets = []string{"172.31.4.0/24"} }, nil},
		{"172.32 is public", func(c *Config) { c.Scan.Targets = []string{"172.32.0.0/24"} }, errTargetPublic},
		{"public target without the flag", func(c *Config) { c.Scan.Targets = []string{"8.8.8.8/32"} }, errTargetPublic},
		{"public target with the flag", func(c *Config) {
			c.Scan.Targets = []string{"8.8.8.8/32"}
			c.AllowPublicTargets = true
		}, nil},
		{"wide target stays wide with the flag", func(c *Config) {
			c.Scan.Targets = []string{"8.0.0.0/8"}
			c.AllowPublicTargets = true
		}, errTargetWide},
		{"interval below 1m", func(c *Config) { c.Scan.Interval = Duration(59 * time.Second) }, errInterval},
		{"negative interval", func(c *Config) { c.Scan.Interval = Duration(-time.Minute) }, errInterval},
		{"interval 0 turns the schedule off", func(c *Config) { c.Scan.Interval = 0 }, nil},
		{"interval exactly 1m", func(c *Config) { c.Scan.Interval = Duration(time.Minute) }, nil},
		{"snmp timeout below 1s", func(c *Config) { c.SNMP.Timeout = Duration(500 * time.Millisecond) }, errSNMPTimeout},
		{"snmp timeout above 30s", func(c *Config) { c.SNMP.Timeout = Duration(31 * time.Second) }, errSNMPTimeout},
		{"snmp timeout at the upper bound", func(c *Config) { c.SNMP.Timeout = Duration(30 * time.Second) }, nil},
		{"ssh host without key", func(c *Config) {
			c.SSH.Hosts = []SSHHost{{Address: "192.168.1.10", User: "audit"}}
		}, errSSHKey},
		{"ssh host with key", func(c *Config) {
			c.SSH.Hosts = []SSHHost{{Address: "192.168.1.10", User: "audit", KeyFile: "/k"}}
		}, nil},
		{"idle timeout below 15m", func(c *Config) { c.Auth.SessionIdleTimeout = Duration(14 * time.Minute) }, errSession},
		{"idle timeout above 24h", func(c *Config) {
			c.Auth.SessionIdleTimeout = Duration(25 * time.Hour)
			c.Auth.SessionMaxAge = Duration(100 * time.Hour)
		}, errSession},
		{"max age below idle timeout", func(c *Config) { c.Auth.SessionMaxAge = Duration(time.Hour) }, errSession},
		{"max age above 720h", func(c *Config) { c.Auth.SessionMaxAge = Duration(721 * time.Hour) }, errSession},
		{"min password length 11", func(c *Config) { c.Auth.MinPasswordLength = 11 }, errMinPassword},
		{"max failed logins 2", func(c *Config) { c.Auth.MaxFailedLogins = 2 }, errMaxFailed},
		{"max failed logins 21", func(c *Config) { c.Auth.MaxFailedLogins = 21 }, errMaxFailed},
		{"argon2 memory below minimum", func(c *Config) { c.Auth.Argon2.MemoryKiB = 19455 }, errArgon2},
		{"argon2 iterations below minimum", func(c *Config) { c.Auth.Argon2.Iterations = 1 }, errArgon2},
		{"argon2 parallelism 0", func(c *Config) { c.Auth.Argon2.Parallelism = 0 }, errArgon2},
		{"argon2 at the minimum", func(c *Config) { c.Auth.Argon2 = Argon2Config{MemoryKiB: 19456, Iterations: 2, Parallelism: 1} }, nil},
		{"tls cert without key", func(c *Config) { c.TLS.CertFile = "c.pem" }, errTLS},
		{"tls key without cert", func(c *Config) { c.TLS.KeyFile = "k.pem" }, errTLS},
		{"tls both files", func(c *Config) { c.TLS = TLSConfig{CertFile: "c.pem", KeyFile: "k.pem"} }, nil},
		{"trusted proxy is not a CIDR", func(c *Config) { c.TrustedProxies = []string{"10.0.0.1"} }, errProxyCIDR},
		{"trusted proxy CIDR", func(c *Config) { c.TrustedProxies = []string{"10.0.0.1/32"} }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := valid()
			tt.edit(&c)
			err := c.Validate()
			switch {
			case tt.want == nil && err != nil:
				t.Fatalf("Validate() = %v, want nil", err)
			case tt.want != nil && !errors.Is(err, tt.want):
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestValidateReportsEveryViolation(t *testing.T) {
	c := valid()
	c.Scan.Targets = []string{"8.8.8.8/32", "bogus"}
	c.Scan.Interval = Duration(time.Second)
	err := c.Validate()
	for _, want := range []error{errTargetPublic, errTargetCIDR, errInterval} {
		if !errors.Is(err, want) {
			t.Errorf("Validate() = %v, missing %v", err, want)
		}
	}
}

func TestValidateNamesTheOffendingEntry(t *testing.T) {
	c := valid()
	c.Scan.Targets = []string{"192.168.1.0/24", "8.8.8.8/32"}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "scan.targets[1]") {
		t.Fatalf("Validate() = %v, want the index of the second target", err)
	}
}

func TestSecretHidesValue(t *testing.T) {
	const raw = "hunter2-community"
	s := Secret(raw)

	if got := s.String(); got != "***" {
		t.Errorf("String() = %q", got)
	}
	if got := fmt.Sprintf("%v %s %q %+v %#v", s, s, s, s, s); strings.Contains(got, raw) {
		t.Errorf("fmt leaked the value: %s", got)
	}

	b, err := json.Marshal(struct{ Community Secret }{s})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), raw) || !strings.Contains(string(b), `"***"`) {
		t.Errorf("JSON = %s", b)
	}

	for name, h := range map[string]func(*bytes.Buffer) slog.Handler{
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
	} {
		var buf bytes.Buffer
		l := slog.New(h(&buf))
		l.Info("direct", "community", s)
		l.Info("nested", "snmp", SNMPConfig{Communities: []Secret{s}})
		l.Info("whole config", "config", Config{SNMP: SNMPConfig{Communities: []Secret{s}}})
		if strings.Contains(buf.String(), raw) {
			t.Errorf("slog %s handler leaked the value: %s", name, buf.String())
		}
		if !strings.Contains(buf.String(), "***") {
			t.Errorf("slog %s handler printed no mask: %s", name, buf.String())
		}
	}
}

func TestSecretFieldsInLoadedConfigStayMasked(t *testing.T) {
	path := writeConfig(t, 0o600, "snmp:\n  communities: [topsecret-community]\nssh:\n  hosts:\n    - {address: 192.168.1.10, user: audit, keyFile: /home/u/topsecret-key}\n")
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	out := fmt.Sprintf("%+v", cfg)
	if strings.Contains(out, "topsecret") {
		t.Errorf("config printout leaked a secret: %s", out)
	}
	if string(cfg.SNMP.Communities[0]) != "topsecret-community" {
		t.Errorf("the loaded secret value was changed")
	}
}

func writeConfig(t *testing.T, mode os.FileMode, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "netscribe.yaml")
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // WriteFile applies the umask
		t.Fatal(err)
	}
	return path
}

func TestExampleConfigIsValid(t *testing.T) {
	cfg, err := Load("../../netscribe.example.yaml", true)
	if err != nil {
		t.Fatalf("netscribe.example.yaml: %v", err)
	}
	if len(cfg.Scan.Targets) == 0 || len(cfg.SSH.Hosts) == 0 {
		t.Errorf("example config lost its sections: %+v", cfg)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, 0o600, "scan:\n  targets: [10.0.0.0/24]\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Errorf("Listen = %q, want the loopback default", cfg.Listen)
	}
	if cfg.Auth.MinPasswordLength != 12 || cfg.Auth.Argon2.MemoryKiB != 65536 {
		t.Errorf("auth defaults missing: %+v", cfg.Auth)
	}
	if cfg.Scan.Interval.Std() != 15*time.Minute {
		t.Errorf("Interval = %v", cfg.Scan.Interval.Std())
	}
}

func TestLoadEmptyFileUsesDefaults(t *testing.T) {
	if _, err := Load(writeConfig(t, 0o600, ""), false); err != nil {
		t.Fatalf("empty file: %v", err)
	}
}

func TestLoadIntervalZeroAndDurations(t *testing.T) {
	cfg, err := Load(writeConfig(t, 0o600, "scan:\n  interval: 0\nsnmp:\n  timeout: 5s\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scan.Interval != 0 {
		t.Errorf("Interval = %v, want 0", cfg.Scan.Interval.Std())
	}
	if cfg.SNMP.Timeout.Std() != 5*time.Second {
		t.Errorf("Timeout = %v", cfg.SNMP.Timeout.Std())
	}
}

func TestLoadEmptyPortsTurnsPortScanOff(t *testing.T) {
	cfg, err := Load(writeConfig(t, 0o600, "scan:\n  ports: []\n"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Scan.Ports) != 0 {
		t.Errorf("Ports = %v, want none", cfg.Scan.Ports)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"unknown key", "scna:\n  targets: [10.0.0.0/24]\n", "scna"},
		{"broken yaml", "scan: [unclosed\n", "parse config"},
		{"bad duration", "scan:\n  interval: soon\n", "not a valid duration"},
		{"validation error", "scan:\n  targets: [8.8.8.8/32]\n", "target is outside private ranges"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, 0o600, tt.body), false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() = %v, want an error containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml"), false); err == nil {
		t.Fatal("Load() succeeded for a missing file")
	}
}

func TestLoadErrorDoesNotEchoValues(t *testing.T) {
	// A community given as a mapping triggers a yaml type error that quotes scalars.
	path := writeConfig(t, 0o600, "snmp:\n  communities: topsecret-community\n")
	_, err := Load(path, false)
	if err == nil {
		t.Fatal("Load() accepted a string where a list is required")
	}
	if strings.Contains(err.Error(), "topsecret") {
		t.Errorf("error leaked a value: %v", err)
	}
}

func TestLoadRefusesLooseFilePermissions(t *testing.T) {
	path := writeConfig(t, 0o644, "")
	_, err := Load(path, false)
	if err == nil || !strings.Contains(err.Error(), "wider than 0600") {
		t.Fatalf("Load() = %v, want a permissions error", err)
	}
	if !strings.Contains(err.Error(), "--allow-loose-permissions") {
		t.Errorf("error does not name the override flag: %v", err)
	}
}

func TestLoadAllowsLoosePermissionsWithFlag(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	if _, err := Load(writeConfig(t, 0o640, ""), true); err != nil {
		t.Fatalf("Load() with allowLoose = %v", err)
	}
	if !strings.Contains(buf.String(), "wider than 0600") {
		t.Errorf("no warning logged: %q", buf.String())
	}
}

func TestLoadAcceptsOwnerOnlyPermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400} {
		if _, err := Load(writeConfig(t, mode, ""), false); err != nil {
			t.Errorf("mode %04o: %v", mode, err)
		}
	}
}
