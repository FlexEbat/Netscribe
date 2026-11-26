package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/api"
	"github.com/FlexEbat/Netscribe/internal/certgen"
	"github.com/FlexEbat/Netscribe/internal/config"
)

func TestTLSServiceMakesTheFilesAndSetsTheConfig(t *testing.T) {
	c := newCLI(t)
	svc := newTLSService(c.cfg, func() config.Config { return config.Config{} })

	st, err := svc.Status(context.Background())
	if err != nil || st.Enabled || st.Names == nil {
		t.Fatalf("status before = %+v, %v", st, err)
	}

	res, err := svc.GenerateSelfSigned(context.Background(), []string{"Panel.LAN", "10.0.0.5"}, 30, false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedRestart {
		t.Error("the first https needs a restart")
	}
	if strings.Join(res.Names, ",") != "panel.lan,10.0.0.5" || res.Fingerprint == "" || res.Subject != "CN=panel.lan" {
		t.Errorf("result = %+v", res)
	}
	dir := filepath.Join(filepath.Dir(c.cfg), "tls")
	if m := fileMode(t, filepath.Join(dir, certgen.KeyName)); m != 0o600 {
		t.Errorf("key mode %o", m)
	}
	cfg, err := config.Load(c.cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS.CertFile != filepath.Join(dir, certgen.CertName) || cfg.TLS.KeyFile != filepath.Join(dir, certgen.KeyName) {
		t.Errorf("tls = %+v", cfg.TLS)
	}

	st, err = svc.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || !st.SelfSigned || st.Fingerprint != res.Fingerprint || strings.Join(st.Names, ",") != "panel.lan,10.0.0.5" {
		t.Errorf("status after = %+v", st)
	}
}

func TestTLSServiceKeepsTheKeyOutOfTheResult(t *testing.T) {
	c := newCLI(t)
	svc := newTLSService(c.cfg, func() config.Config { return config.Config{} })
	res, err := svc.GenerateSelfSigned(context.Background(), []string{"a.lan"}, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(filepath.Dir(c.cfg), "tls", certgen.KeyName)) //nolint:gosec // a path inside the test directory
	if err != nil {
		t.Fatal(err)
	}
	body := res.Subject + strings.Join(res.Names, "") + res.Fingerprint
	if strings.Contains(body, "PRIVATE KEY") || strings.Contains(body, strings.TrimSpace(strings.Split(string(key), "\n")[1])) {
		t.Error("the result holds key material")
	}
}

func TestTLSServiceConflictAndReplace(t *testing.T) {
	c := newCLI(t)
	dir := filepath.Join(filepath.Dir(c.cfg), "tls")
	running := config.Config{}
	svc := newTLSService(c.cfg, func() config.Config { return running })
	first, err := svc.GenerateSelfSigned(context.Background(), []string{"a.lan"}, 0, false)
	if err != nil {
		t.Fatal(err)
	}

	var conflict api.TLSConflict
	if _, err := svc.GenerateSelfSigned(context.Background(), []string{"b.lan"}, 0, false); !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	// The service serves these files now: a renewal needs no restart.
	running.TLS.CertFile = filepath.Join(dir, certgen.CertName)
	running.TLS.KeyFile = filepath.Join(dir, certgen.KeyName)
	second, err := svc.GenerateSelfSigned(context.Background(), []string{"b.lan"}, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.NeedRestart {
		t.Error("a renewal of the served files needs no restart")
	}
	if second.Fingerprint == first.Fingerprint {
		t.Error("replace kept the certificate")
	}
	if _, err := os.Stat(filepath.Join(dir, certgen.CertName+".previous")); err != nil {
		t.Errorf("the old certificate was not kept: %v", err)
	}
}

func TestTLSServiceRefusals(t *testing.T) {
	c := newCLI(t)
	svc := newTLSService(c.cfg, func() config.Config { return config.Config{} })
	for name, tc := range map[string]struct {
		names []string
		days  int
	}{
		"bad name":      {[]string{"bad_name!"}, 0},
		"too many days": {[]string{"a.lan"}, 826},
		"negative days": {[]string{"a.lan"}, -1},
	} {
		t.Run(name, func(t *testing.T) {
			var refusal api.TLSRefusal
			if _, err := svc.GenerateSelfSigned(context.Background(), tc.names, tc.days, false); !errors.As(err, &refusal) {
				t.Fatalf("err = %v, want a refusal", err)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(c.cfg), "tls")); err == nil {
		t.Error("a refused request made the directory")
	}
	cfg, err := config.Load(c.cfg, false)
	if err != nil || cfg.TLS.CertFile != "" {
		t.Errorf("a refused request changed the config: %+v, %v", cfg.TLS, err)
	}
}

func TestTLSServiceDefaultsToTheNamesOfTheHost(t *testing.T) {
	c := newCLI(t)
	svc := newTLSService(c.cfg, func() config.Config { return config.Config{} })
	res, err := svc.GenerateSelfSigned(context.Background(), nil, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"localhost", "127.0.0.1", "::1"} {
		if !contains(res.Names, want) {
			t.Errorf("no %s in %v", want, res.Names)
		}
	}
}
