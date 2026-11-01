// Package config loads and validates the service configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	Updates            UpdatesConfig   `yaml:"updates"`
	Metrics            MetricsConfig   `yaml:"metrics"`
	Notify             NotifyConfig    `yaml:"notify"`
}

type ScanConfig struct {
	Targets  []string `yaml:"targets"`
	Interval Duration `yaml:"interval"`
	Ports    []int    `yaml:"ports"`
	// PortTimeout is how long one TCP connection to a port may take before the port counts as closed.
	PortTimeout Duration `yaml:"portTimeout"`
	// Concurrency is how many TCP connections the port scan holds open at one moment.
	Concurrency int `yaml:"concurrency"`
	// RetryAfter is the wait before a scheduled scan that failed is tried again. 0 turns the retry off.
	RetryAfter Duration `yaml:"retryAfter"`
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
	RouterTables       bool   `yaml:"routerTables"`
	HTTPHeaders        bool   `yaml:"httpHeaders"` // HEAD / on the open web ports
	MDNS               bool   `yaml:"mdns"`        // one PTR query in each local segment
	SSDP               bool   `yaml:"ssdp"`        // one M-SEARCH in each local segment
	Nmap               bool   `yaml:"nmap"`        // ask an installed nmap about devices nothing else could type
	NmapScope          string `yaml:"nmapScope"`   // untyped (default) or all
	NmapPath           string `yaml:"nmapPath"`    // empty: look for nmap in PATH
	ProbeRoutedTargets bool   `yaml:"probeRoutedTargets"`
}

type ExportConfig struct {
	ChromePath string `yaml:"chromePath"`
}

// UpdatesConfig controls the check for a newer release. The check asks one fixed GitHub
// address and sends nothing about this installation.
type UpdatesConfig struct {
	Check bool `yaml:"check"`
}

// MetricsConfig switches the Prometheus route on. It is off until somebody wants it.
type MetricsConfig struct {
	Enabled bool `yaml:"enabled"`
}

// The events a channel can be told about.
const (
	EventDeviceAdded   = "device_added"
	EventDeviceOffline = "device_offline"
	EventPortOpened    = "port_opened"
	EventRiskyService  = "risky_service"
	EventScanFailed    = "scan_failed"
)

// The kinds of notification channel.
const (
	ChannelWebhook  = "webhook"
	ChannelTelegram = "telegram"
	ChannelEmail    = "email"
)

// NotifyConfig says which events are sent and where. Every address, token and password in
// it is a Secret: the panel and the API show channel names only.
type NotifyConfig struct {
	Events   []string        `yaml:"events"`
	Channels []ChannelConfig `yaml:"channels"`
}

