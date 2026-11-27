package notify

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

func change(kind, entity, summary string, at time.Time) model.Change {
	return model.Change{Kind: kind, EntityID: entity, Summary: summary, At: at}
}

func srcWith(scan int64, devices []model.Device, changes ...model.Change) fakeSource {
	return fakeSource{scan: doneScan(scan), changes: changes, devices: devices}
}

func cam(id int64, group string) model.Device {
	d := model.Device{ID: id, IP: fmt.Sprintf("192.168.1.%d", id), Hostname: fmt.Sprintf("cam%d", id), FirstSeenAt: t0.Add(-time.Hour)}
	if group != "" {
		d.Group = &model.GroupRef{ID: id, Name: group}
	}
	return d
}

// run handles the scans one after the other with the same channel, and returns what it got.
func run(t *testing.T, c config.NotifyConfig, now time.Time, sources ...fakeSource) []string {
	t.Helper()
	rec := &recorder{}
	n, _ := newTest(sources[0], c, map[string]*recorder{"a": rec})
	n.now = func() time.Time { return now }
	for _, s := range sources {
		n.src = s
		n.Handle(context.Background(), s.scan)
	}
	return rec.got
}

func TestTheNewEventsAreSentOnlyWhenChosen(t *testing.T) {
	devs := []model.Device{cam(1, "")}
	chs := []model.Change{
		change("device_online", "1", "cam1 is back online", t0),
		change("device_changed", "1", "cam1 changed: ip 192.168.1.9 -> 192.168.1.20", t0),
		change("device_changed", "1", "cam1 changed: mac aa:aa:aa:aa:aa:01 -> bb:bb:bb:bb:bb:02", t0),
		change("device_changed", "1", "cam1 changed: hostname cam1 -> cam-one", t0),
		change("link_removed", "4", "Link disappeared: sw1 - cam1", t0),
		change("tls_expiring", "7", "Certificate of cam1:443 ends 2026-12-01", t0),
	}
	all := []string{config.EventDeviceOnline, config.EventAddressChanged, config.EventLinkDown, config.EventTLSExpiring}
	got := run(t, cfg(all, "a"), t0, srcWith(1, devs, chs...))
	if len(got) != 1 {
		t.Fatalf("messages = %v, want one", got)
	}
	for _, want := range []string{"back online", "ip 192.168.1.9", "mac aa:aa", "Link disappeared", "ends 2026-12-01"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("the message lacks %q:\n%s", want, got[0])
		}
	}
	if strings.Contains(got[0], "hostname cam1 -> cam-one") {
		t.Error("a change of the name is not a change of the address")
	}

	for _, ev := range all {
		only := run(t, cfg([]string{ev}, "a"), t0, srcWith(1, devs, chs...))
		if len(only) != 1 {
			t.Errorf("%s alone: %d messages, want 1", ev, len(only))
		}
	}
	if none := run(t, cfg([]string{config.EventDeviceAdded}, "a"), t0, srcWith(1, devs, chs...)); len(none) != 0 {
		t.Errorf("events that were not chosen were sent: %v", none)
	}
}

func flapCfg(count int, events ...string) config.NotifyConfig {
	c := cfg(append([]string{config.EventDeviceFlapping}, events...), "a")
	c.FlapCount = count
	return c
}

func TestAFlappingDeviceIsReportedOnceAndThenSilent(t *testing.T) {
	devs := []model.Device{cam(1, "")}
	min := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	off := func(m int) model.Change { return change("device_offline", "1", "cam1 went offline", min(m)) }
	on := func(m int) model.Change { return change("device_online", "1", "cam1 is back online", min(m)) }
	got := run(t, flapCfg(3, config.EventDeviceOffline, config.EventDeviceOnline), min(90),
		srcWith(1, devs, off(0)),
		srcWith(2, devs, on(10)),
		srcWith(3, devs, off(20)), // the third change in an hour
		srcWith(4, devs, on(30)),
		srcWith(5, devs, off(40)),
	)
	if len(got) != 3 {
		t.Fatalf("messages = %d (%v), want offline, online and one flapping message", len(got), got)
	}
	if !strings.Contains(got[2], "is flapping") || !strings.Contains(got[2], "3 times") {
		t.Errorf("the third message = %q", got[2])
	}
	for _, m := range got[3:] {
		t.Errorf("a message after the flapping one: %q", m)
	}
}

