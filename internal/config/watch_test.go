package config

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func testWatcher(t *testing.T, body string, log *slog.Logger) (*Watcher, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "netscribe.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	w := NewWatcher(path, false, cfg, log)
	w.delay = 20 * time.Millisecond
	return w, path
}

func TestReloadAppliesTheRuntimeSections(t *testing.T) {
	w, path := testWatcher(t, "scan:\n  targets: [192.168.1.0/24]\n  interval: 15m\n", quietLog())
	body := "scan:\n  targets: [10.0.0.0/24]\n  interval: 30m\nhistory:\n  keepScans: 5\ndocker:\n  enabled: true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := w.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if got := w.Current(); got.Scan.Interval.Std() != 30*time.Minute || got.Scan.Targets[0] != "10.0.0.0/24" || got.History.KeepScans != 5 || !got.Docker.Enabled {
		t.Errorf("current = %+v", got.Scan)
	}
	if cfg.Scan.Interval.Std() != 30*time.Minute {
		t.Errorf("returned interval = %v", cfg.Scan.Interval.Std())
	}
}

func TestReloadKeepsTheKeysThatNeedARestart(t *testing.T) {
	var logs bytes.Buffer
	w, path := testWatcher(t, "listen: 127.0.0.1:8080\nscan:\n  targets: [192.168.1.0/24]\n", slog.New(slog.NewTextHandler(&logs, nil)))
	body := "listen: 0.0.0.0:9090\ndatabase: ./other.db\nauth:\n  minPasswordLength: 12\n  maxFailedLogins: 9\nscan:\n  targets: [192.168.1.0/24]\n  interval: 20m\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Reload(); err != nil {
		t.Fatal(err)
	}
	got := w.Current()
	if got.Listen != "127.0.0.1:8080" || got.Database != "./netscribe.db" || got.Auth.MaxFailedLogins != 5 {
		t.Errorf("a restart-only key changed: listen %q, database %q, max failed logins %d", got.Listen, got.Database, got.Auth.MaxFailedLogins)
	}
	if got.Scan.Interval.Std() != 20*time.Minute {
		t.Errorf("interval = %v, want the new 20m", got.Scan.Interval.Std())
	}
	for _, key := range []string{"listen", "database", "auth"} {
		if !strings.Contains(logs.String(), "key="+key) {
			t.Errorf("no restart warning for %s in %q", key, logs.String())
		}
	}
}

func TestReloadRefusesAFileThatBreaksTheKeptKeys(t *testing.T) {
	w, path := testWatcher(t, "scan:\n  targets: [192.168.1.0/24]\n", quietLog())
	// The flag is valid in the new file, but only a restart applies it.
	body := "allowPublicTargets: true\nscan:\n  targets: [8.8.8.8/32]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Reload(); err == nil {
		t.Fatal("public target was accepted without allowPublicTargets in force")
	}
	if got := w.Current().Scan.Targets; len(got) != 1 || got[0] != "192.168.1.0/24" {
		t.Errorf("targets = %v, want the previous ones", got)
	}
}

func TestReloadOfABrokenFileKeepsTheCurrentConfigAndHidesSecrets(t *testing.T) {
	w, path := testWatcher(t, "scan:\n  targets: [192.168.1.0/24]\n  interval: 15m\n", quietLog())
	for name, body := range map[string]string{
		"broken yaml":      "scan: [\n",
		"unknown key":      "snmp:\n  communities: [hunter2]\n  bogus: 1\n",
		"validation error": "snmp:\n  communities: [hunter2]\nscan:\n  targets: [nonsense]\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := w.Reload()
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%s: the error shows a secret: %v", name, err)
		}
		if got := w.Current().Scan.Interval.Std(); got != 15*time.Minute {
			t.Errorf("%s: interval = %v, want the previous 15m", name, got)
		}
	}
}

type reloads struct {
	ok     chan Config
	failed chan error
	hup    chan os.Signal
}

