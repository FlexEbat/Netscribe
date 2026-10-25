package setup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
)

const (
	clockTicks    = 100 // USER_HZ of Linux: the unit of /proc/PID/stat times
	historyLength = 28
	eventsKept    = 8
	logLines      = 8
)

// Sample is what the watcher saw at one moment.
type Sample struct {
	At time.Time

	Active   string // active, inactive, failed, activating, unknown
	Sub      string
	Enabled  string
	PID      int
	Since    string
	Restarts int

	RSS     uint64 // bytes
	Threads int
	Files   int
	CPU     float64 // percent of one core since the previous sample, -1 when unknown
	cpuTick uint64  // utime+stime of the process, for the next sample

	URL     string
	Health  error
	Latency time.Duration

	DBBytes  uint64
	WALBytes uint64
	DiskFree uint64

	Load       [3]float64
	MemAvail   uint64 // bytes
	MemTotal   uint64
	ConfigErr  string
	ConfigTime time.Time
	Log        []string
}

// Up says whether the service runs and the panel answers.
func (s Sample) Up() bool { return s.Active == "active" && s.Health == nil }

// show parses the key=value lines that systemctl show prints.
func show(out []byte) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			m[k] = strings.TrimSpace(v)
		}
	}
	return m
}

// kb reads a "Name:   123 kB" line of /proc files, in bytes.
func kb(text, name string) uint64 {
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(line, name+":"); ok {
			f := strings.Fields(rest)
			if len(f) > 0 {
				n, _ := strconv.ParseUint(f[0], 10, 64)
				return n << 10
			}
		}
	}
	return 0
}

// processTicks returns utime+stime of /proc/PID/stat. The name of the process may hold
// spaces and brackets, so the fields count from the last closing bracket.
func processTicks(stat string) (uint64, bool) {
	i := strings.LastIndex(stat, ")")
	if i < 0 {
		return 0, false
	}
	f := strings.Fields(stat[i+1:])
	if len(f) < 13 { // state is f[0]; utime and stime are fields 14 and 15 of the whole line
		return 0, false
	}
	u, err1 := strconv.ParseUint(f[11], 10, 64)
	st, err2 := strconv.ParseUint(f[12], 10, 64)
	return u + st, err1 == nil && err2 == nil
}

// collect takes one sample. prev, when given, turns the process time into a percentage.
func collect(ctx context.Context, env Env, o Options, prev *Sample) Sample {
	now := env.Now()
	s := Sample{At: now, Active: "unknown", CPU: -1}

	out, _ := env.Run(ctx, Command{Name: "systemctl", Args: []string{
		"show", "netscribe", "--no-pager",
		"-p", "ActiveState", "-p", "SubState", "-p", "MainPID", "-p", "NRestarts", "-p", "ActiveEnterTimestamp", "-p", "UnitFileState",
	}})
	m := show(out)
	if v := m["ActiveState"]; v != "" {
		s.Active = v
	}
	s.Sub, s.Enabled, s.Since = m["SubState"], m["UnitFileState"], m["ActiveEnterTimestamp"]
	s.PID, _ = strconv.Atoi(m["MainPID"])
	s.Restarts, _ = strconv.Atoi(m["NRestarts"])

	if s.PID > 0 {
		base := "/proc/" + strconv.Itoa(s.PID)
		if data, err := env.ReadFile(base + "/status"); err == nil {
			s.RSS = kb(string(data), "VmRSS")
			for _, line := range strings.Split(string(data), "\n") {
				if rest, ok := strings.CutPrefix(line, "Threads:"); ok {
					s.Threads, _ = strconv.Atoi(strings.TrimSpace(rest))
				}
			}
		}
		if data, err := env.ReadFile(base + "/stat"); err == nil {
			if ticks, ok := processTicks(string(data)); ok {
				s.cpuTick = ticks
				if prev != nil && prev.PID == s.PID && prev.cpuTick > 0 && now.After(prev.At) && ticks >= prev.cpuTick {
					s.CPU = float64(ticks-prev.cpuTick) / clockTicks / now.Sub(prev.At).Seconds() * 100
				}
			}
		}
		if entries, err := env.ReadDir(base + "/fd"); err == nil {
			s.Files = len(entries)
		}
	}

	cfg, err := config.Load(o.ConfigPath, false)
	switch {
	case err != nil:
		s.ConfigErr = err.Error()
	default:
		if info, err := env.Stat(o.ConfigPath); err == nil {
			s.ConfigTime = info.ModTime()
		}
		s.URL = healthURL(cfg)
		if s.Active == "active" && s.URL != "" {
			start := env.Now()
			s.Health = env.Health(ctx, s.URL)
			s.Latency = env.Now().Sub(start)
		} else if s.URL != "" {
			s.Health = errors.New("the service is not running")
		}
		if info, err := env.Stat(cfg.Database); err == nil {
			s.DBBytes = uint64(max(info.Size(), 0))
		}
		if info, err := env.Stat(cfg.Database + "-wal"); err == nil {
			s.WALBytes = uint64(max(info.Size(), 0))
		}
		if free, err := env.FreeBytes(existingParent(env, filepath.Dir(cfg.Database))); err == nil {
			s.DiskFree = free
		}
	}

	if data, err := env.ReadFile("/proc/loadavg"); err == nil {
		f := strings.Fields(string(data))
		for i := 0; i < 3 && i < len(f); i++ {
			s.Load[i], _ = strconv.ParseFloat(f[i], 64)
		}
	}
	if data, err := env.ReadFile("/proc/meminfo"); err == nil {
		s.MemAvail, s.MemTotal = kb(string(data), "MemAvailable"), kb(string(data), "MemTotal")
	}
	if out, err := env.Run(ctx, Command{Name: "journalctl", Args: []string{"-u", "netscribe", "-n", strconv.Itoa(logLines), "-o", "cat", "--no-pager"}}); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				s.Log = append(s.Log, l)
			}
		}
	}
	return s
}