func TestADeviceThatIsSteadyForAnHourIsHeardAgain(t *testing.T) {
	devs := []model.Device{cam(1, "")}
	min := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	got := run(t, flapCfg(2, config.EventDeviceOffline), min(300),
		srcWith(1, devs, change("device_offline", "1", "cam1 went offline", min(0))),
		srcWith(2, devs, change("device_offline", "1", "cam1 went offline", min(5))),   // second: flapping
		srcWith(3, devs, change("device_offline", "1", "cam1 went offline", min(40))),  // held
		srcWith(4, devs, change("device_offline", "1", "cam1 went offline", min(101))), // 61 minutes calm: normal again
	)
	if len(got) != 3 {
		t.Fatalf("messages = %v, want the first offline, the flapping one and the one after the calm", got)
	}
	if !strings.Contains(got[1], "is flapping") || strings.Contains(got[2], "flapping") {
		t.Errorf("messages = %v", got)
	}
}

func TestFlappingIsPerDeviceAndOffByDefault(t *testing.T) {
	devs := []model.Device{cam(1, ""), cam(2, "")}
	min := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	// Two devices with two changes each, flapCount 3: nobody flaps.
	got := run(t, flapCfg(3, config.EventDeviceOffline), min(30),
		srcWith(1, devs, change("device_offline", "1", "cam1 went offline", min(0))),
		srcWith(2, devs, change("device_offline", "2", "cam2 went offline", min(1))),
		srcWith(3, devs, change("device_offline", "1", "cam1 went offline", min(2))),
		srcWith(4, devs, change("device_offline", "2", "cam2 went offline", min(3))),
	)
	if len(got) != 4 {
		t.Errorf("messages = %d, want 4: the devices do not share a count", len(got))
	}
	// Without the event, every change is sent as before.
	c := cfg([]string{config.EventDeviceOffline}, "a")
	c.FlapCount = 2
	got = run(t, c, min(30),
		srcWith(1, devs, change("device_offline", "1", "cam1 went offline", min(0))),
		srcWith(2, devs, change("device_offline", "1", "cam1 went offline", min(1))),
		srcWith(3, devs, change("device_offline", "1", "cam1 went offline", min(2))),
	)
	if len(got) != 3 {
		t.Errorf("without device_flapping: %d messages, want 3", len(got))
	}
}

func TestQuietHoursKeepMessagesAndSendOneSummary(t *testing.T) {
	devs := []model.Device{cam(1, "")}
	c := cfg([]string{config.EventDeviceOffline}, "a")
	c.QuietHours = "22:00-07:00"
	rec := &recorder{}
	night := time.Date(2026, 12, 1, 23, 30, 0, 0, time.UTC)
	n, _ := newTest(srcWith(1, devs), c, map[string]*recorder{"a": rec})
	now := night
	n.now = func() time.Time { return now }

	n.src = srcWith(1, devs, change("device_offline", "1", "cam1 went offline", night))
	n.Handle(context.Background(), n.src.(fakeSource).scan)
	n.src = srcWith(2, devs, change("device_offline", "1", "cam1 went offline again", night))
	n.Handle(context.Background(), n.src.(fakeSource).scan)
	if len(rec.got) != 0 {
		t.Fatalf("sent during quiet hours: %v", rec.got)
	}

	now = time.Date(2026, 12, 2, 6, 59, 0, 0, time.UTC)
	n.flush(context.Background(), c)
	if len(rec.got) != 0 {
		t.Fatalf("sent a minute before the end: %v", rec.got)
	}
	now = time.Date(2026, 12, 2, 7, 0, 0, 0, time.UTC)
	n.flush(context.Background(), c)
	if len(rec.got) != 1 {
		t.Fatalf("messages = %v, want one summary", rec.got)
	}
	if !strings.HasPrefix(rec.got[0], quietSubject+"|") || !strings.Contains(rec.got[0], "went offline\ncam1 went offline again") {
		t.Errorf("summary = %q", rec.got[0])
	}
	n.flush(context.Background(), c)
	if len(rec.got) != 1 {
		t.Error("the summary was sent twice")
	}
}

