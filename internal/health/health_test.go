package health

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

type fakeInfo struct{ mode fs.FileMode }

func (f fakeInfo) Name() string       { return "x" }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return now }
func (f fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any           { return nil }

func probe() Probe {
	return Probe{
		Now:       func() time.Time { return now },
		Stat:      func(string) (fs.FileInfo, error) { return fakeInfo{0o640}, nil },
		ReadFile:  func(string) ([]byte, error) { return nil, os.ErrNotExist },
		LookPath:  func(string) (string, error) { return "", errors.New("not found") },
		FreeBytes: func(string) (uint64, error) { return 10 << 30, nil },
	}
}

func input() Input {
	cfg := config.Config{Database: "/var/lib/netscribe/db"}
	cfg.Scan.Targets = []string{"192.168.1.0/24"}
	cfg.Scan.Interval = config.Duration(15 * time.Minute)
	fin := now.Add(-5 * time.Minute)
	return Input{Config: cfg, ConfigPath: "/etc/netscribe/netscribe.yaml",
		Overview: model.Overview{LastScan: &model.Scan{Status: "done", StartedAt: fin.Add(-time.Minute), FinishedAt: &fin}}}
}

func byID(list []Check, id string) Check {
	for _, c := range list {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestAHealthyInstallHasNoWarnings(t *testing.T) {
	list := Run(context.Background(), probe(), input())
	if len(list) != 9 {
		t.Fatalf("%d checks", len(list))
	}
	for _, c := range list {
		if c.Status != OK || c.Title == "" || c.ID == "" {
			t.Errorf("%+v", c)
		}
	}
	if Worst(list) != OK {
		t.Error("Worst of a healthy list")
	}
}

func TestEachProblemShowsWithItsFix(t *testing.T) {
	cases := map[string]struct {
		id     string
		mod    func(p *Probe, in *Input)
		status Status
	}{
		"config readable by all": {"config.mode", func(p *Probe, _ *Input) {
			p.Stat = func(string) (fs.FileInfo, error) { return fakeInfo{0o644}, nil }
		}, Warn},
		"low disk":    {"disk.free", func(p *Probe, _ *Input) { p.FreeBytes = func(string) (uint64, error) { return 100 << 20, nil } }, Warn},
		"full disk":   {"disk.free", func(p *Probe, _ *Input) { p.FreeBytes = func(string) (uint64, error) { return 1 << 20, nil } }, Fail},
		"no targets":  {"scan.targets", func(_ *Probe, in *Input) { in.Config.Scan.Targets = nil }, Warn},
		"no scan yet": {"scan.recent", func(_ *Probe, in *Input) { in.Overview.LastScan = nil }, Warn},
		"failed scan": {"scan.recent", func(_ *Probe, in *Input) { in.Overview.LastScan.Status = "failed" }, Warn},
		"stale scan": {"scan.recent", func(_ *Probe, in *Input) {
			old := now.Add(-3 * time.Hour)
			in.Overview.LastScan.FinishedAt = &old
		}, Warn},
		"nmap missing": {"nmap", func(_ *Probe, in *Input) { in.Config.Discovery.Nmap = true }, Warn},
		"docker missing": {"docker.socket", func(p *Probe, in *Input) {
			in.Config.Docker = config.DockerConfig{Enabled: true, Socket: "/run/docker.sock"}
			p.Stat = func(path string) (fs.FileInfo, error) {
				if path == "/run/docker.sock" {
					return nil, os.ErrNotExist
				}
				return fakeInfo{0o640}, nil
			}
		}, Warn},
		"unreadable cert": {"tls.cert", func(_ *Probe, in *Input) { in.Config.TLS.CertFile = "/etc/c.pem" }, Fail},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p, in := probe(), input()
			c.mod(&p, &in)
			got := byID(Run(context.Background(), p, in), c.id)
			if got.Status != c.status {
				t.Fatalf("status = %q, want %q (%+v)", got.Status, c.status, got)
			}
			if got.Fix == "" || got.Detail == "" {
				t.Errorf("a problem without a detail or a fix: %+v", got)
			}
		})
	}
}

func TestACheckThatPanicsOrHangsDoesNotStopTheOthers(t *testing.T) {
	p, in := probe(), input()
	p.FreeBytes = func(string) (uint64, error) { panic("boom") }
	list := Run(context.Background(), p, in)
	if got := byID(list, "disk.free"); got.Status != Fail || strings.Contains(got.Detail, "boom") {
		t.Errorf("panic: %+v", got)
	}
	if byID(list, "scan.targets").Status != OK {
		t.Error("the other checks did not run")
	}
}

func TestNoCheckPrintsASecret(t *testing.T) {
	p, in := probe(), input()
	in.Config.SNMP.Communities = []config.Secret{"s3cret-community"}
	in.Config.TLS.CertFile = "/etc/private/cert.pem"
	for _, c := range Run(context.Background(), p, in) {
		if strings.Contains(c.Detail+c.Fix, "s3cret") {
			t.Errorf("%s leaks a secret: %+v", c.ID, c)
		}
	}
}

func TestWorst(t *testing.T) {
	if Worst([]Check{{Status: OK}, {Status: Warn}}) != Warn || Worst([]Check{{Status: Warn}, {Status: Fail}}) != Fail || Worst(nil) != OK {
		t.Error("Worst")
	}
}
