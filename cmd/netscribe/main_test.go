package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := run(tt.args, &stderr); got != tt.code {
				t.Errorf("exit code = %d, want %d (stderr: %s)", got, tt.code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.stderr)
			}
		})
	}
}