func TestTheNextScanAfterQuietHoursSendsTheSummary(t *testing.T) {
	devs := []model.Device{cam(1, "")}
	c := cfg([]string{config.EventDeviceOffline}, "a")
	c.QuietHours = "22:00-07:00"
	rec := &recorder{}
	n, _ := newTest(srcWith(1, devs), c, map[string]*recorder{"a": rec})
	now := time.Date(2026, 12, 1, 23, 0, 0, 0, time.UTC)
	n.now = func() time.Time { return now }
	n.src = srcWith(1, devs, change("device_offline", "1", "cam1 went offline", now))
	n.Handle(context.Background(), doneScan(1))

	now = time.Date(2026, 12, 2, 8, 0, 0, 0, time.UTC)
	n.src = srcWith(2, devs) // a quiet scan with no news
	n.Handle(context.Background(), doneScan(2))
	if len(rec.got) != 1 || !strings.Contains(rec.got[0], "cam1 went offline") {
		t.Errorf("messages = %v, want the summary", rec.got)
	}
}

func TestTheSummaryIsCappedAtTwoHundredLines(t *testing.T) {
	devs := []model.Device{cam(1, "")}
	c := cfg([]string{config.EventDeviceAdded}, "a")
	c.QuietHours = "22:00-07:00"
	rec := &recorder{}
	n, _ := newTest(srcWith(1, devs), c, map[string]*recorder{"a": rec})
	now := time.Date(2026, 12, 1, 23, 0, 0, 0, time.UTC)
	n.now = func() time.Time { return now }
	var chs []model.Change
	for i := 0; i < 300; i++ {
		chs = append(chs, change("device_added", fmt.Sprint(i+10), fmt.Sprintf("New device d%d", i), now))
	}
	n.src = srcWith(1, devs, chs...)
	n.Handle(context.Background(), doneScan(1))
	now = time.Date(2026, 12, 2, 7, 5, 0, 0, time.UTC)
	n.flush(context.Background(), c)
	if len(rec.got) != 1 {
		t.Fatalf("messages = %d", len(rec.got))
	}
	_, body, _ := strings.Cut(rec.got[0], "|")
	lines := strings.Split(body, "\n")
	if len(lines) != 201 || lines[200] != "and 100 more were cut" {
		t.Errorf("summary has %d lines, last %q, want 200 lines and a count of the 100 that were cut", len(lines), lines[len(lines)-1])
	}
}

func TestNothingIsHeldOutsideQuietHoursOrWithoutQuietHours(t *testing.T) {
	devs := []model.Device{cam(1, "")}
	chs := change("device_offline", "1", "cam1 went offline", t0)
	c := cfg([]string{config.EventDeviceOffline}, "a")
	c.QuietHours = "22:00-07:00"
	if got := run(t, c, time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC), srcWith(1, devs, chs)); len(got) != 1 {
		t.Errorf("at noon: %v, want one message at once", got)
	}
	c.QuietHours = ""
	if got := run(t, c, time.Date(2026, 12, 1, 23, 0, 0, 0, time.UTC), srcWith(1, devs, chs)); len(got) != 1 {
		t.Errorf("no quiet hours at night: %v, want one message at once", got)
	}
}

func maint(from, to time.Time, group string) config.MaintenanceWindow {
	return config.MaintenanceWindow{From: from.Format(config.MaintenanceLayout), To: to.Format(config.MaintenanceLayout), Group: group}
}

func TestMaintenanceMutesTheGroupAndNoOtherDevice(t *testing.T) {
	now := time.Date(2026, 12, 1, 23, 0, 0, 0, time.Local)
	devs := []model.Device{cam(1, "Lab"), cam(2, "Office")}
	chs := []model.Change{
		change("device_offline", "1", "cam1 went offline", now),
		change("device_offline", "2", "cam2 went offline", now),
	}
	c := cfg([]string{config.EventDeviceOffline}, "a")
	c.Maintenance = []config.MaintenanceWindow{maint(now.Add(-time.Hour), now.Add(time.Hour), " lab ")}
	got := run(t, c, now, srcWith(1, devs, chs...))
	if len(got) != 1 || !strings.Contains(got[0], "cam2") || strings.Contains(got[0], "cam1") {
		t.Errorf("messages = %v, want only cam2", got)
	}
}

