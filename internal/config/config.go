// Package config loads and validates the service configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/netip"
	"os"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Secret holds a value that must never reach logs, API responses or exports.
// Every formatting path prints a mask instead of the value.
type Secret string

const secretMask = "***"

func (Secret) String() string   { return secretMask }
func (Secret) GoString() string { return `"` + secretMask + `"` }

func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + secretMask + `"`), nil }

func (Secret) MarshalYAML() (any, error) { return secretMask, nil }

func (Secret) LogValue() slog.Value { return slog.StringValue(secretMask) }

// Duration is a time.Duration that accepts a bare 0 in YAML.
// yaml.v3 rejects an integer for time.Duration, but the config documents
// "interval: 0" as the way to turn the schedule off.
type Duration time.Duration

// Std returns the value as a time.Duration.
func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: duration must be a scalar", n.Line)
	}
	if n.Value == "0" {
		*d = 0
		return nil
	}
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: value is not a valid duration", n.Line)
	}
	*d = Duration(v)
	return nil
}

type Config struct {
	Listen             string          `yaml:"listen"`
	Database           string          `yaml:"database"`
	AllowPublicTargets bool            `yaml:"allowPublicTargets"`
	Scan               ScanConfig      `yaml:"scan"`
	SNMP               SNMPConfig      `yaml:"snmp"`
	SSH                SSHConfig       `yaml:"ssh"`
	Docker             DockerConfig    `yaml:"docker"`
	History            HistoryConfig   `yaml:"history"`
	Audit              AuditConfig     `yaml:"audit"`
	Auth               AuthConfig      `yaml:"auth"`
	TLS                TLSConfig       `yaml:"tls"`
	TrustedProxies     []string        `yaml:"trustedProxies"`
	Discovery          DiscoveryConfig `yaml:"discovery"`
	Export             ExportConfig    `yaml:"export"`
}

type ScanConfig struct {
	Targets  []string `yaml:"targets"`
	Interval Duration `yaml:"interval"`
	Ports    []int    `yaml:"ports"`
}

type SNMPConfig struct {
	Communities []Secret `yaml:"communities"`
	Timeout     Duration `yaml:"timeout"`
}

type SSHConfig struct {
	KnownHosts string    `yaml:"knownHosts"`
	Hosts      []SSHHost `yaml:"hosts"`
}

type SSHHost struct {
	Address string `yaml:"address"`
	User    string `yaml:"user"`
	KeyFile Secret `yaml:"keyFile"`
}

type DockerConfig struct {
	Enabled bool   `yaml:"enabled"`
	Socket  string `yaml:"socket"`
}

type HistoryConfig struct {
	KeepScans int `yaml:"keepScans"`
}

type AuditConfig struct {
	KeepDays int `yaml:"keepDays"`
}

type AuthConfig struct {
	SessionIdleTimeout Duration     `yaml:"sessionIdleTimeout"`
	SessionMaxAge      Duration     `yaml:"sessionMaxAge"`
	MinPasswordLength  int          `yaml:"minPasswordLength"`
	MaxFailedLogins    int          `yaml:"maxFailedLogins"`
	LockoutDuration    Duration     `yaml:"lockoutDuration"`
	Argon2             Argon2Config `yaml:"argon2"`
}

type Argon2Config struct {
	MemoryKiB   uint32 `yaml:"memoryKiB"`
	Iterations  uint32 `yaml:"iterations"`
	Parallelism uint8  `yaml:"parallelism"`
}

type TLSConfig struct {
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
}

type DiscoveryConfig struct {
	RouterTables       bool `yaml:"routerTables"`
	ProbeRoutedTargets bool `yaml:"probeRoutedTargets"`
}

type ExportConfig struct {
	ChromePath string `yaml:"chromePath"`
}

// defaults returns the values used for every key the file leaves out.
// The auth block must be complete, because Validate enforces minimums on it.
func defaults() Config {
	return Config{
		Listen:   "127.0.0.1:8080",
		Database: "./netscribe.db",
		Scan: ScanConfig{
			Interval: Duration(15 * time.Minute),
			Ports:    []int{22, 53, 80, 443, 445, 3389, 8080},
		},
		SNMP:    SNMPConfig{Timeout: Duration(2 * time.Second)},
		SSH:     SSHConfig{KnownHosts: "~/.ssh/known_hosts"},
		Docker:  DockerConfig{Socket: "/var/run/docker.sock"},
		History: HistoryConfig{KeepScans: 200},
		Audit:   AuditConfig{KeepDays: 365},
		Auth: AuthConfig{
			SessionIdleTimeout: Duration(8 * time.Hour),
			SessionMaxAge:      Duration(168 * time.Hour),
			MinPasswordLength:  12,
			MaxFailedLogins:    5,
			LockoutDuration:    Duration(15 * time.Minute),
			Argon2:             Argon2Config{MemoryKiB: 65536, Iterations: 3, Parallelism: 2},
		},
		Discovery: DiscoveryConfig{RouterTables: true},
	}
}

