package notify

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// The three things that keep a busy network from filling a phone (slice 46): a device that
// goes up and down is reported once as flapping, quiet hours keep messages until the morning,
// and a maintenance window mutes the devices that a person is working on.

const (
	flapWindow = time.Hour // changes this close count together, and this much calm ends a flap

	maxSummaryLines = 200  // lines of the summary after quiet hours
	maxHeldLines    = 1000 // lines kept in memory during quiet hours; the rest are counted

	quietSubject = "Netscribe: news from the quiet hours"
)

// flapResult says what a change of a device means for its messages.
type flapResult int

const (
	flapNone    flapResult = iota // an ordinary change
	flapStarted                   // this change makes the device flapping: say so once
	flapHeld                      // the device is flapping already: say nothing
)

type flapState struct {
	times    []time.Time
	flapping bool
	last     time.Time
}

// flaps remembers the recent online and offline changes of every device. It lives in memory:
// a restart forgets it, and the worst that follows is one more message.
type flaps struct {
	mu     sync.Mutex
	m      map[string]*flapState
	latest time.Time // the newest change seen: the clock of the changes, whatever the clock of the host
}

func newFlaps() *flaps { return &flaps{m: map[string]*flapState{}} }

// observe records a change of the device at the given time. count changes within an hour make
// the device flapping; it stops flapping when an hour passes without a change.
func (f *flaps) observe(id string, at time.Time, count int) flapResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	if at.After(f.latest) {
		f.latest = at
	}
	st := f.m[id]
	if st == nil {
		st = &flapState{}
		f.m[id] = st
	}
	if st.flapping {
		if at.Sub(st.last) < flapWindow {
			st.last = at
			return flapHeld
		}
		*st = flapState{} // steady for an hour: it starts afresh
	}
	keep := st.times[:0]
	for _, t := range st.times {
		if at.Sub(t) < flapWindow {
			keep = append(keep, t)
		}
	}
	st.times = append(keep, at)
	st.last = at
	if len(st.times) >= count {
		st.flapping, st.times = true, nil
		return flapStarted
	}
	return flapNone
}

// forget drops the devices that have been calm for more than an hour, so the map stays small.
func (f *flaps) forget() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, st := range f.m {
		if f.latest.Sub(st.last) >= flapWindow {
			delete(f.m, id)
		}
	}
}

// inQuiet reports whether the clock time of t lies in the quiet hours. A range that is empty or
// not valid is never quiet; the config refuses a bad one, so this is only a safety.
func inQuiet(quiet string, t time.Time) bool {
	if strings.TrimSpace(quiet) == "" {
		return false
	}
	from, to, err := config.ParseWindow(quiet)
	if err != nil {
		return false
	}
	m := t.Hour()*60 + t.Minute()
	if from < to {
		return m >= from && m < to
	}
	return m >= from || m < to
}

// held is what quiet hours kept back.
type held struct {
	lines []string
	cut   int // lines that did not fit even in memory
}

func (h *held) add(lines []string) {
	for _, l := range lines {
		if len(h.lines) >= maxHeldLines {
			h.cut++
			continue
		}
		h.lines = append(h.lines, l)
	}
}

// summary is the one message that ends quiet hours: at most 200 lines, and a count of the
// ones that were cut.
func (h held) summary() string {
	lines := h.lines
	cut := h.cut
	if len(lines) > maxSummaryLines {
		cut += len(lines) - maxSummaryLines
		lines = lines[:maxSummaryLines]
	}
	out := strings.Join(lines, "\n")
	if cut > 0 {
		out += fmt.Sprintf("\nand %d more were cut", cut)
	}
	return out
}

// muted reports whether a maintenance window silences the line at the given time. A window
// without a group silences every device change, and one with a group the devices of that
// group. A failed scan is never muted: it says that Netscribe itself cannot see.
func muted(windows []config.MaintenanceWindow, now time.Time, it item, groups map[int64]string) bool {
	if it.event == config.EventScanFailed {
		return false
	}
	for _, w := range windows {
		from, to, err := w.Range()
		if err != nil || now.Before(from) || !now.Before(to) {
			continue
		}
		g := strings.TrimSpace(w.Group)
		switch {
		case g == "":
			return true
		case it.device != 0 && strings.EqualFold(groups[it.device], g):
			return true
		}
	}
	return false
}

// entityDevice reads the device id of a change whose entity is a device.
func entityDevice(c model.Change) int64 {
	switch c.Kind {
	case "device_added", "device_online", "device_offline", "device_changed":
		if id, err := strconv.ParseInt(c.EntityID, 10, 64); err == nil {
			return id
		}
	}
	return 0
}

// addressChanged tells whether a device_changed summary names a change of the address: the
// summary reads "<name> changed: ip a -> b; hostname c -> d".
func addressChanged(summary string) bool {
	_, parts, ok := strings.Cut(summary, " changed: ")
	if !ok {
		return false
	}
	for _, p := range strings.Split(parts, "; ") {
		for _, f := range []string{"ip ", "ip6 ", "mac "} {
			if strings.HasPrefix(p, f) {
				return true
			}
		}
	}
	return false
}
