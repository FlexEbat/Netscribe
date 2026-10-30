package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

var t0 = time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)

type fakeSource struct {
	scan     model.Scan
	changes  []model.Change
	services []model.Service
	devices  []model.Device
}

func (f fakeSource) LatestChanges(context.Context) (model.Scan, []model.Change, bool, error) {
	return f.scan, f.changes, true, nil
}
func (f fakeSource) ListServices(context.Context, int64) ([]model.Service, error) {
	return f.services, nil
}
func (f fakeSource) Devices(context.Context) ([]model.Device, error) { return f.devices, nil }

// recorder is a sender that remembers what it was given.
type recorder struct {
	mu    sync.Mutex
	got   []string
	fail  int // how many calls fail first
	calls int
}

func (r *recorder) Send(_ context.Context, subject, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.fail > 0 {
		r.fail--
		return errors.New("boom")
	}
	r.got = append(r.got, subject+"|"+body)
	return nil
}

func newTest(src Source, c config.NotifyConfig, rec map[string]*recorder) (*Notifier, *bytes.Buffer) {
	var logs bytes.Buffer
	n := New(src, func() config.NotifyConfig { return c }, slog.New(slog.NewTextHandler(&logs, nil)))
	n.retryDelay = time.Millisecond
	n.build = func(ch config.ChannelConfig) sender { return rec[ch.Name] }
	return n, &logs
}

func cfg(events []string, names ...string) config.NotifyConfig {
	c := config.NotifyConfig{Events: events}
	for _, n := range names {
		c.Channels = append(c.Channels, config.ChannelConfig{Name: n, Type: config.ChannelWebhook, URL: "https://secret.example/hook-token"})
	}
	return c
}

func doneScan(id int64) model.Scan {
	return model.Scan{ID: id, Target: "192.168.1.0/24", Status: "done", StartedAt: t0}
}

func newDeviceSource(id int64) fakeSource {
	return fakeSource{
		scan:    doneScan(id),
		changes: []model.Change{{Kind: "device_added", Summary: "New device cam1 (192.168.1.9, camera)"}},
		devices: []model.Device{{ID: 1, IP: "192.168.1.9", Hostname: "cam1", FirstSeenAt: t0.Add(time.Second)}},
	}
}

func TestOneMessagePerChannelAndNothingTwiceForTheSameScan(t *testing.T) {
	a, b := &recorder{}, &recorder{}
	n, _ := newTest(newDeviceSource(7), cfg([]string{config.EventDeviceAdded}, "a", "b"), map[string]*recorder{"a": a, "b": b})

	n.Handle(context.Background(), doneScan(7))
	n.Handle(context.Background(), doneScan(7)) // the same scan again

	for name, r := range map[string]*recorder{"a": a, "b": b} {
		if len(r.got) != 1 || !strings.Contains(r.got[0], "New device cam1") {
			t.Errorf("channel %s got %v, want exactly one message about cam1", name, r.got)
		}
	}
}

func TestOnlyTheChosenEventsAreSent(t *testing.T) {
	src := newDeviceSource(1)
	src.changes = append(src.changes, model.Change{Kind: "device_offline", Summary: "nas went offline"},
		model.Change{Kind: "device_online", Summary: "tv is back online"})
	r := &recorder{}
	n, _ := newTest(src, cfg([]string{config.EventDeviceOffline}, "a"), map[string]*recorder{"a": r})
	n.Handle(context.Background(), doneScan(1))
	if len(r.got) != 1 || strings.Contains(r.got[0], "cam1") || strings.Contains(r.got[0], "tv is back") || !strings.Contains(r.got[0], "nas went offline") {
		t.Errorf("got %v, want only the offline line", r.got)
	}
}

