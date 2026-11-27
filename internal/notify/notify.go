// Package notify turns what a finished scan found into short messages and sends them to
// the channels of the config. It imports only config and model: the scanner's data comes
// in through Source, so the package knows nothing about the database.
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	maxLines     = 20  // lines in one message, the rest is counted
	maxLineRunes = 200 // one line, whatever the network called the device
	testGap      = 10 * time.Second
	retryDelay   = 5 * time.Second
	subject      = "Netscribe: news from the network"
)

// The errors of Test. Everything else is a failed delivery.
var (
	ErrUnknownChannel = errors.New("no such channel")
	ErrTooSoon        = errors.New("a test message was sent a moment ago")
)

// Source is what the notifier reads about a finished scan.
type Source interface {
	LatestChanges(ctx context.Context) (scan model.Scan, changes []model.Change, ok bool, err error)
	ListServices(ctx context.Context, deviceID int64) ([]model.Service, error) // 0 means every device
	Devices(ctx context.Context) ([]model.Device, error)
}

// sender delivers one message to one place. Its errors never hold an address or a secret.
type sender interface {
	Send(ctx context.Context, subject, body string) error
}

// ChannelInfo is what the API may show about a channel: no address, token or password.
type ChannelInfo struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Events []string `json:"events"`
}

// Notifier sends one summary message per channel for each finished scan.
type Notifier struct {
	src Source
	cfg func() config.NotifyConfig
	log *slog.Logger

	build      func(config.ChannelConfig) sender // replaced in tests
	now        func() time.Time
	retryDelay time.Duration

	lastScan atomic.Int64

	mu       sync.Mutex
	lastTest map[string]time.Time
	held     held // what quiet hours kept back, sent when they end

	flaps *flaps
}

// New returns a notifier that reads the live notify block through cfg on every scan, so a
// reload of the config applies without a restart.
func New(src Source, cfg func() config.NotifyConfig, log *slog.Logger) *Notifier {
	return &Notifier{
		src: src, cfg: cfg, log: log,
		build: newSender, now: time.Now, retryDelay: retryDelay,
		lastTest: map[string]time.Time{},
		flaps:    newFlaps(),
	}
}

// Rules is what keeps alerts steady, for the panel to show. It is read only there: the
// windows are written in the config file.
type Rules struct {
	FlapCount   int               `json:"flapCount"`
	QuietHours  string            `json:"quietHours"`
	Maintenance []MaintenanceInfo `json:"maintenance"`
}

// MaintenanceInfo is one maintenance window as the config has it.
type MaintenanceInfo struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Group string `json:"group"`
}

// Rules returns the flap count, the quiet hours and the maintenance windows of the config.
func (n *Notifier) Rules() Rules {
	c := n.cfg()
	out := Rules{FlapCount: c.FlapCount, QuietHours: strings.TrimSpace(c.QuietHours), Maintenance: make([]MaintenanceInfo, 0, len(c.Maintenance))}
	for _, m := range c.Maintenance {
		out.Maintenance = append(out.Maintenance, MaintenanceInfo{From: m.From, To: m.To, Group: m.Group})
	}
	return out
}

// Channels lists the configured channels with the events they are sent.
func (n *Notifier) Channels() []ChannelInfo {
	c := n.cfg()
	out := make([]ChannelInfo, 0, len(c.Channels))
	for _, ch := range c.Channels {
		out = append(out, ChannelInfo{Name: ch.Name, Type: ch.Type, Events: append([]string{}, c.Events...)})
	}
	return out
}

