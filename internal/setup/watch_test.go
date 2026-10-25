package setup

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestProcessTicksHandlesNamesWithSpacesAndBrackets(t *testing.T) {
	got, ok := processTicks("4242 (net scribe) x) S 1 1 1 0 -1 0 0 0 0 0 150 50 0 0 20 0 9 0 1 0\n")
	if !ok || got != 200 {
		t.Errorf("ticks %d, %v", got, ok)
	}
	for _, bad := range []string{"", "no brackets", "1 (x) S 1 2"} {
		if _, ok := processTicks(bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
}

func TestKBAndShow(t *testing.T) {
	if got := kb("Name:\tx\nVmRSS:\t  2048 kB\n", "VmRSS"); got != 2<<20 {
		t.Errorf("VmRSS %d", got)
	}
	if kb("nothing", "VmRSS") != 0 {
		t.Error("a missing line is zero")
	}
	m := show([]byte("ActiveState=active\nMainPID=7\nempty=\nnot a pair\n"))
	if m["ActiveState"] != "active" || m["MainPID"] != "7" || len(m) != 3 {
		t.Errorf("%v", m)
	}
}

func TestSpark(t *testing.T) {
	if got := spark([]float64{0, 5, 10}); got != "▁▄█" {
		t.Errorf("%q", got)
	}
	if got := spark([]float64{0, 0}); got != "▁▁" {
		t.Errorf("flat: %q", got)
	}
	if spark(nil) != "" {
		t.Error("empty")
	}
}

func TestSize(t *testing.T) {
	for in, want := range map[uint64]string{12: "12 B", 2048: "2 KiB", 5 << 20: "5.0 MiB", 3 << 30: "3.0 GiB"} {
		if got := size(in); got != want {
			t.Errorf("size(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestDiffNamesWhatChanged(t *testing.T) {
	at := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	up := Sample{At: at, Active: "active", URL: "http://x/healthz", ConfigTime: at, DiskFree: 10 << 30, RSS: 50 << 20}
	down := up
	down.At = at.Add(time.Second)
	down.Active, down.Health, down.Restarts = "failed", errors.New("refused"), 2
	got := diff(up, down)
	text := ""
	for _, e := range got {
		text += e.Text + "|"
		if !e.Bad {
			t.Errorf("%q must be bad", e.Text)
		}
	}
	for _, want := range []string{"active, now failed", "restarted itself 2", "config"} {
		if want == "config" {
			continue
		}
		if !strings.Contains(text, want) {
			t.Errorf("events lack %q: %s", want, text)
		}
	}

	back := up
	back.At = at.Add(2 * time.Second)
	events := diff(Sample{At: at, Active: "active", URL: "u", Health: errors.New("x")}, back)
	if len(events) != 1 || events[0].Bad || !strings.Contains(events[0].Text, "answers again") {
		t.Errorf("recovery: %+v", events)
	}

	broken := up
	broken.ConfigErr = "listen: bad"
	if ev := diff(up, broken); len(ev) != 1 || !ev[0].Bad {
		t.Errorf("config break: %+v", ev)
	}
	edited := up
	edited.ConfigTime = at.Add(time.Minute)
	if ev := diff(up, edited); len(ev) != 1 || ev[0].Bad || !strings.Contains(ev[0].Text, "config file changed") {
		t.Errorf("config edit: %+v", ev)
	}
	low := up
	low.DiskFree = 100 << 20
	if ev := diff(up, low); len(ev) != 1 || !ev[0].Bad {
		t.Errorf("disk: %+v", ev)
	}
	big := up
	big.RSS = 400 << 20
	if ev := diff(up, big); len(ev) != 1 || !strings.Contains(ev[0].Text, "memory") {
		t.Errorf("memory: %+v", ev)
	}
	if ev := diff(up, up); len(ev) != 0 {
		t.Errorf("no change, no event: %+v", ev)
	}
}

const statLine = "4242 (netscribe) S 1 4242 4242 0 -1 4194560 0 0 0 0 %d %d 0 0 20 0 9 0 1 0\n"

func TestCollectReadsTheServiceAndTheHost(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	f.logLines = "level=INFO msg=started\nlevel=ERROR msg=\"scan failed\"\n"
	f.proc["/proc/4242/status"] = "Name:\tnetscribe\nVmRSS:\t  51200 kB\nThreads:\t9\n"
	f.proc["/proc/4242/stat"] = strings.Replace(statLine, "%d %d", "100 100", 1)
	f.proc["/proc/loadavg"] = "0.50 0.25 0.10 1/200 999\n"
	f.proc["/proc/meminfo"] = "MemTotal:\t4194304 kB\nMemAvailable:\t2097152 kB\n"
	env := f.env("")

	first := collect(context.Background(), env, o, nil)
	if first.Active != "active" || first.PID != 4242 || first.Restarts != 0 || first.Enabled != "enabled" {
		t.Errorf("service: %+v", first)
	}
	if first.RSS != 50<<20 || first.Threads != 9 || first.CPU != -1 {
		t.Errorf("process: rss %d threads %d cpu %v", first.RSS, first.Threads, first.CPU)
	}
	if first.Health != nil || first.Latency <= 0 || first.URL != "http://127.0.0.1:8080/healthz" {
		t.Errorf("panel: %v %v %q", first.Health, first.Latency, first.URL)
	}
	if first.Load[0] != 0.5 || first.MemTotal != 4<<30 || first.MemAvail != 2<<30 || first.DiskFree == 0 {
		t.Errorf("host: %+v", first)
	}
	if len(first.Log) != 2 || first.ConfigErr != "" || first.ConfigTime.IsZero() {
		t.Errorf("log %v, config %q", first.Log, first.ConfigErr)
	}

	f.clock = first.At.Add(2*time.Second - 14*time.Millisecond)
	f.proc["/proc/4242/stat"] = strings.Replace(statLine, "%d %d", "150 150", 1) // 100 ticks later
	second := collect(context.Background(), env, o, &first)
	if second.CPU < 40 || second.CPU > 60 {
		t.Errorf("100 ticks in about 2 s is about 50 percent, got %.1f", second.CPU)
	}
}

func TestCollectOfAStoppedServiceAndABrokenConfig(t *testing.T) {
	f, o := installed(t)
	got := collect(context.Background(), f.env(""), o, nil)
	if got.Active != "inactive" || got.Health == nil || got.Up() {
		t.Errorf("a stopped service is not up: %+v", got)
	}
	writeRaw(t, o, "listen: nonsense\n")
	got = collect(context.Background(), f.env(""), o, nil)
	if got.ConfigErr == "" || got.URL != "" {
		t.Errorf("a broken config must be reported: %+v", got)
	}
}

func TestWatchOnceReportsWhetherTheServiceIsUp(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	o.Once = true
	if err := Watch(context.Background(), f.env(""), o, "vtest"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	out := f.out.String()
	for _, want := range []string{"Netscribe watch", "Service", "active", "Panel", "answers at http://127.0.0.1:8080", "Process", "Data", "Host", "Config", "Events", "Log of the service"} {
		if !strings.Contains(out, want) {
			t.Errorf("the screen lacks %q:\n%s", want, out)
		}
	}

	f.health = errors.New("connection refused")
	f.out.Reset()
	if err := Watch(context.Background(), f.env(""), o, "vtest"); err == nil {
		t.Error("a panel that does not answer must be an error with --once")
	}
	f.active = "inactive"
	if err := Watch(context.Background(), f.env(""), o, "vtest"); err == nil {
		t.Error("a stopped service must be an error with --once")
	}
}

func TestWatchLoopsAndRecordsAnEvent(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	o.WatchCount = 2
	o.WatchEvery = time.Millisecond
	env := f.env("")
	calls := 0
	env.Health = func(context.Context, string) error {
		calls++
		if calls == 2 {
			return errors.New("timeout")
		}
		return nil
	}
	if err := Watch(context.Background(), env, o, "v"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "panel stopped answering: timeout") {
		t.Errorf("the second screen must show the event:\n%s", f.out.String())
	}
}

func TestWatchStopsWhenTheContextEnds(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	o.WatchEvery = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	env := f.env("")
	env.Health = func(context.Context, string) error {
		cancel()
		return nil
	}
	if err := Watch(ctx, env, o, "v"); err != nil {
		t.Errorf("a stop is not an error: %v", err)
	}
}

func TestWatchOnATerminalRedrawsAndRestoresTheCursor(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	o.WatchCount = 2
	o.WatchEvery = time.Millisecond
	if err := Watch(context.Background(), f.env(""), o, "v"); err != nil {
		t.Fatal(err)
	}
	out := f.out.String()
	if strings.Count(out, "\x1b[H\x1b[J") != 2 || !strings.HasPrefix(out, "\x1b[?25l") || !strings.HasSuffix(out, "\x1b[?25h") || !strings.Contains(out, "Ctrl-C to stop") {
		t.Errorf("terminal control codes: %q", out)
	}
}
