package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/certgen"
)

var fingerprintLine = regexp.MustCompile(`fingerprint: SHA-256 (\S+)`)

func fingerprintOf(t *testing.T, out string) string {
	t.Helper()
	m := fingerprintLine.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no fingerprint in %q", out)
	}
	return m[1]
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestCertGenerateWritesTheFilesAndKeepsTheConfig(t *testing.T) {
	c := newCLI(t)
	before, err := os.ReadFile(c.cfg) //nolint:gosec // a path inside the test directory
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := c.run("", "cert", "generate", "--name", "panel.lan", "--name", "10.0.0.5", "--days", "30")
	if code != 0 {
		t.Fatalf("code %d: %s", code, errOut)
	}
	dir := filepath.Join(filepath.Dir(c.cfg), "tls")
	if m := fileMode(t, filepath.Join(dir, certgen.CertName)); m != 0o644 {
		t.Errorf("certificate mode %o", m)
	}
	if m := fileMode(t, filepath.Join(dir, certgen.KeyName)); m != 0o600 {
		t.Errorf("key mode %o", m)
	}
	for _, want := range []string{"panel.lan", "10.0.0.5", "self-signed", "certFile: " + filepath.Join(dir, certgen.CertName)} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "PRIVATE KEY") {
		t.Error("the output shows key material")
	}
	after, err := os.ReadFile(c.cfg) //nolint:gosec // a path inside the test directory
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("generate without --set changed the config")
	}
}

func TestCertGenerateRefusesToOverwriteWithoutForce(t *testing.T) {
	c := newCLI(t)
	if code, _, errOut := c.run("", "cert", "generate", "--name", "a.lan"); code != 0 {
		t.Fatalf("first: %d %s", code, errOut)
	}
	code, _, errOut := c.run("", "cert", "generate", "--name", "b.lan")
	if code != 1 || !strings.Contains(errOut, "exist already") {
		t.Fatalf("second: code %d, %q", code, errOut)
	}
	if code, _, errOut := c.run("", "cert", "generate", "--name", "b.lan", "--force"); code != 0 {
		t.Fatalf("forced: %d %s", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.cfg), "tls", certgen.CertName+".previous")); err != nil {
		t.Errorf("the old certificate was not kept: %v", err)
	}
}

func TestCertGenerateRefusals(t *testing.T) {
	c := newCLI(t)
	for name, args := range map[string][]string{
		"bad name":        {"cert", "generate", "--name", "bad_name!"},
		"too many days":   {"cert", "generate", "--name", "a.lan", "--days", "826"},
		"no command":      {"cert"},
		"unknown command": {"cert", "wipe"},
	} {
		t.Run(name, func(t *testing.T) {
			if code, _, _ := c.run("", args...); code == 0 {
				t.Error("want a failure")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.cfg), "tls")); err == nil {
		t.Error("a refused command made the directory")
	}
}

func TestCertSetShowAndRenew(t *testing.T) {
	c := newCLI(t)
	if code, _, errOut := c.run("", "cert", "show"); code != 1 || !strings.Contains(errOut, "no tls.certFile") {
		t.Fatalf("show before set: %d %q", code, errOut)
	}
	if code, _, errOut := c.run("", "cert", "renew"); code != 1 || !strings.Contains(errOut, "no tls.certFile") {
		t.Fatalf("renew before set: %d %q", code, errOut)
	}
	code, out, errOut := c.run("", "cert", "generate", "--name", "panel.lan", "--name", "10.0.0.5", "--days", "40", "--set")
	if code != 0 {
		t.Fatalf("generate --set: %d %s", code, errOut)
	}
	first := fingerprintOf(t, out)
	cfgText, err := os.ReadFile(c.cfg) //nolint:gosec // a path inside the test directory
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfgText), "certFile:") || !strings.Contains(string(cfgText), "keyFile:") {
		t.Fatalf("the config does not name the files:\n%s", cfgText)
	}

	code, shown, errOut := c.run("", "cert", "show")
	if code != 0 {
		t.Fatalf("show: %d %s", code, errOut)
	}
	if fingerprintOf(t, shown) != first || !strings.Contains(shown, "panel.lan, 10.0.0.5") {
		t.Errorf("show differs from generate:\n%s", shown)
	}

	code, renewed, errOut := c.run("", "cert", "renew")
	if code != 0 {
		t.Fatalf("renew: %d %s", code, errOut)
	}
	if fingerprintOf(t, renewed) == first {
		t.Error("renew kept the certificate")
	}
	if !strings.Contains(renewed, "panel.lan, 10.0.0.5") || !strings.Contains(renewed, "no restart is needed") {
		t.Errorf("renew output:\n%s", renewed)
	}
	// The same length: 40 days.
	dir := filepath.Join(filepath.Dir(c.cfg), "tls")
	data, err := os.ReadFile(filepath.Join(dir, certgen.CertName)) //nolint:gosec // a path inside the test directory
	if err != nil {
		t.Fatal(err)
	}
	info, err := certgen.Describe(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := int(info.NotAfter.Sub(info.NotBefore).Hours()/24 + 0.5); got != 40 {
		t.Errorf("renewed for %d days, want 40", got)
	}
	if info.CommonName != "panel.lan" {
		t.Errorf("common name = %q", info.CommonName)
	}
	if m := fileMode(t, filepath.Join(dir, certgen.KeyName)); m != 0o600 {
		t.Errorf("key mode %o", m)
	}
}

func TestDefaultCertNames(t *testing.T) {
	for _, listen := range []string{"127.0.0.1:8080", "0.0.0.0:8443", "", "192.168.1.5:8080", "panel.lan:8080"} {
		names := defaultCertNames(listen)
		if len(names) > certgen.MaxNames {
			t.Errorf("%q: %d names", listen, len(names))
		}
		if _, err := certgen.NormalizeNames(names); err != nil {
			t.Errorf("%q: %v", listen, err)
		}
		have := map[string]bool{}
		for _, n := range names {
			have[n] = true
		}
		for _, want := range []string{"localhost", "127.0.0.1", "::1"} {
			if !have[want] {
				t.Errorf("%q: no %s in %v", listen, want, names)
			}
		}
	}
	if names := defaultCertNames("192.168.1.5:8080"); !contains(names, "192.168.1.5") {
		t.Errorf("the listen address is missing: %v", names)
	}
	if names := defaultCertNames("panel.lan:8080"); !contains(names, "panel.lan") {
		t.Errorf("the listen host is missing: %v", names)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