// ChannelConfig is one place to send messages to. Which fields count depends on Type.
type ChannelConfig struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`

	URL Secret `yaml:"url"` // webhook

	BotToken Secret `yaml:"botToken"` // telegram
	ChatID   Secret `yaml:"chatId"`

	Host     string   `yaml:"host"` // email
	Port     int      `yaml:"port"`
	User     string   `yaml:"user"`
	Password Secret   `yaml:"password"`
	From     string   `yaml:"from"`
	To       []string `yaml:"to"`
}

var channelName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// validate checks the notify block. A single problem refuses the whole config, so a typo
// never silently turns an alert off.
func (n NotifyConfig) validate(fail func(where string, err error)) {
	known := map[string]bool{EventDeviceAdded: true, EventDeviceOffline: true, EventPortOpened: true, EventRiskyService: true, EventScanFailed: true}
	seenEvent := map[string]bool{}
	for i, e := range n.Events {
		where := fmt.Sprintf("notify.events[%d]", i)
		switch {
		case !known[e]:
			fail(where, errors.New("unknown event"))
		case seenEvent[e]:
			fail(where, errors.New("listed twice"))
		}
		seenEvent[e] = true
	}
	seenName := map[string]bool{}
	for i, ch := range n.Channels {
		where := fmt.Sprintf("notify.channels[%d]", i)
		if !channelName.MatchString(ch.Name) {
			fail(where, errors.New("name must be 1 to 32 letters, digits, - or _"))
		} else if seenName[ch.Name] {
			fail(where, errors.New("name is used twice"))
		}
		seenName[ch.Name] = true
		switch ch.Type {
		case ChannelWebhook:
			u, err := url.Parse(string(ch.URL))
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				fail(where, errors.New("url must be an http or https address"))
			}
		case ChannelTelegram:
			if ch.BotToken == "" || ch.ChatID == "" {
				fail(where, errors.New("botToken and chatId are required"))
			}
		case ChannelEmail:
			if ch.Host == "" || ch.Port < 1 || ch.Port > 65535 {
				fail(where, errors.New("host and a port from 1 to 65535 are required"))
			}
			if (ch.User == "") != (ch.Password == "") {
				fail(where, errors.New("user and password go together"))
			}
			if _, err := mail.ParseAddress(ch.From); err != nil {
				fail(where, errors.New("from is not an address"))
			}
			if len(ch.To) == 0 {
				fail(where, errors.New("to needs at least one address"))
			}
			for _, a := range ch.To {
				if _, err := mail.ParseAddress(a); err != nil {
					fail(where, errors.New("to holds something that is not an address"))
					break
				}
			}
		default:
			fail(where, errors.New("type must be webhook, telegram or email"))
		}
	}
}

// defaults returns the values used for every key the file leaves out.
// The auth block must be complete, because Validate enforces minimums on it.
func defaults() Config {
	return Config{
		Listen:   "127.0.0.1:8080",
		Database: "./netscribe.db",
		Scan: ScanConfig{
			Interval: Duration(15 * time.Minute),
			Ports:    []int{21, 22, 23, 53, 80, 443, 445, 515, 554, 631, 3389, 5900, 8080, 9100},

			PortTimeout: Duration(500 * time.Millisecond),
			Concurrency: 256,
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
		Discovery: DiscoveryConfig{RouterTables: true, HTTPHeaders: true, MDNS: true, SSDP: true, NmapScope: "untyped"},
		Updates:   UpdatesConfig{Check: true},
		Notify:    NotifyConfig{Events: []string{EventDeviceAdded, EventScanFailed}},
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
	errTargetIPv4   = errors.New("only IPv4 targets are supported")
	errTargetWide   = errors.New("target is wider than /16")
	errTargetPublic = errors.New("target is outside private ranges")
	errInterval     = errors.New("scan interval is below 1m")
	errSNMPTimeout  = errors.New("snmp timeout is out of range")
	errPortTimeout  = errors.New("scan.portTimeout must be between 100ms and 10s")
	errConcurrency  = errors.New("scan.concurrency must be between 1 and 512")
	errRetryAfter   = errors.New("scan.retryAfter must be 0 or between 1m and 24h")
	errSSHKey       = errors.New("ssh host has no key file")
	errSession      = errors.New("session timeout is out of range")
	errArgon2       = errors.New("argon2 parameters are below the minimum")
	errTLS          = errors.New("tls needs both certFile and keyFile")

	errMinPassword = errors.New("min password length is below 12")
	errMaxFailed   = errors.New("max failed logins is out of range")
	errProxyCIDR   = errors.New("trusted proxy is not a valid CIDR")
	errListen      = errors.New("listen must be host:port with a port from 1 to 65535")
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

	if err := ValidateListen(c.Listen); err != nil {
		fail("listen", err)
	}

	for i, t := range c.Scan.Targets {
		if _, err := ParseTarget(t, c.AllowPublicTargets); err != nil {
			fail(fmt.Sprintf("scan.targets[%d] %q", i, t), err)
		}
	}
	if sc := c.Discovery.NmapScope; sc != "" && sc != "untyped" && sc != "all" {
		fail("discovery.nmapScope", errors.New("must be untyped or all"))
	}
	if p := c.Discovery.NmapPath; p != "" && !filepath.IsAbs(p) {
		fail("discovery.nmapPath", errors.New("must be an absolute path"))
	}
	if iv := c.Scan.Interval.Std(); iv != 0 && iv < time.Minute {
		fail("scan.interval", errInterval)
	}
	if t := c.Scan.PortTimeout.Std(); t < 100*time.Millisecond || t > 10*time.Second {
		fail("scan.portTimeout", errPortTimeout)
	}
	if n := c.Scan.Concurrency; n < 1 || n > 512 {
		fail("scan.concurrency", errConcurrency)
	}
	if r := c.Scan.RetryAfter.Std(); r != 0 && (r < time.Minute || r > 24*time.Hour) {
		fail("scan.retryAfter", errRetryAfter)
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
	c.Notify.validate(fail)
	return errors.Join(errs...)
}

// ValidateListen checks a listen address before the server binds it, so a typo such as
// "8080" without a colon is reported at load time and not after the workers started.
func ValidateListen(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errListen
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errListen
	}
	return nil
}

// ParseTarget parses and checks one scan target, from the config or from a CLI argument.
// It accepts IPv4 CIDRs only: discovery relies on ARP, which has no IPv6 form.
func ParseTarget(t string, allowPublic bool) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(strings.TrimSpace(t))
	if err != nil {
		return netip.Prefix{}, errTargetCIDR
	}
	if !p.Addr().Is4() {
		return netip.Prefix{}, errTargetIPv4
	}
	if p.Bits() < 16 {
		return netip.Prefix{}, errTargetWide
	}
	if !allowPublic && !isPrivate(p) {
		return netip.Prefix{}, errTargetPublic
	}
	return p, nil
}

func isPrivate(p netip.Prefix) bool {
	for _, r := range rfc1918 {
		if r.Bits() <= p.Bits() && r.Contains(p.Addr()) {
			return true
		}
	}
	return false
}