// Handle sends what the scan found. It is called once for every finished scan and does
// nothing for a scan it already handled, for a scan that was only canceled, and when no
// channel is configured. A delivery that fails is logged and never returned: a broken
// webhook must not break a scan.
func (n *Notifier) Handle(ctx context.Context, scan model.Scan) {
	c := n.cfg()
	n.flush(ctx, c) // quiet hours may have ended since the last scan
	if len(c.Channels) == 0 || len(c.Events) == 0 || scan.Status == "running" {
		return
	}
	if scan.ID <= n.lastScan.Load() {
		return
	}
	n.lastScan.Store(scan.ID)

	enabled := map[string]bool{}
	for _, e := range c.Events {
		enabled[e] = true
	}
	var in input
	if scan.Status == "done" {
		var err error
		if in, err = n.read(ctx, scan); err != nil {
			n.log.Warn("notify: reading the scan failed", "scan", scan.ID, "err", err)
			return
		}
	}
	now := n.now()
	n.flaps.forget()
	items := compose(scan, enabled, in, n.flaps, c.FlapCount)
	groups := map[int64]string{}
	for _, d := range in.devices {
		if d.Group != nil {
			groups[d.ID] = d.Group.Name
		}
	}
	var lines []string
	for _, it := range items {
		if !muted(c.Maintenance, now, it, groups) {
			lines = append(lines, it.line)
		}
	}
	if len(lines) == 0 {
		return
	}
	if inQuiet(c.QuietHours, now) {
		n.mu.Lock()
		n.held.add(lines)
		n.mu.Unlock()
		return
	}
	body := body(lines)
	for _, ch := range c.Channels {
		if err := n.deliver(ctx, ch, subject, body); err != nil {
			n.log.Warn("notify: delivery failed", "channel", ch.Name, "type", ch.Type, "reason", err.Error())
		}
	}
}

// Run sends the summary of the quiet hours when they end, even when no scan comes by. It
// returns when ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.flush(ctx, n.cfg())
		}
	}
}

// flush sends what quiet hours kept, as one message, once they are over. With no channel left
// the lines are dropped: there is nobody to tell.
func (n *Notifier) flush(ctx context.Context, c config.NotifyConfig) {
	if inQuiet(c.QuietHours, n.now()) {
		return
	}
	n.mu.Lock()
	h := n.held
	n.held = held{}
	n.mu.Unlock()
	if len(h.lines) == 0 {
		return
	}
	body := h.summary()
	for _, ch := range c.Channels {
		if err := n.deliver(ctx, ch, quietSubject, body); err != nil {
			n.log.Warn("notify: delivery failed", "channel", ch.Name, "type", ch.Type, "reason", err.Error())
		}
	}
}

// Test sends a test message to one channel. A second test to the same channel within ten
// seconds is refused.
func (n *Notifier) Test(ctx context.Context, name string) error {
	var found *config.ChannelConfig
	c := n.cfg()
	for i := range c.Channels {
		if c.Channels[i].Name == name {
			found = &c.Channels[i]
		}
	}
	if found == nil {
		return ErrUnknownChannel
	}
	n.mu.Lock()
	if last, ok := n.lastTest[name]; ok && n.now().Sub(last) < testGap {
		n.mu.Unlock()
		return ErrTooSoon
	}
	n.lastTest[name] = n.now()
	n.mu.Unlock()
	return n.deliver(ctx, *found, subject, "This is a test message from Netscribe.")
}

func (n *Notifier) deliver(ctx context.Context, ch config.ChannelConfig, subject, body string) error {
	s := n.build(ch)
	err := s.Send(ctx, subject, body)
	if err == nil {
		return nil
	}
	select {
	case <-time.After(n.retryDelay):
	case <-ctx.Done():
		return err
	}
	return s.Send(ctx, subject, body)
}

// input is what compose needs besides the scan itself.
type input struct {
	changes  []model.Change
	services []model.Service
	devices  []model.Device
}

func (n *Notifier) read(ctx context.Context, scan model.Scan) (input, error) {
	latest, changes, ok, err := n.src.LatestChanges(ctx)
	if err != nil {
		return input{}, err
	}
	var in input
	if ok && latest.ID == scan.ID {
		in.changes = changes
	}
	if in.services, err = n.src.ListServices(ctx, 0); err != nil {
		return input{}, err
	}
	if in.devices, err = n.src.Devices(ctx); err != nil {
		return input{}, err
	}
	return in, nil
}

// item is one line of a message with what the filters need to know about it.
type item struct {
	event  string // the name in notify.events that allowed it
	device int64  // the device it is about, 0 when it is about none
	line   string
}

