// Package metrics writes the numbers of Netscribe in the text format Prometheus reads. It
// is written by hand, so there is no dependency, and it only ever prints fixed names and a
// few fixed label values: nothing that comes from the network can reach the output.
package metrics

import (
	"fmt"
	"io"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// ContentType is what the route answers with.
const ContentType = "text/plain; version=0.0.4; charset=utf-8"

type series struct {
	name, help string
	samples    []sample
}

type sample struct {
	labels string // already escaped, for example `status="online"`, or empty
	value  float64
}

// Write prints every series, each with its HELP and TYPE lines. All of them are gauges: they
// are read from the database at the moment of the request, not counted by the process.
func Write(w io.Writer, version string, o model.Overview) error {
	failed, done := o.RecentScans.Failed, o.RecentScans.Total-o.RecentScans.Failed
	var lastAt, lastTook, lastOK float64
	if sc := o.LastScan; sc != nil {
		lastAt = float64(sc.StartedAt.Unix())
		if sc.FinishedAt != nil {
			lastAt = float64(sc.FinishedAt.Unix())
			lastTook = sc.FinishedAt.Sub(sc.StartedAt).Seconds()
		}
		if sc.Status == "done" {
			lastOK = 1
		}
	}
	all := []series{
		{"netscribe_build_info", "The running version, always 1.",
			[]sample{{`version="` + labelValue(version) + `"`, 1}}},
		{"netscribe_devices", "Devices in the inventory by presence.",
			[]sample{{`status="online"`, float64(o.Devices.Online)}, {`status="offline"`, float64(o.Devices.Offline)}}},
		{"netscribe_devices_untrusted", "Devices nobody has marked as trusted.",
			[]sample{{"", float64(o.Devices.Unknown)}}},
		{"netscribe_devices_new_this_week", "Devices first seen in the last 7 days.",
			[]sample{{"", float64(o.Devices.NewThisWeek)}}},
		{"netscribe_risky_services", "Open services with a risk note.",
			[]sample{{"", float64(o.Risky)}}},
		{"netscribe_recent_scans", "The latest finished scans by result.",
			[]sample{{`status="done"`, float64(done)}, {`status="failed"`, float64(failed)}}},
		{"netscribe_last_scan_timestamp_seconds", "When the latest scan finished, or started while it runs. 0 when there is none.",
			[]sample{{"", lastAt}}},
		{"netscribe_last_scan_duration_seconds", "How long the latest scan took. 0 while it runs or when there is none.",
			[]sample{{"", lastTook}}},
		{"netscribe_last_scan_ok", "1 when the latest scan finished well, otherwise 0.",
			[]sample{{"", lastOK}}},
	}
	var b strings.Builder
	for _, s := range all {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", s.name, s.help, s.name)
		for _, sm := range s.samples {
			if sm.labels == "" {
				fmt.Fprintf(&b, "%s %s\n", s.name, number(sm.value))
			} else {
				fmt.Fprintf(&b, "%s{%s} %s\n", s.name, sm.labels, number(sm.value))
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// number prints a whole number without an exponent, which is what the counts and the
// seconds are.
func number(v float64) string { return fmt.Sprintf("%.0f", v) }

// labelValue keeps a label value to characters that need no escaping, so a version string
// can never break the line.
func labelValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_', r == '+':
			b.WriteRune(r)
		}
		if b.Len() >= 40 {
			break
		}
	}
	if b.Len() == 0 {
		return "dev"
	}
	return b.String()
}