func TestMaintenanceWithoutAGroupMutesEveryDeviceButNotAFailedScan(t *testing.T) {
	now := time.Date(2026, 12, 1, 23, 0, 0, 0, time.Local)
	devs := []model.Device{cam(1, "Lab"), cam(2, "")}
	chs := []model.Change{
		change("device_offline", "1", "cam1 went offline", now),
		change("device_offline", "2", "cam2 went offline", now),
		change("link_removed", "9", "Link disappeared: sw1 - cam1", now),
	}
	c := cfg([]string{config.EventDeviceOffline, config.EventLinkDown, config.EventScanFailed}, "a")
	c.Maintenance = []config.MaintenanceWindow{maint(now.Add(-time.Hour), now.Add(time.Hour), "")}
	if got := run(t, c, now, srcWith(1, devs, chs...)); len(got) != 0 {
		t.Errorf("messages = %v, want none in a window for the whole network", got)
	}
	failed := fakeSource{scan: model.Scan{ID: 2, Target: "192.168.1.0/24", Status: "failed", Error: "no route", StartedAt: t0}}
	if got := run(t, c, now, failed); len(got) != 1 || !strings.Contains(got[0], "no route") {
		t.Errorf("a failed scan must still be told: %v", got)
	}
}

func TestAMaintenanceWindowThatIsOverOrNotYetOpenMutesNothing(t *testing.T) {
	now := time.Date(2026, 12, 1, 23, 0, 0, 0, time.Local)
	devs := []model.Device{cam(1, "Lab")}
	chs := change("device_offline", "1", "cam1 went offline", now)
	c := cfg([]string{config.EventDeviceOffline}, "a")
	c.Maintenance = []config.MaintenanceWindow{
		maint(now.Add(-3*time.Hour), now.Add(-time.Hour), "Lab"), // over
		maint(now.Add(time.Hour), now.Add(3*time.Hour), "Lab"),   // not yet
		maint(now.Add(-time.Hour), now, "Lab"),                   // ends at this very minute: the end is outside
	}
	if got := run(t, c, now, srcWith(1, devs, chs)); len(got) != 1 {
		t.Errorf("messages = %v, want one", got)
	}
}

func TestAddressChangedReadsTheSummary(t *testing.T) {
	for summary, want := range map[string]bool{
		"cam1 changed: ip 1.1.1.1 -> 1.1.1.2":               true,
		"cam1 changed: hostname a -> b; ip6 - -> fe80::1":   true,
		"cam1 changed: kind camera -> server; mac aa -> bb": true,
		"cam1 changed: hostname a -> b":                     false,
		"cam1 changed: system linux -> bsd":                 false,
		"cam1 went offline":                                 false,
	} {
		if got := addressChanged(summary); got != want {
			t.Errorf("addressChanged(%q) = %v, want %v", summary, got, want)
		}
	}
}

func TestRulesShowWhatKeepsAlertsSteady(t *testing.T) {
	c := cfg(nil, "a")
	c.FlapCount = 4
	c.QuietHours = " 22:00-07:00 "
	c.Maintenance = []config.MaintenanceWindow{{From: "2026-12-01T22:00", To: "2026-12-02T02:00", Group: "Lab"}}
	n, _ := newTest(fakeSource{}, c, map[string]*recorder{"a": {}})
	got := n.Rules()
	if got.FlapCount != 4 || got.QuietHours != "22:00-07:00" || len(got.Maintenance) != 1 || got.Maintenance[0].Group != "Lab" {
		t.Errorf("rules = %+v", got)
	}
	c.Maintenance = nil
	n, _ = newTest(fakeSource{}, c, map[string]*recorder{"a": {}})
	if m := n.Rules().Maintenance; m == nil || len(m) != 0 {
		t.Errorf("no windows must be an empty list and not null: %#v", m)
	}
}