// compose is the whole decision of what is worth a message. It returns one item per event.
// flap and flapCount turn the device_flapping event on; flap may be nil.
func compose(scan model.Scan, enabled map[string]bool, in input, flap *flaps, flapCount int) []item {
	var items []item
	add := func(event string, device int64, format string, args ...any) {
		items = append(items, item{event: event, device: device, line: clean(fmt.Sprintf(format, args...))})
	}

	if scan.Status == "failed" {
		// A scan that a person canceled is not news.
		if enabled[config.EventScanFailed] && scan.Error != "canceled" {
			add(config.EventScanFailed, 0, "Scan of %s failed: %s", scan.Target, scan.Error)
		}
		return items
	}

	names := map[int64]string{}
	created := map[int64]bool{}
	for _, d := range in.devices {
		names[d.ID] = deviceName(d)
		if !d.FirstSeenAt.Before(scan.StartedAt) {
			created[d.ID] = true
		}
	}

	for _, c := range in.changes {
		dev := entityDevice(c)
		switch {
		case c.Kind == "device_added" && enabled[config.EventDeviceAdded]:
			add(config.EventDeviceAdded, dev, "%s", c.Summary)
		case c.Kind == "device_offline" || c.Kind == "device_online":
			event := config.EventDeviceOffline
			if c.Kind == "device_online" {
				event = config.EventDeviceOnline
			}
			// Flapping is looked at for every change, whether or not the plain event is
			// wanted, so a device that flaps is reported even by a person who wants only that.
			if enabled[config.EventDeviceFlapping] && flap != nil {
				switch flap.observe(c.EntityID, c.At, flapCount) {
				case flapStarted:
					name := names[dev]
					if name == "" {
						name = "device " + c.EntityID
					}
					add(config.EventDeviceFlapping, dev, "%s is flapping: it went up and down %d times within an hour. No more messages about it until it is steady for an hour.", name, flapCount)
					continue
				case flapHeld:
					continue
				}
			}
			if enabled[event] {
				add(event, dev, "%s", c.Summary)
			}
		case c.Kind == "device_changed" && enabled[config.EventAddressChanged] && addressChanged(c.Summary):
			add(config.EventAddressChanged, dev, "%s", c.Summary)
		case c.Kind == "link_removed" && enabled[config.EventLinkDown]:
			add(config.EventLinkDown, 0, "%s", c.Summary)
		case c.Kind == "tls_expiring" && enabled[config.EventTLSExpiring]:
			add(config.EventTLSExpiring, 0, "%s", c.Summary)
		}
	}

	for _, sv := range in.services {
		if sv.FirstSeenAt.Before(scan.StartedAt) {
			continue
		}
		host := names[sv.DeviceID]
		if host == "" {
			host = fmt.Sprintf("device %d", sv.DeviceID)
		}
		switch {
		case sv.Risk != nil && enabled[config.EventRiskyService]:
			add(config.EventRiskyService, sv.DeviceID, "Risky service on %s: %d/%s %s (%s, %s) - %s", host, sv.Port, sv.Proto, sv.Name, sv.Risk.Severity, sv.Risk.Code, sv.Risk.Message)
		case enabled[config.EventPortOpened] && !created[sv.DeviceID]:
			add(config.EventPortOpened, sv.DeviceID, "New open port on %s: %d/%s %s", host, sv.Port, sv.Proto, sv.Name)
		}
	}
	return items
}

func deviceName(d model.Device) string {
	for _, s := range []string{d.Label, d.Hostname, d.IP, d.IP6, d.MAC} {
		if s != "" {
			if addr := d.Address(); addr != "" && s != addr {
				return s + " (" + addr + ")"
			}
			return s
		}
	}
	return fmt.Sprintf("device %d", d.ID)
}

// body joins the lines and counts what does not fit.
func body(lines []string) string {
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	rest := len(lines) - maxLines
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\nand %d more", rest)
}

// clean makes a line safe to send: the network chose the names in it. Control characters
// and the ones that turn the text around go, a line break becomes a space, and a long line
// is cut.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case model.UnsafeRune(r):
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > maxLineRunes {
		s = string([]rune(s)[:maxLineRunes])
	}
	return s
}