// Event is a change between two samples.
type Event struct {
	At   time.Time
	Text string
	Bad  bool
}

// diff lists what changed between two samples, in words a person acts on.
func diff(prev, cur Sample) []Event {
	var ev []Event
	add := func(bad bool, format string, a ...any) {
		ev = append(ev, Event{At: cur.At, Text: fmt.Sprintf(format, a...), Bad: bad})
	}
	if prev.Active != cur.Active {
		add(cur.Active != "active", "service: %s, now %s", prev.Active, cur.Active)
	}
	if cur.Restarts > prev.Restarts {
		add(true, "service restarted itself %d time(s)", cur.Restarts-prev.Restarts)
	}
	switch {
	case prev.Health == nil && cur.Health != nil && cur.Active == "active":
		add(true, "panel stopped answering: %v", cur.Health)
	case prev.Health != nil && cur.Health == nil && prev.URL != "":
		add(false, "panel answers again")
	}
	switch {
	case prev.ConfigErr == "" && cur.ConfigErr != "":
		add(true, "config is not valid: %s", cur.ConfigErr)
	case prev.ConfigErr != "" && cur.ConfigErr == "":
		add(false, "config is valid again")
	case !prev.ConfigTime.IsZero() && cur.ConfigTime.After(prev.ConfigTime):
		add(false, "config file changed")
	}
	if prev.DiskFree >= minFreeBytes && cur.DiskFree > 0 && cur.DiskFree < minFreeBytes {
		add(true, "less than 256 MiB left for the data")
	}
	if prev.RSS > 0 && cur.RSS > prev.RSS*3/2 && cur.RSS > 256<<20 {
		add(true, "memory of the service grew to %s", size(cur.RSS))
	}
	return ev
}

func size(b uint64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KiB", float64(b)/(1<<10))
	}
	return fmt.Sprintf("%d B", b)
}

var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// spark draws values as a row of blocks, scaled to the largest.
func spark(values []float64) string {
	hi := 0.0
	for _, v := range values {
		hi = max(hi, v)
	}
	var b strings.Builder
	for _, v := range values {
		i := 0
		if hi > 0 {
			i = int(v / hi * float64(len(sparkRunes)-1))
		}
		b.WriteRune(sparkRunes[min(max(i, 0), len(sparkRunes)-1)])
	}
	return b.String()
}

// view holds what the watcher remembers between samples.
type view struct {
	prev    *Sample
	latency []float64
	events  []Event
}

func (v *view) push(s Sample) {
	if v.prev != nil {
		v.events = append(v.events, diff(*v.prev, s)...)
		if len(v.events) > eventsKept {
			v.events = v.events[len(v.events)-eventsKept:]
		}
	}
	if s.Health == nil && s.Latency > 0 {
		v.latency = append(v.latency, float64(s.Latency.Microseconds())/1000)
	} else if s.Active == "active" {
		v.latency = append(v.latency, 0)
	}
	if len(v.latency) > historyLength {
		v.latency = v.latency[len(v.latency)-historyLength:]
	}
	cp := s
	v.prev = &cp
}

func (u ui) dot(good bool, warn bool) string {
	switch {
	case good:
		return u.paint("32", "●")
	case warn:
		return u.paint("33", "●")
	}
	return u.paint("31", "●")
}

