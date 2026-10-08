package main

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

func TestInsecureListen(t *testing.T) {
	tests := []struct {
		name string
		edit func(*config.Config)
		want bool
	}{
		{"loopback v4", func(c *config.Config) { c.Listen = "127.0.0.1:8080" }, false},
		{"loopback v6", func(c *config.Config) { c.Listen = "[::1]:8080" }, false},
		{"localhost", func(c *config.Config) { c.Listen = "localhost:8080" }, false},
		{"all interfaces", func(c *config.Config) { c.Listen = ":8080" }, true},
		{"lan address", func(c *config.Config) { c.Listen = "192.168.1.5:8080" }, true},
		{"unspecified v4", func(c *config.Config) { c.Listen = "0.0.0.0:8080" }, true},
		{"lan address with TLS", func(c *config.Config) {
			c.Listen = "0.0.0.0:8443"
			c.TLS = config.TLSConfig{CertFile: "c", KeyFile: "k"}
		}, false},
		{"lan address behind a proxy", func(c *config.Config) {
			c.Listen = "192.168.1.5:8080"
			c.TrustedProxies = []string{"192.168.1.1/32"}
		}, false},
		{"malformed address", func(c *config.Config) { c.Listen = "nonsense" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c config.Config
			tt.edit(&c)
			if got := insecureListen(c); got != tt.want {
				t.Errorf("insecureListen(%q) = %v, want %v", c.Listen, got, tt.want)
			}
		})
	}
}

func TestNewServerLimits(t *testing.T) {
	srv := newServer(config.Config{Listen: "127.0.0.1:1"}, nil)
	if srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 30*time.Second ||
		srv.WriteTimeout != 60*time.Second || srv.IdleTimeout != 120*time.Second {
		t.Errorf("timeouts = %v %v %v %v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != 16<<10 {
		t.Errorf("MaxHeaderBytes = %d, want 16 KiB", srv.MaxHeaderBytes)
	}
	if srv.TLSConfig == nil || srv.TLSConfig.MinVersion < 0x0303 { // 0x0303 is TLS 1.2
		t.Errorf("TLS minimum version = %v, want TLS 1.2 or higher", srv.TLSConfig)
	}
}

func TestLoggerMasksSensitiveKeys(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf)
	l.Info("event",
		"password", "pw-value",
		"api_token", "tok-value",
		"Cookie", "cookie-value",
		"Authorization", "auth-value",
		"snmp_community", "comm-value",
		"clientSecret", "secret-value",
		"addr", "127.0.0.1:8080",
	)
	out := buf.String()
	for _, leaked := range []string{"pw-value", "tok-value", "cookie-value", "auth-value", "comm-value", "secret-value"} {
		if strings.Contains(out, leaked) {
			t.Errorf("log leaked %q: %s", leaked, out)
		}
	}
	if !strings.Contains(out, "127.0.0.1:8080") {
		t.Errorf("harmless attribute was masked: %s", out)
	}
}

func TestLoggerMasksSensitiveKeysInGroups(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf).Info("event", slog.Group("req", slog.String("authorization", "Bearer abc")))
	if strings.Contains(buf.String(), "Bearer abc") {
		t.Errorf("grouped attribute leaked: %s", buf.String())
	}
}

func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"no command", nil, 2, "usage:"},
		{"unknown command", []string{"frobnicate"}, 2, "unknown command"},
		{"help", []string{"help"}, 0, "usage:"},
		{"serve help", []string{"serve", "-h"}, 0, "-config"},
		{"serve with a bad flag", []string{"serve", "--nope"}, 1, "flag provided but not defined"},
		{"serve with a missing config", []string{"serve", "--config", "/nonexistent/netscribe.yaml"}, 1, "read config"},
		{"scan with a missing config", []string{"scan", "--config", "/nonexistent/netscribe.yaml"}, 1, "read config"},
		{"devices with a missing config", []string{"devices", "--config", "/nonexistent/netscribe.yaml"}, 1, "read config"},
		{"scan help", []string{"scan", "-h"}, 0, "-config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := run(tt.args, io.Discard, &stderr); got != tt.code {
				t.Errorf("exit code = %d, want %d (stderr: %s)", got, tt.code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.stderr)
			}
		})
	}
}

func TestScanTargets(t *testing.T) {
	cfg := config.Config{Scan: config.ScanConfig{Targets: []string{"192.168.1.0/24"}}}

	got, err := scanTargets(nil, cfg)
	if err != nil || len(got) != 1 || got[0].String() != "192.168.1.0/24" {
		t.Fatalf("config targets: %v, %v", got, err)
	}
	got, err = scanTargets([]string{"10.1.0.0/24", "10.2.0.0/24"}, cfg)
	if err != nil || len(got) != 2 || got[0].String() != "10.1.0.0/24" {
		t.Fatalf("arguments must replace the config targets: %v, %v", got, err)
	}

	_, err = scanTargets([]string{"8.8.8.8/32"}, cfg)
	if err == nil || !strings.Contains(err.Error(), "target is outside private ranges") {
		t.Errorf("public target = %v, want the private-range error", err)
	}
	cfg.AllowPublicTargets = true
	if _, err := scanTargets([]string{"8.8.8.8/32"}, cfg); err != nil {
		t.Errorf("public target with allowPublicTargets = %v", err)
	}
	if _, err := scanTargets([]string{"10.0.0.0/8"}, cfg); err == nil || !strings.Contains(err.Error(), "wider than /16") {
		t.Errorf("wide target = %v", err)
	}
	if _, err := scanTargets([]string{"192.168.1.0/24", "oops"}, cfg); err == nil || !strings.Contains(err.Error(), "oops") {
		t.Errorf("one bad argument must fail the whole scan and name the argument: %v", err)
	}
	if _, err := scanTargets(nil, config.Config{}); err == nil || !strings.Contains(err.Error(), "no scan targets") {
		t.Errorf("no targets = %v", err)
	}
}

func TestPrintable(t *testing.T) {
	for in, want := range map[string]string{
		"nas.lan":              "nas.lan",
		"Принтер":              "Принтер",
		"evil\x1b[2Jhost":      "evil?[2Jhost",
		"line\nbreak\ttab\x00": "line?break?tab?",
	} {
		if got := printable(in); got != want {
			t.Errorf("printable(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrintDevices(t *testing.T) {
	var buf bytes.Buffer
	err := printDevices(&buf, []model.Device{
		{IP: "192.168.1.1", MAC: "aa:bb:cc:00:00:01", Hostname: "router.lan", Online: true},
		{IP: "192.168.1.20", MAC: "aa:bb:cc:00:00:20", Hostname: "x\x1b[31m", Online: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("output:\n%s", buf.String())
	}
	for i, want := range [][]string{
		{"IP", "MAC", "HOSTNAME", "ONLINE"},
		{"192.168.1.1", "aa:bb:cc:00:00:01", "router.lan", "yes"},
		{"192.168.1.20", "aa:bb:cc:00:00:20", "x?[31m", "no"},
	} {
		got := strings.Fields(lines[i])
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("line %d = %v, want %v", i, got, want)
		}
	}
	if strings.Contains(buf.String(), "\x1b") {
		t.Error("an escape sequence reached the terminal output")
	}
}

func TestPrintDevicesEmptyHasHeaderOnly(t *testing.T) {
	var buf bytes.Buffer
	if err := printDevices(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), "\n") != 1 || !strings.HasPrefix(buf.String(), "IP") {
		t.Errorf("output = %q", buf.String())
	}
}
