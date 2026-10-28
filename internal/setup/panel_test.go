package setup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
)

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestEverySettingHasAValueReaderForThePanel(t *testing.T) {
	for _, g := range settingGroups {
		for _, st := range g.items {
			if _, ok := panelGetters[st.key()]; !ok {
				t.Errorf("setting %s has no value reader for the panel", st.key())
			}
		}
	}
	if len(panelGetters) != countSettings() {
		t.Errorf("%d readers for %d settings: a reader without a setting is dead code", len(panelGetters), countSettings())
	}
}

func countSettings() int {
	n := 0
	for _, g := range settingGroups {
		n += len(g.items)
	}
	return n
}

func TestPanelSettingsListsTheValuesInUseAndNeverASecret(t *testing.T) {
	f, o := installed(t)
	groups, err := PanelSettings(f.env(""), o)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != len(settingGroups) {
		t.Fatalf("%d groups, want %d", len(groups), len(settingGroups))
	}
	all := map[string]PanelSetting{}
	for _, g := range groups {
		for _, st := range g.Settings {
			all[st.Key] = st
		}
	}
	if got := all["scan.interval"].Value; got != "15m" {
		t.Errorf("scan.interval = %v, want 15m", got)
	}
	if got := all["discovery.mdns"].Value; got != true {
		t.Errorf("discovery.mdns = %v, want the default true", got)
	}
	if !all["scan.interval"].Hot || all["discovery.mdns"].Hot {
		t.Error("scan.interval applies at once and discovery.mdns needs a restart")
	}
	comm := all["snmp.communities"]
	if comm.Value != nil || comm.Count != 1 {
		t.Errorf("communities = %+v: the panel gets how many, never which", comm)
	}
	body, _ := json.Marshal(groups)
	if strings.Contains(string(body), "s3cr3t-community") {
		t.Error("a community reached the answer")
	}
}

func TestApplyPanelSettingsChangesSeveralKeysInOneSave(t *testing.T) {
	f, o := installed(t)
	res, err := ApplyPanelSettings(f.env(""), o, map[string]json.RawMessage{
		"scan.interval":  raw("45m"),
		"scan.ports":     raw([]any{22, "443", 8080}),
		"discovery.mdns": raw(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Saved, ",") != "discovery.mdns,scan.interval,scan.ports" {
		t.Errorf("saved = %v", res.Saved)
	}
	if strings.Join(res.NeedRestart, ",") != "discovery.mdns" {
		t.Errorf("needRestart = %v, want only discovery.mdns", res.NeedRestart)
	}
	cfg, err := config.Load(o.ConfigPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scan.Interval.Std() != 45*time.Minute || cfg.Discovery.MDNS || len(cfg.Scan.Ports) != 3 {
		t.Errorf("interval %v, mdns %v, ports %v", cfg.Scan.Interval.Std(), cfg.Discovery.MDNS, cfg.Scan.Ports)
	}
	if !exists(o.ConfigPath + ".bak") {
		t.Error("the old config was not kept as a .bak")
	}
}

func TestApplyPanelSettingsRefusesAndChangesNothing(t *testing.T) {
	f, o := installed(t)
	before := readFile(t, o.ConfigPath)
	for name, changes := range map[string]map[string]json.RawMessage{
		"unknown key":           {"nonsense": raw(1)},
		"listen is not here":    {"listen": raw("0.0.0.0:80")},
		"wrong type":            {"scan.interval": raw(15)},
		"bad duration":          {"scan.interval": raw("soon")},
		"bool as text":          {"discovery.mdns": raw("yes")},
		"public target":         {"scan.targets": raw([]string{"8.8.8.0/24"})},
		"no targets":            {"scan.targets": raw([]string{})},
		"bad port":              {"scan.ports": raw([]any{99999})},
		"control character":     {"docker.socket": raw("/run/\nx")},
		"short password":        {"auth.minPasswordLength": raw(8)},
		"one bad among good":    {"scan.interval": raw("30m"), "history.keepScans": raw(-1)},
		"fraction for a number": {"history.keepScans": raw(1.5)},
	} {
		_, err := ApplyPanelSettings(f.env(""), o, changes)
		var bad PanelInputError
		if err == nil || !asInput(err, &bad) {
			t.Errorf("%s: err = %v, want a PanelInputError", name, err)
		}
		if readFile(t, o.ConfigPath) != before {
			t.Fatalf("%s: a refused change altered the file", name)
		}
	}
	if exists(o.ConfigPath + ".bak") {
		t.Error("a refused change left a .bak")
	}
}

func asInput(err error, target *PanelInputError) bool {
	if e, ok := err.(PanelInputError); ok { //nolint:errorlint // PanelInputError is returned as a value
		*target = e
		return true
	}
	return false
}

func TestApplyPanelSettingsReplacesSecretsWithoutEchoingThem(t *testing.T) {
	f, o := installed(t)
	res, err := ApplyPanelSettings(f.env(""), o, map[string]json.RawMessage{"snmp.communities": raw([]string{"first-secret", "second-secret"})})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(res)
	if strings.Contains(string(body), "secret") {
		t.Errorf("the answer holds a community: %s", body)
	}
	cfg, err := config.Load(o.ConfigPath, false)
	if err != nil || len(cfg.SNMP.Communities) != 2 {
		t.Fatalf("communities %d, %v", len(cfg.SNMP.Communities), err)
	}
	if _, err := ApplyPanelSettings(f.env(""), o, map[string]json.RawMessage{"snmp.communities": raw([]string{})}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := config.Load(o.ConfigPath, false); len(cfg.SNMP.Communities) != 0 {
		t.Errorf("communities = %d after an empty list", len(cfg.SNMP.Communities))
	}
}

func TestApplyPanelSettingsKeepsTheCommentsOfTheFile(t *testing.T) {
	f, o := installed(t)
	writeRaw(t, o, "# my network\nscan:\n  targets: [192.168.1.0/24] # the office\n  interval: 15m\n")
	if _, err := ApplyPanelSettings(f.env(""), o, map[string]json.RawMessage{"scan.interval": raw("1h")}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, o.ConfigPath)
	for _, want := range []string{"# my network", "# the office", "interval: 1h"} {
		if !strings.Contains(got, want) {
			t.Errorf("the file lost %q:\n%s", want, got)
		}
	}
}

func TestShortDurationDropsTheZeroTail(t *testing.T) {
	for in, want := range map[time.Duration]string{
		15 * time.Minute: "15m", time.Hour: "1h", 90 * time.Minute: "1h30m", 2 * time.Second: "2s",
		168 * time.Hour: "168h", 0: "0",
	} {
		if got := shortDuration(in); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", in, got, want)
		}
	}
}
