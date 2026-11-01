package setup

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
)

// installed makes a machine with a valid config on disk and returns it with its options.
func installed(t *testing.T) (*fakeSys, Options) {
	t.Helper()
	f := newFake(t)
	o := f.opts()
	f.addUser(o.ServiceUser)
	f.uid = 0
	text := string(RenderConfig(baseAnswers(o))) + "snmp:\n  communities: [s3cr3t-community]\n"
	if err := os.MkdirAll(o.StateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	writeRaw(t, o, text)
	return f, o
}

func writeRaw(t *testing.T, o Options, text string) {
	t.Helper()
	if err := os.MkdirAll(dirOf(o.ConfigPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.ConfigPath, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func dirOf(p string) string { return p[:strings.LastIndex(p, "/")] }

// example is a valid value for each kind of setting.
var example = map[string]string{
	"scan.targets": "10.0.0.0/24", "scan.interval": "30m", "scan.ports": "22,80,443",
	"scan.portTimeout": "1s", "scan.concurrency": "64", "scan.retryAfter": "10m",
	"allowPublicTargets": "true", "discovery.probeRoutedTargets": "true",
	"discovery.nmap": "false", "discovery.nmapScope": "all", "discovery.mdns": "false", "discovery.ssdp": "false",
	"discovery.httpHeaders": "false", "discovery.routerTables": "false",
	"docker.enabled": "false", "docker.socket": "/run/docker.sock",
	"snmp.timeout": "3s", "history.keepScans": "50", "audit.keepDays": "30",
	"auth.minPasswordLength": "14", "auth.maxFailedLogins": "4", "auth.lockoutDuration": "20m",
	"auth.sessionIdleTimeout": "20m", "auth.sessionMaxAge": "8h", "export.chromePath": "/usr/bin/chromium",
	"discovery.nmapPath": "/usr/bin/nmap", "ssh.knownHosts": "/etc/ssh/ssh_known_hosts",
	"updates.check": "false", "metrics.enabled": "true",
}

func TestEverySettingIsAKeyOfTheConfigAndLoads(t *testing.T) {
	for _, g := range settingGroups {
		for _, st := range g.items {
			if st.kind == kSecrets {
				continue
			}
			t.Run(st.key(), func(t *testing.T) {
				f, o := installed(t)
				want, ok := example[st.key()]
				if !ok {
					t.Fatalf("no example value for %s", st.key())
				}
				if err := ConfigSet(context.Background(), f.env(""), o, st.key(), []string{want}); err != nil {
					t.Fatalf("set %s %s: %v", st.key(), want, err)
				}
				if _, err := config.Load(o.ConfigPath, false); err != nil {
					t.Fatalf("the config does not load after %s: %v", st.key(), err)
				}
				doc, _ := parseYAML([]byte(readFile(t, o.ConfigPath)))
				if got := value(doc.find(st.path)); !strings.Contains(got, strings.Split(want, ",")[0]) {
					t.Errorf("%s is %q after setting %q", st.key(), got, want)
				}
			})
		}
	}
}

func TestSetRefusesWhatWouldBreakTheConfigAndChangesNothing(t *testing.T) {
	f, o := installed(t)
	before := readFile(t, o.ConfigPath)
	for _, tc := range []struct{ key, value, want string }{
		{"scan.interval", "soon", "duration"},
		{"scan.targets", "8.8.8.0/24", "private ranges"},
		{"scan.ports", "22,99999", "port"},
		{"docker.enabled", "maybe", "on or off"},
		{"discovery.nmapScope", "some", "one of"},
		{"history.keepScans", "-3", "whole number"},
		{"nonsense.key", "1", "unknown key"},
	} {
		err := ConfigSet(context.Background(), f.env(""), o, tc.key, []string{tc.value})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("set %s %s: error %v, want %q", tc.key, tc.value, err, tc.want)
		}
	}
	if readFile(t, o.ConfigPath) != before {
		t.Error("a refused change altered the file")
	}
	if exists(o.ConfigPath + ".bak") {
		t.Error("a refused change left a backup")
	}
}

func TestSetKeepsABackupAndTheModeAndTheComments(t *testing.T) {
	f, o := installed(t)
	writeRaw(t, o, "# my notes\n"+readFile(t, o.ConfigPath))
	if err := ConfigSet(context.Background(), f.env(""), o, "scan.interval", []string{"1h"}); err != nil {
		t.Fatal(err)
	}
	text := readFile(t, o.ConfigPath)
	if !strings.Contains(text, "# my notes") || !strings.Contains(text, "interval: 1h") {
		t.Errorf("config:\n%s", text)
	}
	if !strings.Contains(readFile(t, o.ConfigPath+".bak"), "interval: 15m") {
		t.Error("the backup lacks the old value")
	}
	if info, _ := os.Stat(o.ConfigPath); info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	if exists(dirOf(o.ConfigPath) + "/.netscribe-check.yaml") {
		t.Error("the check file stayed behind")
	}
}

func TestSetListen(t *testing.T) {
	f, o := installed(t)
	if err := ConfigSet(context.Background(), f.env(""), o, "listen", []string{"127.0.0.1:9999"}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := config.Load(o.ConfigPath, false); cfg.Listen != "127.0.0.1:9999" {
		t.Errorf("listen %q", cfg.Listen)
	}
	if err := ConfigSet(context.Background(), f.env(""), o, "listen", []string{"a", "b"}); err == nil {
		t.Error("two addresses")
	}
	if err := ConfigSet(context.Background(), f.env(""), o, "listen", []string{"no way"}); err == nil {
		t.Error("an invalid address")
	}
}

func TestSetRestartsAKeyThatNeedsIt(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	if err := ConfigSet(context.Background(), f.env(""), o, "auth.minPasswordLength", []string{"16"}); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.ran(), "systemctl restart netscribe") {
		t.Errorf("a key that needs a restart must restart the service:\n%s", f.ran())
	}

	f.cmds = nil
	if err := ConfigSet(context.Background(), f.env(""), o, "scan.interval", []string{"20m"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.ran(), "restart") || !strings.Contains(f.out.String(), "applies this by itself") {
		t.Errorf("a hot key needs no restart:\n%s\n%s", f.ran(), f.out.String())
	}
}

func TestSecretsAreNeverShownNorSetFromTheCommandLine(t *testing.T) {
	f, o := installed(t)
	if err := ConfigSet(context.Background(), f.env(""), o, "snmp.communities", []string{"x"}); err == nil || !strings.Contains(err.Error(), "without echo") {
		t.Errorf("set a secret: %v", err)
	}
	var out strings.Builder
	env := f.env("")
	env.Out = &out
	if err := ConfigShow(env, o); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "s3cr3t-community") || !strings.Contains(out.String(), "***") {
		t.Errorf("show:\n%s", out.String())
	}
	out.Reset()
	if err := ConfigGet(env, o, "snmp.communities"); err != nil || strings.Contains(out.String(), "s3cr3t") || !strings.Contains(out.String(), "1 set") {
		t.Errorf("get: %q, %v", out.String(), err)
	}
}

func TestConfigGet(t *testing.T) {
	f, o := installed(t)
	var out strings.Builder
	env := f.env("")
	env.Out = &out
	if err := ConfigGet(env, o, "scan.interval"); err != nil || strings.TrimSpace(out.String()) != "15m" {
		t.Errorf("%q, %v", out.String(), err)
	}
	out.Reset()
	if err := ConfigGet(env, o, "auth.maxFailedLogins"); err != nil || !strings.Contains(out.String(), "default") {
		t.Errorf("a key that is not in the file shows its default: %q, %v", out.String(), err)
	}
	out.Reset()
	if err := ConfigGet(env, o, "listen"); err != nil || strings.TrimSpace(out.String()) != "127.0.0.1:8080" {
		t.Errorf("listen: %q, %v", out.String(), err)
	}
	if err := ConfigGet(env, o, "nope"); err == nil {
		t.Error("unknown key")
	}
}

func menu(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func TestConfigureChangesASettingThroughTheMenu(t *testing.T) {
	f, o := installed(t)
	// Scanning, "Scan every", the new value, Back, Back
	input := menu("2", "2", "45m", "y", "9", "15")
	if err := Configure(context.Background(), f.env(input), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if cfg, _ := config.Load(o.ConfigPath, false); cfg.Scan.Interval.Std().Minutes() != 45 {
		t.Errorf("interval %v", cfg.Scan.Interval.Std())
	}
	for _, want := range []string{"Settings of", "Scanning", "Scan every", "default, 15m"} {
		if want == "default, 15m" {
			continue
		}
		if !strings.Contains(f.out.String(), want) {
			t.Errorf("the menu lacks %q", want)
		}
	}
}

func TestConfigureToggleEnumAndSecrets(t *testing.T) {
	f, o := installed(t)
	f.secrets = []string{"another-community"}
	// Discovery: Nmap off->on? Use the mDNS toggle (item 3): answer n. Then the scope enum (item 2): all.
	// SNMP (menu 5): Communities (item 1): add one.
	input := menu("3", "3", "n", "y", "2", "2", "y", "8", "5", "1", "1", "y", "3", "15")
	if err := Configure(context.Background(), f.env(input), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	cfg, err := config.Load(o.ConfigPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Discovery.MDNS || cfg.Discovery.NmapScope != "all" || len(cfg.SNMP.Communities) != 2 {
		t.Errorf("mdns %v, scope %q, communities %d", cfg.Discovery.MDNS, cfg.Discovery.NmapScope, len(cfg.SNMP.Communities))
	}
	if strings.Contains(f.out.String(), "another-community") || strings.Contains(f.out.String(), "s3cr3t-community") {
		t.Error("a community was printed")
	}
}

func TestConfigureAccessWritesTLSAndDropsTheProxyList(t *testing.T) {
	f, o := installed(t)
	cert, key := writeKeyPair(t)
	// Panel access: every interface (3), TLS (2), the paths. The restart question is not asked: the service is stopped.
	input := menu("1", "3", "2", cert, key, "y", "15")
	if err := Configure(context.Background(), f.env(input), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	cfg, err := config.Load(o.ConfigPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "0.0.0.0:8080" || cfg.TLS.CertFile != cert || len(cfg.TrustedProxies) != 0 {
		t.Errorf("listen %q, tls %q, proxies %v", cfg.Listen, cfg.TLS.CertFile, cfg.TrustedProxies)
	}
}

func TestEnablingNmapOffersTheCapabilityDropIn(t *testing.T) {
	f, o := installed(t)
	f.bins["nmap"] = true
	// Discovery (3), Nmap (1): yes, then the drop-in question: yes
	input := menu("3", "1", "y", "y", "y", "8", "15")
	if err := Configure(context.Background(), f.env(input), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	drop := readFile(t, o.UnitDir+"/netscribe.service.d/"+dropInName)
	if !strings.Contains(drop, "CAP_NET_RAW") {
		t.Errorf("drop-in:\n%s", drop)
	}
	if !strings.Contains(f.ran(), "systemctl daemon-reload") {
		t.Errorf("commands:\n%s", f.ran())
	}
}

func TestConfigEditChecksTheResultAndLetsThePersonFixIt(t *testing.T) {
	f, o := installed(t)
	f.vars["EDITOR"] = "myeditor"
	env := f.env("y\ny\n") // after the first, broken edit: edit again, then save
	attempts := 0
	env.Run = func(_ context.Context, c Command) ([]byte, error) {
		if c.Name != "myeditor" {
			return nil, nil
		}
		attempts++
		text := readFile(t, c.Args[0])
		if attempts == 1 {
			text = strings.Replace(text, "interval: 15m", "interval: forever", 1)
		} else {
			text = strings.Replace(text, "interval: forever", "interval: 2h", 1)
		}
		return nil, os.WriteFile(c.Args[0], []byte(text), 0o600)
	}
	if err := ConfigEdit(context.Background(), env, o); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if attempts != 2 {
		t.Errorf("attempts %d", attempts)
	}
	if cfg, _ := config.Load(o.ConfigPath, false); cfg.Scan.Interval.Std().Hours() != 2 {
		t.Errorf("interval %v", cfg.Scan.Interval.Std())
	}
	if !strings.Contains(f.out.String(), "nothing was saved") {
		t.Errorf("the broken edit was not reported:\n%s", f.out.String())
	}

	// leaving the file as it was is not an error
	f2, o2 := installed(t)
	f2.vars["EDITOR"] = "true"
	if err := ConfigEdit(context.Background(), f2.env(""), o2); err != nil || !strings.Contains(f2.out.String(), "no change") {
		t.Errorf("%v\n%s", err, f2.out.String())
	}
	// no editor at all
	f3, o3 := installed(t)
	if err := ConfigEdit(context.Background(), f3.env(""), o3); err == nil || !strings.Contains(err.Error(), "no editor") {
		t.Errorf("no editor: %v", err)
	}
}

func TestSettingsNeedAConfig(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	if err := ConfigSet(context.Background(), f.env(""), o, "scan.interval", []string{"1h"}); err == nil || !strings.Contains(err.Error(), "netscribe setup") {
		t.Errorf("%v", err)
	}
}

// The default shown next to a setting is a fact about internal/config: this test reads it
// from there, so the two cannot drift apart.
func TestDefaultsShownMatchTheConfig(t *testing.T) {
	path := dirOf(t.TempDir()) + "/defaults.yaml"
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:8080\ndatabase: /tmp/x.db\nscan:\n  targets: [10.0.0.0/24]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	onOff := func(b bool) string { return map[bool]string{true: "on", false: "off"}[b] }
	ports := make([]string, len(c.Scan.Ports))
	for i, p := range c.Scan.Ports {
		ports[i] = strconv.Itoa(p)
	}
	want := map[string]string{
		"scan.interval": c.Scan.Interval.Std().String(), "scan.ports": strings.Join(ports, ", "),
		"allowPublicTargets": onOff(c.AllowPublicTargets), "discovery.probeRoutedTargets": onOff(c.Discovery.ProbeRoutedTargets),
		"discovery.nmap": onOff(c.Discovery.Nmap), "discovery.nmapScope": c.Discovery.NmapScope,
		"discovery.mdns": onOff(c.Discovery.MDNS), "discovery.ssdp": onOff(c.Discovery.SSDP),
		"discovery.httpHeaders": onOff(c.Discovery.HTTPHeaders), "discovery.routerTables": onOff(c.Discovery.RouterTables),
		"docker.enabled": onOff(c.Docker.Enabled), "docker.socket": c.Docker.Socket,
		"snmp.timeout": c.SNMP.Timeout.Std().String(), "history.keepScans": strconv.Itoa(c.History.KeepScans),
		"audit.keepDays": strconv.Itoa(c.Audit.KeepDays), "auth.minPasswordLength": strconv.Itoa(c.Auth.MinPasswordLength),
		"auth.maxFailedLogins": strconv.Itoa(c.Auth.MaxFailedLogins), "auth.lockoutDuration": c.Auth.LockoutDuration.Std().String(),
		"auth.sessionIdleTimeout": c.Auth.SessionIdleTimeout.Std().String(), "auth.sessionMaxAge": c.Auth.SessionMaxAge.Std().String(),
	}
	for key, actual := range want {
		st, ok := findSetting(key)
		if !ok {
			t.Errorf("no setting %s", key)
			continue
		}
		if d, err := time.ParseDuration(st.def); err == nil {
			if a, _ := time.ParseDuration(actual); a != d {
				t.Errorf("%s: shown default %s, real %s", key, st.def, actual)
			}
			continue
		}
		if st.def != actual {
			t.Errorf("%s: shown default %q, real %q", key, st.def, actual)
		}
	}
}

// writeBin puts a file where the binary of the install would be.
func writeBin(o Options) error {
	if err := os.MkdirAll(dirOf(o.BinPath), 0o750); err != nil {
		return err
	}
	return os.WriteFile(o.BinPath, []byte("x"), 0o600)
}