func runWatcher(t *testing.T, w *Watcher) *reloads {
	t.Helper()
	r := &reloads{ok: make(chan Config, 8), failed: make(chan error, 8), hup: make(chan os.Signal, 1)}
	ready := make(chan struct{})
	w.watching = func() { close(ready) }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx, r.hup, func(c Config) { r.ok <- c }, func(err error) { r.failed <- err })
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Run did not stop when its context ended")
		}
	})
	// The directory watch is added inside Run: a change made before that is never seen.
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Run ended before it started watching: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not start")
	}
	return r
}

// writeAtomic replaces the file in one step, like an editor does. os.WriteFile truncates
// first, so a reload that lands between the truncate and the write would read an empty
// file and fail.
func writeAtomic(t *testing.T, path, body string) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func waitConfig(t *testing.T, r *reloads) Config {
	t.Helper()
	select {
	case c := <-r.ok:
		return c
	case err := <-r.failed:
		t.Fatalf("reload failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("no reload within 3s")
	}
	return Config{}
}

func TestRunPicksUpAFileReplacedByRename(t *testing.T) {
	w, path := testWatcher(t, "scan:\n  targets: [192.168.1.0/24]\n  interval: 15m\n", quietLog())
	r := runWatcher(t, w)

	// An editor or mv writes a new file next to the config and renames it over.
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte("scan:\n  targets: [192.168.1.0/24]\n  interval: 45m\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	if got := waitConfig(t, r).Scan.Interval.Std(); got != 45*time.Minute {
		t.Errorf("interval = %v, want 45m", got)
	}
	if got := w.Current().Scan.Interval.Std(); got != 45*time.Minute {
		t.Errorf("current interval = %v, want 45m", got)
	}
}

func TestRunReloadsOnceForABurstOfWrites(t *testing.T) {
	w, path := testWatcher(t, "scan:\n  targets: [192.168.1.0/24]\n", quietLog())
	w.delay = 300 * time.Millisecond
	r := runWatcher(t, w)

	// No pauses between the writes: a pause longer than the delay, which a loaded
	// runner can cause, would end the burst early and count as a second reload.
	for i := 0; i < 5; i++ {
		writeAtomic(t, path, "scan:\n  targets: [192.168.1.0/24]\n  interval: 20m\n")
	}
	waitConfig(t, r)
	select {
	case <-r.ok:
		t.Error("a second reload followed the burst")
	case err := <-r.failed:
		t.Errorf("reload failed: %v", err)
	case <-time.After(700 * time.Millisecond):
	}
}

func TestRunReportsABrokenFileAndKeepsWorking(t *testing.T) {
	w, path := testWatcher(t, "scan:\n  targets: [192.168.1.0/24]\n  interval: 15m\n", quietLog())
	r := runWatcher(t, w)

	writeAtomic(t, path, "scan: [\n")
	select {
	case err := <-r.failed:
		if err == nil {
			t.Error("nil error reported")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no error reported for a broken file")
	}
	if got := w.Current().Scan.Interval.Std(); got != 15*time.Minute {
		t.Errorf("interval = %v, want the previous 15m", got)
	}

	writeAtomic(t, path, "scan:\n  targets: [192.168.1.0/24]\n  interval: 25m\n")
	if got := waitConfig(t, r).Scan.Interval.Std(); got != 25*time.Minute {
		t.Errorf("interval = %v after the repair, want 25m", got)
	}
}

func TestRunReloadsOnSIGHUP(t *testing.T) {
	w, path := testWatcher(t, "scan:\n  targets: [192.168.1.0/24]\n  interval: 15m\n", quietLog())
	// The file changes before the watch starts, so only the signal can trigger the reload.
	writeAtomic(t, path, "scan:\n  targets: [192.168.1.0/24]\n  interval: 50m\n")
	r := runWatcher(t, w)
	r.hup <- os.Interrupt
	if got := waitConfig(t, r).Scan.Interval.Std(); got != 50*time.Minute {
		t.Errorf("interval = %v, want 50m", got)
	}
}