// render prints a screen of the watcher.
func (v *view) render(u ui, s Sample, o Options, version string) {
	u.printf("%s  %s  %s\n\n", u.paint("1;36", "Netscribe watch"), u.dim(version), u.dim(s.At.Format("15:04:05")))

	state := s.Active
	if s.Sub != "" && s.Sub != s.Active {
		state += " (" + s.Sub + ")"
	}
	line := fmt.Sprintf("%s %s", u.dot(s.Active == "active", s.Active == "activating"), state)
	if s.Since != "" && s.Active == "active" {
		line += u.dim("   since " + s.Since)
	}
	if s.Enabled != "" {
		line += u.dim("   " + s.Enabled)
	}
	if s.Restarts > 0 {
		line += "   " + u.paint("33", fmt.Sprintf("restarts %d", s.Restarts))
	}
	u.printf("  %-8s %s\n", u.bold("Service"), line)

	switch {
	case s.URL == "":
		u.printf("  %-8s %s %s\n", u.bold("Panel"), u.dot(false, true), "unknown: the config does not load")
	case s.Health == nil:
		u.printf("  %-8s %s answers at %s   %d ms   %s\n", u.bold("Panel"), u.dot(true, false), strings.TrimSuffix(s.URL, "/healthz"),
			s.Latency.Milliseconds(), u.dim(spark(v.latency)))
	default:
		u.printf("  %-8s %s %s: %v\n", u.bold("Panel"), u.dot(false, false), strings.TrimSuffix(s.URL, "/healthz"), s.Health)
	}

	if s.PID > 0 {
		cpu := "n/a"
		if s.CPU >= 0 {
			cpu = fmt.Sprintf("%.1f%%", s.CPU)
		}
		files := ""
		if s.Files > 0 {
			files = fmt.Sprintf("   files %d", s.Files)
		}
		u.printf("  %-8s pid %d   cpu %s   memory %s   threads %d%s\n", u.bold("Process"), s.PID, cpu, size(s.RSS), s.Threads, files)
	} else {
		u.printf("  %-8s %s\n", u.bold("Process"), u.dim("none"))
	}

	data := "database " + size(s.DBBytes)
	if s.WALBytes > 0 {
		data += " (+" + size(s.WALBytes) + " log)"
	}
	disk := u.paint("32", size(s.DiskFree)+" free")
	if s.DiskFree > 0 && s.DiskFree < minFreeBytes {
		disk = u.paint("31", size(s.DiskFree)+" free")
	}
	u.printf("  %-8s %s   disk %s\n", u.bold("Data"), data, disk)
	u.printf("  %-8s load %.2f %.2f %.2f   memory %s free of %s\n", u.bold("Host"), s.Load[0], s.Load[1], s.Load[2], size(s.MemAvail), size(s.MemTotal))

	if s.ConfigErr != "" {
		u.printf("  %-8s %s %s\n", u.bold("Config"), u.dot(false, false), s.ConfigErr)
	} else {
		u.printf("  %-8s %s valid   %s   %s\n", u.bold("Config"), u.dot(true, false), o.ConfigPath, u.dim("changed "+s.ConfigTime.Format("15:04")))
	}

	u.printf("\n  %s\n", u.bold("Events"))
	if len(v.events) == 0 {
		u.printf("    %s\n", u.dim("nothing changed since the watcher started"))
	}
	for _, e := range v.events {
		text := e.Text
		if e.Bad {
			text = u.paint("31", text)
		}
		u.printf("    %s  %s\n", u.dim(e.At.Format("15:04:05")), text)
	}

	u.printf("\n  %s\n", u.bold("Log of the service"))
	if len(s.Log) == 0 {
		u.printf("    %s\n", u.dim("no lines: journalctl is not available, or the service never ran"))
	}
	for _, l := range s.Log {
		if len(l) > 110 {
			l = l[:109] + "…"
		}
		lower := strings.ToLower(l)
		switch {
		case strings.Contains(lower, "level=error") || strings.Contains(lower, "error"):
			l = u.paint("31", l)
		case strings.Contains(lower, "level=warn") || strings.Contains(lower, "warn"):
			l = u.paint("33", l)
		default:
			l = u.dim(l)
		}
		u.printf("    %s\n", l)
	}
}

// Watch shows the state of the service and the host and keeps showing it, every
// o.WatchEvery, until ctx ends. With o.Once it prints one sample and returns an error when
// the service is not up, so a cron job or a monitor can use it.
func Watch(ctx context.Context, env Env, o Options, version string) error {
	u := ui{w: env.Out, color: env.Color}
	v := &view{}
	every := o.WatchEvery
	if every <= 0 {
		every = 2 * time.Second
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	live := env.Interactive && !o.Once
	if live {
		u.printf("\x1b[?25l") // hide the cursor
		defer u.printf("\x1b[?25h")
	}
	for n := 1; ; n++ {
		s := collect(ctx, env, o, v.prev)
		v.push(s)
		if live {
			u.printf("\x1b[H\x1b[J")
		}
		v.render(u, s, o, version)
		if live {
			u.printf("\n  %s\n", u.dim(fmt.Sprintf("every %s, Ctrl-C to stop", every)))
		}
		if o.Once {
			if !s.Up() {
				return errors.New("the service is not up")
			}
			return nil
		}
		if o.WatchCount > 0 && n >= o.WatchCount {
			return nil
		}
		if live && n > 1 && len(v.events) > 0 && v.events[len(v.events)-1].Bad && v.events[len(v.events)-1].At.Equal(s.At) {
			u.printf("\a") // a bell when something went wrong
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}
