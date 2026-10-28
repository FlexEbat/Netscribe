package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/FlexEbat/Netscribe/internal/config"
)

// The panel edits the same settings as netscribe config. These types are what the
// web interface receives; the groups, the keys and the checks are the console's own.

// PanelSetting is one setting as the panel shows it.
type PanelSetting struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Help    string   `json:"help"`
	Type    string   `json:"type"`  // bool, int, duration, text, list, ports, enum, secrets
	Value   any      `json:"value"` // bool, int, string or []string; nil for secrets
	Default string   `json:"default"`
	Choices []string `json:"choices,omitempty"`
	Hot     bool     `json:"hot"`             // a running service applies a change by itself
	Count   int      `json:"count,omitempty"` // secrets: how many are set, never what they are
}

// PanelGroup is a group of settings.
type PanelGroup struct {
	Name     string         `json:"name"`
	Hint     string         `json:"hint"`
	Settings []PanelSetting `json:"settings"`
}

// PanelResult says what a change did.
type PanelResult struct {
	Saved       []string `json:"saved"`       // the keys written
	NeedRestart []string `json:"needRestart"` // of those, the keys that wait for a restart
}

// PanelInputError is a refusal that the person can fix: an unknown key, a value of the wrong
// kind, a config that would not be valid. Any other error is a failure of the system.
type PanelInputError struct{ Msg string }

func (e PanelInputError) Error() string { return e.Msg }

const (
	maxPanelItems    = 200
	maxPanelItemSize = 256
)

// panelGetters read the effective value of a key from the loaded config. A test checks that
// every setting has one, so a new setting cannot reach the panel without a value.
var panelGetters = map[string]func(c config.Config) any{
	"scan.targets":                 func(c config.Config) any { return nonNil(c.Scan.Targets) },
	"scan.interval":                func(c config.Config) any { return shortDuration(c.Scan.Interval.Std()) },
	"scan.ports":                   func(c config.Config) any { return portStrings(c.Scan.Ports) },
	"scan.portTimeout":             func(c config.Config) any { return shortDuration(c.Scan.PortTimeout.Std()) },
	"scan.concurrency":             func(c config.Config) any { return c.Scan.Concurrency },
	"scan.retryAfter":              func(c config.Config) any { return shortDuration(c.Scan.RetryAfter.Std()) },
	"allowPublicTargets":           func(c config.Config) any { return c.AllowPublicTargets },
	"discovery.probeRoutedTargets": func(c config.Config) any { return c.Discovery.ProbeRoutedTargets },
	"discovery.nmap":               func(c config.Config) any { return c.Discovery.Nmap },
	"discovery.nmapScope":          func(c config.Config) any { return orDefault(c.Discovery.NmapScope, "untyped") },
	"discovery.mdns":               func(c config.Config) any { return c.Discovery.MDNS },
	"discovery.ssdp":               func(c config.Config) any { return c.Discovery.SSDP },
	"discovery.httpHeaders":        func(c config.Config) any { return c.Discovery.HTTPHeaders },
	"discovery.routerTables":       func(c config.Config) any { return c.Discovery.RouterTables },
	"discovery.nmapPath":           func(c config.Config) any { return c.Discovery.NmapPath },
	"docker.enabled":               func(c config.Config) any { return c.Docker.Enabled },
	"docker.socket":                func(c config.Config) any { return c.Docker.Socket },
	"snmp.communities":             func(config.Config) any { return nil },
	"snmp.timeout":                 func(c config.Config) any { return shortDuration(c.SNMP.Timeout.Std()) },
	"history.keepScans":            func(c config.Config) any { return c.History.KeepScans },
	"audit.keepDays":               func(c config.Config) any { return c.Audit.KeepDays },
	"auth.minPasswordLength":       func(c config.Config) any { return c.Auth.MinPasswordLength },
	"auth.maxFailedLogins":         func(c config.Config) any { return c.Auth.MaxFailedLogins },
	"auth.lockoutDuration":         func(c config.Config) any { return shortDuration(c.Auth.LockoutDuration.Std()) },
	"auth.sessionIdleTimeout":      func(c config.Config) any { return shortDuration(c.Auth.SessionIdleTimeout.Std()) },
	"auth.sessionMaxAge":           func(c config.Config) any { return shortDuration(c.Auth.SessionMaxAge.Std()) },
	"export.chromePath":            func(c config.Config) any { return c.Export.ChromePath },
	"ssh.knownHosts":               func(c config.Config) any { return c.SSH.KnownHosts },
	"updates.check":                func(c config.Config) any { return c.Updates.Check },
}

func nonNil(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func portStrings(ports []int) []string {
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, strconv.Itoa(p))
	}
	return out
}

// shortDuration prints 15m, not 15m0s.
func shortDuration(d time.Duration) string {
	if d == 0 {
		return "0"
	}
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = s[:len(s)-2]
	}
	if strings.HasSuffix(s, "h0m") {
		s = s[:len(s)-2]
	}
	return s
}