// Load reads, parses and validates the config file at path.
// The file holds secrets, so permissions wider than 0600 are refused
// unless allowLoose is set (bind mounts in Docker cannot always meet it).
func Load(path string, allowLoose bool) (Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if err := checkPermissions(path, info.Mode(), allowLoose); err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // the path comes from the --config flag
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	cfg := defaults()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	// Unknown keys are errors: a typo in a security setting must not pass silently.
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parse config: %s", redact(err.Error()))
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func checkPermissions(path string, mode fs.FileMode, allowLoose bool) error {
	perm := mode.Perm()
	if perm&0o077 == 0 {
		return nil
	}
	if allowLoose {
		slog.Warn("config file permissions are wider than 0600", "path", path, "mode", fmt.Sprintf("%04o", perm))
		return nil
	}
	return fmt.Errorf("config file permissions %04o are wider than 0600: run chmod 600 %s or pass --allow-loose-permissions", perm, path)
}

var quotedValue = regexp.MustCompile("`[^`]*`")

// redact removes scalar values that yaml.v3 quotes in type errors,
// because the offending value can be a community or a key path.
func redact(msg string) string { return quotedValue.ReplaceAllString(msg, "value") }

var (
	errTargetCIDR   = errors.New("target is not a valid CIDR")
	errTargetWide   = errors.New("target is wider than /16")
	errTargetPublic = errors.New("target is outside private ranges")
	errInterval     = errors.New("scan interval is below 1m")
	errSNMPTimeout  = errors.New("snmp timeout is out of range")
	errSSHKey       = errors.New("ssh host has no key file")
	errSession      = errors.New("session timeout is out of range")
	errArgon2       = errors.New("argon2 parameters are below the minimum")
	errTLS          = errors.New("tls needs both certFile and keyFile")

	// The messages below are not in section 8 of tech.md yet (see CONTRACT GAP).
	errMinPassword = errors.New("min password length is below 12")
	errMaxFailed   = errors.New("max failed logins is out of range")
	errProxyCIDR   = errors.New("trusted proxy is not a valid CIDR")
)

var rfc1918 = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

// Validate checks every rule of section 8 and returns all violations joined.
func (c Config) Validate() error {
	var errs []error
	fail := func(where string, err error) {
		errs = append(errs, fmt.Errorf("%s: %w", where, err))
	}

	for i, t := range c.Scan.Targets {
		if err := checkTarget(t, c.AllowPublicTargets); err != nil {
			fail(fmt.Sprintf("scan.targets[%d] %q", i, t), err)
		}
	}
	if iv := c.Scan.Interval.Std(); iv != 0 && iv < time.Minute {
		fail("scan.interval", errInterval)
	}
	if t := c.SNMP.Timeout.Std(); t < time.Second || t > 30*time.Second {
		fail("snmp.timeout", errSNMPTimeout)
	}
	for i, h := range c.SSH.Hosts {
		if h.KeyFile == "" {
			fail(fmt.Sprintf("ssh.hosts[%d]", i), errSSHKey)
		}
	}

	idle, maxAge := c.Auth.SessionIdleTimeout.Std(), c.Auth.SessionMaxAge.Std()
	if idle < 15*time.Minute || idle > 24*time.Hour || maxAge < idle || maxAge > 720*time.Hour {
		fail("auth.sessionIdleTimeout, auth.sessionMaxAge", errSession)
	}
	if c.Auth.MinPasswordLength < 12 {
		fail("auth.minPasswordLength", errMinPassword)
	}
	if c.Auth.MaxFailedLogins < 3 || c.Auth.MaxFailedLogins > 20 {
		fail("auth.maxFailedLogins", errMaxFailed)
	}
	if a := c.Auth.Argon2; a.MemoryKiB < 19456 || a.Iterations < 2 || a.Parallelism < 1 {
		fail("auth.argon2", errArgon2)
	}

	if (c.TLS.CertFile == "") != (c.TLS.KeyFile == "") {
		fail("tls", errTLS)
	}
	for i, p := range c.TrustedProxies {
		if _, err := netip.ParsePrefix(p); err != nil {
			fail(fmt.Sprintf("trustedProxies[%d] %q", i, p), errProxyCIDR)
		}
	}
	return errors.Join(errs...)
}

// checkTarget accepts IPv4 CIDRs only: discovery relies on ARP, which has no IPv6 form.
func checkTarget(t string, allowPublic bool) error {
	p, err := netip.ParsePrefix(t)
	if err != nil || !p.Addr().Is4() {
		return errTargetCIDR
	}
	if p.Bits() < 16 {
		return errTargetWide
	}
	if !allowPublic && !isPrivate(p) {
		return errTargetPublic
	}
	return nil
}

func isPrivate(p netip.Prefix) bool {
	for _, r := range rfc1918 {
		if r.Bits() <= p.Bits() && r.Contains(p.Addr()) {
			return true
		}
	}
	return false
}
