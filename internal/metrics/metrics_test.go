package metrics

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// a sample line of the text format: a name, optional labels, a number
var sampleLine = regexp.MustCompile(`^[a-z_]+(\{[a-z_]+="[A-Za-z0-9._+\-]*"\})? [0-9]+$`)

func render(t *testing.T, version string, o model.Overview) string {
	t.Helper()
	var b bytes.Buffer
	if err := Write(&b, version, o); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestEveryLineIsHelpTypeOrASample(t *testing.T) {
	out := render(t, "v0.2.0", model.Overview{})
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.HasPrefix(line, "# HELP ") || strings.HasPrefix(line, "# TYPE ") {
			continue
		}
		if !sampleLine.MatchString(line) {
			t.Errorf("not a sample line: %q", line)
		}
	}
}

func TestAnEmptyDatabaseGivesZerosAndNoMissingSeries(t *testing.T) {
	out := render(t, "dev", model.Overview{})
	for _, want := range []string{
		`netscribe_build_info{version="dev"} 1`,
		`netscribe_devices{status="online"} 0`, `netscribe_devices{status="offline"} 0`,
		"netscribe_devices_untrusted 0", "netscribe_risky_services 0",
		`netscribe_recent_scans{status="failed"} 0`,
		"netscribe_last_scan_timestamp_seconds 0", "netscribe_last_scan_ok 0",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestTheNumbersComeFromTheOverview(t *testing.T) {
	start := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Second)
	o := model.Overview{
		Devices:     model.OverviewDevices{Total: 5, Online: 3, Offline: 2, Unknown: 4, NewThisWeek: 1},
		Risky:       2,
		RecentScans: model.ScanHealth{Total: 10, Failed: 3},
		LastScan:    &model.Scan{Status: "done", StartedAt: start, FinishedAt: &end},
	}
	out := render(t, "v1", o)
	for _, want := range []string{
		`netscribe_devices{status="online"} 3`, `netscribe_devices{status="offline"} 2`,
		"netscribe_devices_untrusted 4", "netscribe_devices_new_this_week 1", "netscribe_risky_services 2",
		`netscribe_recent_scans{status="done"} 7`, `netscribe_recent_scans{status="failed"} 3`,
		"netscribe_last_scan_duration_seconds 90", "netscribe_last_scan_ok 1",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "netscribe_last_scan_timestamp_seconds "+number(float64(end.Unix()))+"\n") {
		t.Errorf("timestamp:\n%s", out)
	}
}

func TestARunningOrFailedScanIsNotOK(t *testing.T) {
	start := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, status := range []string{"running", "failed"} {
		out := render(t, "v1", model.Overview{LastScan: &model.Scan{Status: status, StartedAt: start}})
		if !strings.Contains(out, "netscribe_last_scan_ok 0\n") || !strings.Contains(out, "netscribe_last_scan_duration_seconds 0\n") {
			t.Errorf("%s:\n%s", status, out)
		}
	}
}

func TestAVersionCannotBreakALine(t *testing.T) {
	out := render(t, "v1\"} 99\n# HELP evil "+strings.Repeat("x", 200), model.Overview{})
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if strings.HasPrefix(line, "# HELP netscribe_") || strings.HasPrefix(line, "# TYPE netscribe_") {
			continue
		}
		if !sampleLine.MatchString(line) {
			t.Errorf("a line the version added or broke: %q", line)
		}
	}
	if got := labelValue("\n\"{}"); got != "dev" {
		t.Errorf("labelValue = %q, want dev", got)
	}
	if len(labelValue(strings.Repeat("a", 500))) > 40 {
		t.Error("a long version was not cut")
	}
}