func panelType(k kind) string {
	switch k {
	case kBool:
		return "bool"
	case kInt:
		return "int"
	case kDuration:
		return "duration"
	case kList, kTargets:
		return "list"
	case kPorts:
		return "ports"
	case kEnum:
		return "enum"
	case kSecrets:
		return "secrets"
	}
	return "text"
}

// PanelSettings lists every setting with the value the service uses now.
func PanelSettings(env Env, o Options) ([]PanelGroup, error) {
	s := newSession(env, o)
	cfg, err := s.loadConfig()
	if err != nil {
		return nil, err
	}
	doc, err := s.openConfig()
	if err != nil {
		return nil, err
	}
	out := make([]PanelGroup, 0, len(settingGroups))
	for _, g := range settingGroups {
		pg := PanelGroup{Name: g.name, Hint: g.hint}
		for _, st := range g.items {
			get, ok := panelGetters[st.key()]
			if !ok {
				return nil, fmt.Errorf("setting %s has no value reader", st.key())
			}
			ps := PanelSetting{
				Key: st.key(), Label: st.label, Help: st.help, Type: panelType(st.kind),
				Value: get(*cfg), Default: st.def, Choices: st.choices, Hot: st.hot,
			}
			if st.kind == kSecrets {
				ps.Count = count(doc.find(st.path))
			}
			pg.Settings = append(pg.Settings, ps)
		}
		out = append(out, pg)
	}
	return out, nil
}

// ApplyPanelSettings changes several keys as one edit: every value is checked, the whole
// config is checked, and the file is replaced once with a .bak of the old one. When any
// value is refused nothing is written.
func ApplyPanelSettings(env Env, o Options, changes map[string]json.RawMessage) (PanelResult, error) {
	s := newSession(env, o)
	doc, err := s.openConfig()
	if err != nil {
		return PanelResult{}, err
	}
	keys := make([]string, 0, len(changes))
	for k := range changes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var res PanelResult
	for _, key := range keys {
		st, ok := findSetting(key)
		if !ok {
			return PanelResult{}, PanelInputError{Msg: "unknown setting " + strconv.Quote(clip(key, 64))}
		}
		items, err := panelItems(st, changes[key])
		if err != nil {
			return PanelResult{}, PanelInputError{Msg: st.label + ": " + err.Error()}
		}
		if err := setPanelValue(doc, st, items); err != nil {
			return PanelResult{}, PanelInputError{Msg: st.label + ": " + err.Error()}
		}
		res.Saved = append(res.Saved, key)
		if !st.hot {
			res.NeedRestart = append(res.NeedRestart, key)
		}
	}
	data, err := doc.bytes()
	if err != nil {
		return PanelResult{}, err
	}
	if err := s.validate(data); err != nil {
		return PanelResult{}, PanelInputError{Msg: "the settings would not be valid, nothing was saved: " + err.Error()}
	}
	if err := s.saveConfig(data); err != nil {
		return PanelResult{}, err
	}
	return res, nil
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// panelItems turns the JSON value of a change into the words node() understands.
func panelItems(st setting, raw json.RawMessage) ([]string, error) {
	switch st.kind {
	case kBool:
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, errors.New("must be true or false")
		}
		return []string{strconv.FormatBool(b)}, nil
	case kInt:
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return nil, errors.New("must be a whole number")
		}
		return []string{strconv.Itoa(n)}, nil
	case kDuration, kEnum, kText:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, errors.New("must be text")
		}
		if err := plainText(v); err != nil {
			return nil, err
		}
		return []string{v}, nil
	}
	var arr []any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, errors.New("must be a list")
	}
	if len(arr) > maxPanelItems {
		return nil, fmt.Errorf("has more than %d entries", maxPanelItems)
	}
	var out []string
	for _, it := range arr {
		var v string
		switch x := it.(type) {
		case string:
			v = strings.TrimSpace(x)
		case float64:
			if x != float64(int(x)) {
				return nil, errors.New("must hold whole numbers")
			}
			v = strconv.Itoa(int(x))
		default:
			return nil, errors.New("must hold text")
		}
		if v == "" {
			continue
		}
		if err := plainText(v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// plainText refuses control characters and very long values.
func plainText(v string) error {
	if len(v) > maxPanelItemSize {
		return fmt.Errorf("is longer than %d characters", maxPanelItemSize)
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return errors.New("holds a control character")
		}
	}
	return nil
}

func setPanelValue(doc *yamlDoc, st setting, items []string) error {
	switch st.kind {
	case kSecrets:
		if len(items) == 0 {
			doc.remove(st.path)
			return nil
		}
		return doc.set(st.path, listNode(items, false))
	case kTargets:
		if len(items) == 0 {
			return errors.New("name at least one network")
		}
	}
	n, err := node(st, items)
	if err != nil {
		return err
	}
	return doc.set(st.path, n)
}