func TestNoChannelsOrNoNewsSendsNothing(t *testing.T) {
	r := &recorder{}
	n, _ := newTest(newDeviceSource(1), cfg([]string{config.EventDeviceAdded}), map[string]*recorder{"a": r})
	n.Handle(context.Background(), doneScan(1))

	src := newDeviceSource(2)
	src.changes = nil
	n, _ = newTest(src, cfg([]string{config.EventDeviceAdded}, "a"), map[string]*recorder{"a": r})
	n.Handle(context.Background(), doneScan(2))
	if r.calls != 0 {
		t.Errorf("sender called %d times, want 0", r.calls)
	}
}

func TestFailedScanIsReportedButACanceledOneIsNot(t *testing.T) {
	r := &recorder{}
	n, _ := newTest(fakeSource{}, cfg([]string{config.EventScanFailed}, "a"), map[string]*recorder{"a": r})
	n.Handle(context.Background(), model.Scan{ID: 1, Target: "10.0.0.0/24", Status: "failed", Error: "arp: no interface"})
	n.Handle(context.Background(), model.Scan{ID: 2, Target: "10.0.0.0/24", Status: "failed", Error: "canceled"})
	n.Handle(context.Background(), model.Scan{ID: 3, Status: "running"})
	if len(r.got) != 1 || !strings.Contains(r.got[0], "Scan of 10.0.0.0/24 failed: arp: no interface") {
		t.Errorf("got %v", r.got)
	}
}

func TestPortsAndRiskyServices(t *testing.T) {
	old := t0.Add(-time.Hour)
	src := fakeSource{
		scan: doneScan(1),
		devices: []model.Device{
			{ID: 1, IP: "192.168.1.1", Hostname: "gw", FirstSeenAt: old},
			{ID: 2, IP: "192.168.1.2", Hostname: "fresh", FirstSeenAt: t0.Add(time.Second)},
		},
		services: []model.Service{
			{DeviceID: 1, Port: 22, Proto: "tcp", Name: "ssh", FirstSeenAt: old},                                                        // not new
			{DeviceID: 1, Port: 8080, Proto: "tcp", Name: "http-alt", FirstSeenAt: t0.Add(time.Second)},                                 // new port on an old device
			{DeviceID: 1, Port: 23, Proto: "tcp", Name: "telnet", FirstSeenAt: t0.Add(time.Second), Risk: model.RateService(23, "tcp")}, // risky
			{DeviceID: 2, Port: 80, Proto: "tcp", Name: "http", FirstSeenAt: t0.Add(time.Second)},                                       // new device, not a "port opened"
		},
	}
	r := &recorder{}
	n, _ := newTest(src, cfg([]string{config.EventPortOpened, config.EventRiskyService}, "a"), map[string]*recorder{"a": r})
	n.Handle(context.Background(), doneScan(1))
	if len(r.got) != 1 {
		t.Fatalf("got %v", r.got)
	}
	m := r.got[0]
	for _, want := range []string{"New open port on gw (192.168.1.1): 8080/tcp http-alt", "Risky service on gw (192.168.1.1): 23/tcp telnet (high)"} {
		if !strings.Contains(m, want) {
			t.Errorf("message lacks %q:\n%s", want, m)
		}
	}
	for _, not := range []string{"22/tcp", "fresh"} {
		if strings.Contains(m, not) {
			t.Errorf("message holds %q:\n%s", not, m)
		}
	}

	// Without risky_service the same risky port is only a new port.
	r2 := &recorder{}
	n, _ = newTest(src, cfg([]string{config.EventPortOpened}, "a"), map[string]*recorder{"a": r2})
	n.Handle(context.Background(), doneScan(1))
	if len(r2.got) != 1 || !strings.Contains(r2.got[0], "New open port on gw (192.168.1.1): 23/tcp telnet") {
		t.Errorf("got %v", r2.got)
	}
}

