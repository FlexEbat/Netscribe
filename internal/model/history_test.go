package model

import (
	"testing"
	"time"
)

var h0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// pts builds one point per scan, ten minutes apart: "1" is online, "0" is not, and the
// addresses are given per scan.
func pts(online string, ips ...string) []PresencePoint {
	out := make([]PresencePoint, len(online))
	for i, c := range online {
		ip := ""
		if i < len(ips) {
			ip = ips[i]
		}
		out[i] = PresencePoint{ScanID: int64(i + 1), At: h0.Add(time.Duration(i) * 10 * time.Minute), Online: c == '1', IP: ip}
	}
	return out
}

func TestBuildHistoryUptime(t *testing.T) {
	h := BuildHistory(pts("1101"))
	if h.Scans != 4 || h.Online != 3 || h.Uptime == nil || *h.Uptime != 75 {
		t.Errorf("scans %d online %d uptime %v, want 4, 3 and 75", h.Scans, h.Online, h.Uptime)
	}
	if h := BuildHistory(pts("100")); *h.Uptime != 33.3 {
		t.Errorf("one scan in three = %v, want 33.3", *h.Uptime)
	}
	if h := BuildHistory(pts("11")); *h.Uptime != 100 {
		t.Errorf("always there = %v", *h.Uptime)
	}
}

func TestBuildHistoryOfADeviceThatWasNeverScanned(t *testing.T) {
	h := BuildHistory(nil)
	if h.Scans != 0 || h.Uptime != nil || h.IPs == nil || h.Outages == nil || h.Points == nil {
		t.Errorf("%+v, want zero scans, no uptime and empty lists that are not nil", h)
	}
}

func TestBuildHistoryKeepsTheAddressesNewestFirstAndAReturnedOneAgain(t *testing.T) {
	h := BuildHistory(pts("11111", "10.0.0.5", "10.0.0.5", "10.0.0.9", "10.0.0.9", "10.0.0.5"))
	if len(h.IPs) != 3 {
		t.Fatalf("spans = %+v, want 3 (the first address came back)", h.IPs)
	}
	if h.IPs[0].IP != "10.0.0.5" || !h.IPs[0].From.Equal(h0.Add(40*time.Minute)) {
		t.Errorf("newest = %+v", h.IPs[0])
	}
	if h.IPs[1].IP != "10.0.0.9" || !h.IPs[1].From.Equal(h0.Add(20*time.Minute)) || !h.IPs[1].To.Equal(h0.Add(30*time.Minute)) {
		t.Errorf("middle = %+v", h.IPs[1])
	}
	if h.IPs[2].IP != "10.0.0.5" || !h.IPs[2].To.Equal(h0.Add(10*time.Minute)) {
		t.Errorf("oldest = %+v", h.IPs[2])
	}
}

func TestBuildHistoryOutages(t *testing.T) {
	h := BuildHistory(pts("1001101"))
	if len(h.Outages) != 2 {
		t.Fatalf("outages = %+v, want 2", h.Outages)
	}
	if !h.Outages[0].From.Equal(h0.Add(50*time.Minute)) || h.Outages[0].To == nil || !h.Outages[0].To.Equal(h0.Add(60*time.Minute)) {
		t.Errorf("newest = %+v", h.Outages[0])
	}
	if !h.Outages[1].From.Equal(h0.Add(10*time.Minute)) || h.Outages[1].To == nil || !h.Outages[1].To.Equal(h0.Add(30*time.Minute)) {
		t.Errorf("older = %+v (two missed scans are one outage)", h.Outages[1])
	}

	still := BuildHistory(pts("110"))
	if len(still.Outages) != 1 || still.Outages[0].To != nil {
		t.Errorf("an outage that is still going on = %+v, want To nil", still.Outages)
	}
}

func TestBuildHistoryCapsWhatItReturns(t *testing.T) {
	online := ""
	for i := 0; i < 150; i++ {
		online += "10" // 150 outages
	}
	h := BuildHistory(pts(online))
	if len(h.Points) != maxHistoryPoints || h.Points[len(h.Points)-1].ScanID != 300 || h.Points[0].ScanID != 201 {
		t.Errorf("points: %d from scan %d to %d", len(h.Points), h.Points[0].ScanID, h.Points[len(h.Points)-1].ScanID)
	}
	if len(h.Outages) != maxHistoryOutages {
		t.Errorf("outages = %d, want %d", len(h.Outages), maxHistoryOutages)
	}
	if h.Scans != 300 {
		t.Errorf("scans = %d, the counts rest on every stored scan", h.Scans)
	}
}
