package model

import "time"

// PresencePoint says whether one completed scan saw a device, and under which address.
type PresencePoint struct {
	ScanID int64     `json:"scanId"`
	At     time.Time `json:"at"`
	Online bool      `json:"online"`
	IP     string    `json:"ip"`
}

// IPSpan is a run of scans in which the device kept one address.
type IPSpan struct {
	IP   string    `json:"ip"`
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

// Outage is a run of scans that did not see the device. To is nil while it is still away.
type Outage struct {
	From time.Time  `json:"from"`
	To   *time.Time `json:"to"`
}

// DeviceHistory is what the stored scans say about one device.
type DeviceHistory struct {
	Scans   int             `json:"scans"`
	Online  int             `json:"online"`
	Uptime  *float64        `json:"uptime"` // percent with one decimal, nil without scans
	IPs     []IPSpan        `json:"ips"`    // newest first
	Outages []Outage        `json:"outages"`
	Points  []PresencePoint `json:"points"` // the last maxPoints, oldest first
}

const (
	maxHistoryPoints  = 100
	maxHistoryOutages = 20
)

// BuildHistory turns the rows of one device, oldest scan first, into its history. The lists
// are never nil, so the JSON never holds null for them.
func BuildHistory(points []PresencePoint) DeviceHistory {
	h := DeviceHistory{Scans: len(points), IPs: []IPSpan{}, Outages: []Outage{}, Points: []PresencePoint{}}
	var spans []IPSpan
	var outages []Outage
	var open *Outage // the outage that is going on
	for _, p := range points {
		if p.Online {
			h.Online++
			if open != nil {
				t := p.At
				open.To = &t
				outages = append(outages, *open)
				open = nil
			}
		} else if open == nil {
			open = &Outage{From: p.At}
		}
		if p.IP == "" {
			continue
		}
		if n := len(spans); n > 0 && spans[n-1].IP == p.IP {
			spans[n-1].To = p.At
		} else {
			spans = append(spans, IPSpan{IP: p.IP, From: p.At, To: p.At})
		}
	}
	if open != nil {
		outages = append(outages, *open)
	}
	if len(points) > 0 {
		u := float64(int(float64(h.Online)/float64(len(points))*1000+0.5)) / 10
		h.Uptime = &u
	}
	for i := len(spans) - 1; i >= 0; i-- {
		h.IPs = append(h.IPs, spans[i])
	}
	for i := len(outages) - 1; i >= 0 && len(h.Outages) < maxHistoryOutages; i-- {
		h.Outages = append(h.Outages, outages[i])
	}
	from := 0
	if len(points) > maxHistoryPoints {
		from = len(points) - maxHistoryPoints
	}
	h.Points = append(h.Points, points[from:]...)
	return h
}

// OverviewDevices counts the devices for the overview page.
type OverviewDevices struct {
	Total       int `json:"total"`
	Online      int `json:"online"`
	Offline     int `json:"offline"`
	Unknown     int `json:"unknown"`
	NewThisWeek int `json:"newThisWeek"`
}

// ScanHealth says how many of the last scans failed.
type ScanHealth struct {
	Total  int `json:"total"`
	Failed int `json:"failed"`
}

// Overview is the answer of GET /api/overview.
type Overview struct {
	Devices     OverviewDevices `json:"devices"`
	Risky       int             `json:"risky"`
	LastScan    *Scan           `json:"lastScan"`
	RecentScans ScanHealth      `json:"recentScans"`
	Changes     []Change        `json:"changes"`
}