func TestMessageIsCappedAndCleanedOfWhatTheNetworkSent(t *testing.T) {
	src := fakeSource{scan: doneScan(1)}
	for i := 0; i < 25; i++ {
		src.changes = append(src.changes, model.Change{Kind: "device_added", Summary: fmt.Sprintf("New device d%d", i)})
	}
	src.changes[0].Summary = "New device evil\u202eNAME\x1b[31m\nInjected: line\r\nBcc: x@y.z" + strings.Repeat("x", 500)
	r := &recorder{}
	n, _ := newTest(src, cfg([]string{config.EventDeviceAdded}, "a"), map[string]*recorder{"a": r})
	n.Handle(context.Background(), doneScan(1))

	body := strings.SplitN(r.got[0], "|", 2)[1]
	lines := strings.Split(body, "\n")
	if len(lines) != maxLines+1 || lines[maxLines] != "and 5 more" {
		t.Fatalf("lines = %d, last %q, want %d and %q", len(lines), lines[len(lines)-1], maxLines+1, "and 5 more")
	}
	if len([]rune(lines[0])) > maxLineRunes {
		t.Errorf("first line has %d runes", len([]rune(lines[0])))
	}
	for _, l := range lines {
		for _, r := range l {
			if model.UnsafeRune(r) {
				t.Fatalf("unsafe rune %U in %q", r, l)
			}
		}
	}
	if strings.Contains(body, "\x1b") || strings.Contains(body, "\r") {
		t.Errorf("control characters survived: %q", body)
	}
}

func TestFailedChannelIsRetriedOnceAndDoesNotStopTheOthers(t *testing.T) {
	flaky, broken, fine := &recorder{fail: 1}, &recorder{fail: 99}, &recorder{}
	n, logs := newTest(newDeviceSource(1), cfg([]string{config.EventDeviceAdded}, "flaky", "broken", "fine"),
		map[string]*recorder{"flaky": flaky, "broken": broken, "fine": fine})
	n.Handle(context.Background(), doneScan(1))

	if len(flaky.got) != 1 || flaky.calls != 2 {
		t.Errorf("flaky: %d messages in %d calls, want 1 in 2", len(flaky.got), flaky.calls)
	}
	if broken.calls != 2 || len(broken.got) != 0 {
		t.Errorf("broken: %d calls, want 2 and no message", broken.calls)
	}
	if len(fine.got) != 1 {
		t.Errorf("fine got %v", fine.got)
	}
	if out := logs.String(); !strings.Contains(out, "channel=broken") || strings.Contains(out, "secret.example") || strings.Contains(out, "hook-token") {
		t.Errorf("log = %s, want the channel name and no address", out)
	}
}

func TestTestMessageHasLimitsAndKnownChannels(t *testing.T) {
	r := &recorder{}
	n, _ := newTest(fakeSource{}, cfg(nil, "a"), map[string]*recorder{"a": r})
	now := t0
	n.now = func() time.Time { return now }

	if err := n.Test(context.Background(), "nope"); !errors.Is(err, ErrUnknownChannel) {
		t.Errorf("unknown channel: %v", err)
	}
	if err := n.Test(context.Background(), "a"); err != nil {
		t.Fatalf("first test: %v", err)
	}
	if err := n.Test(context.Background(), "a"); !errors.Is(err, ErrTooSoon) {
		t.Errorf("second test at once: %v, want ErrTooSoon", err)
	}
	now = now.Add(11 * time.Second)
	if err := n.Test(context.Background(), "a"); err != nil {
		t.Errorf("test after 11 seconds: %v", err)
	}
	if len(r.got) != 2 {
		t.Errorf("messages = %d, want 2", len(r.got))
	}
}

func TestChannelsShowNoSecrets(t *testing.T) {
	c := cfg([]string{config.EventScanFailed}, "a")
	c.Channels[0].BotToken, c.Channels[0].ChatID = "tok", "chat"
	n, _ := newTest(fakeSource{}, c, nil)
	got := fmt.Sprintf("%+v", n.Channels())
	for _, s := range []string{"hook-token", "tok", "chat", "secret.example"} {
		if strings.Contains(got, s) {
			t.Errorf("Channels() leaked %q: %s", s, got)
		}
	}
	if len(n.Channels()) != 1 || n.Channels()[0].Name != "a" || n.Channels()[0].Events[0] != config.EventScanFailed {
		t.Errorf("Channels() = %+v", n.Channels())
	}
}
