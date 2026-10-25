package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/config"
)

// byTitle finds the finding whose title contains text.
func byTitle(t *testing.T, list []Finding, text string) Finding {
	t.Helper()
	for _, f := range list {
		if strings.Contains(f.Title, text) {
			return f
		}
	}
	t.Fatalf("no finding with %q in %v", text, titles(list))
	return Finding{}
}

func titles(list []Finding) []string {
	out := make([]string, len(list))
	for i, f := range list {
		out[i] = f.Title
	}
	return out
}

func hasTitle(list []Finding, text string) bool {
	for _, f := range list {
		if strings.Contains(f.Title, text) {
			return true
		}
	}
	return false
}

func writeConfig(t *testing.T, o Options, a Answers) config.Config {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(o.ConfigPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.ConfigPath, RenderConfig(a), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(o.ConfigPath, false)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func baseAnswers(o Options) Answers {
	return Answers{Listen: "127.0.0.1:8080", Database: filepath.Join(o.StateDir, "db"), Targets: []string{"192.168.1.0/24"}, Interval: "15m", DockerSocket: "/var/run/docker.sock"}
}

func TestCheckSystem(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	env := f.env("")
	env.GOOS = "darwin"
	if got := checkSystem(env, o); len(got) != 1 || got[0].Level != LevelFail {
		t.Errorf("a system other than Linux is a failure: %v", got)
	}

	env = f.env("")
	f.uid = 1000
	if got := byTitle(t, checkSystem(env, o), "Not running as root"); got.Level != LevelWarn || got.Fix == "" {
		t.Errorf("a missing root needs a warning with a fix: %+v", got)
	}

	f.uid, f.systemd = 0, false
	if got := byTitle(t, checkSystem(f.env(""), o), "systemd is not running"); got.Level != LevelInfo {
		t.Errorf("no systemd is information, not a failure: %+v", got)
	}

	env = f.env("")
	env.FreeBytes = func(string) (uint64, error) { return 10 << 20, nil }
	if got := byTitle(t, checkSystem(env, o), "MiB free"); got.Level != LevelWarn {
		t.Errorf("10 MiB free must warn: %+v", got)
	}
}

func TestCheckInstall(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	got := checkInstall(f.env(""), o)
	for _, want := range []string{"not registered", "No service account", "No state directory"} {
		if byTitle(t, got, want).Level != LevelWarn {
			t.Errorf("%q must warn", want)
		}
	}

	f.addUser(o.ServiceUser)
	f.exe = o.BinPath
	if err := os.MkdirAll(o.StateDir, 0o755); err != nil { //nolint:gosec // a deliberately open directory
		t.Fatal(err)
	}
	got = checkInstall(f.env(""), o)
	if byTitle(t, got, "readable by every user").Level != LevelWarn {
		t.Error("a state directory open to everyone must warn")
	}
	if err := os.Chmod(o.StateDir, 0o750); err != nil { //nolint:gosec // a directory mode, not a file mode
		t.Fatal(err)
	}
	for _, f := range checkInstall(f.env(""), o) {
		if f.Level != LevelOK {
			t.Errorf("a complete install has a problem: %+v", f)
		}
	}
}

func TestCheckConfig(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	f.addUser(o.ServiceUser)

	cfg, got := checkConfig(f.env(""), o)
	if cfg != nil || byTitle(t, got, "No config").Level != LevelWarn {
		t.Errorf("a missing config is a warning: %v", got)
	}

	writeConfig(t, o, baseAnswers(o))
	cfg, got = checkConfig(f.env(""), o)
	if cfg == nil || byTitle(t, got, "valid").Level != LevelOK {
		t.Errorf("a valid config must load: %v", got)
	}

	if err := os.Chmod(o.ConfigPath, 0o644); err != nil { //nolint:gosec // the permission is the subject of the test
		t.Fatal(err)
	}
	cfg, got = checkConfig(f.env(""), o)
	bad := byTitle(t, got, "not valid")
	if cfg != nil || bad.Level != LevelFail || bad.Fix != "chmod 600 "+o.ConfigPath {
		t.Errorf("a loose mode needs the chmod fix: %+v", bad)
	}

	if err := os.WriteFile(o.ConfigPath, []byte("listen: nonsense\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, got = checkConfig(f.env(""), o); cfg != nil || byTitle(t, got, "not valid").Level != LevelFail {
		t.Errorf("an invalid config is a failure: %v", got)
	}
}

func TestCheckConfigFailsWhenAnotherUserOwnsTheFile(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	writeConfig(t, o, baseAnswers(o))
	acc := f.addUser(o.ServiceUser)
	acc.UID++ // the file belongs to the user of the test, not to the account
	f.users[o.ServiceUser] = acc
	_, got := checkConfig(f.env(""), o)
	if g := byTitle(t, got, "cannot read the config"); g.Level != LevelFail || !strings.Contains(g.Fix, "chown "+o.ServiceUser) {
		t.Errorf("a config of the wrong owner must fail with the chown fix: %+v", g)
	}
}

func TestHealthURL(t *testing.T) {
	for _, tc := range []struct{ listen, tls, want string }{
		{"127.0.0.1:8080", "", "http://127.0.0.1:8080/healthz"},
		{"0.0.0.0:9000", "", "http://127.0.0.1:9000/healthz"},
		{":8080", "", "http://127.0.0.1:8080/healthz"},
		{"[::]:8080", "", "http://127.0.0.1:8080/healthz"},
		{"192.168.1.5:443", "/c.pem", "https://192.168.1.5:443/healthz"},
	} {
		cfg := config.Config{Listen: tc.listen}
		cfg.TLS.CertFile = tc.tls
		if got := healthURL(cfg); got != tc.want {
			t.Errorf("healthURL(%q) = %q, want %q", tc.listen, got, tc.want)
		}
	}
}

func TestCheckService(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	cfg := config.Config{Listen: "127.0.0.1:8080"}

	if active, got := checkService(context.Background(), f.env(""), o, cfg); active || byTitle(t, got, "unit is not installed").Level != LevelWarn {
		t.Errorf("no unit: %v", got)
	}
	if err := os.MkdirAll(o.UnitDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.UnitDir, "netscribe.service"), []byte("[Unit]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if active, got := checkService(context.Background(), f.env(""), o, cfg); active || byTitle(t, got, "not running").Fix == "" {
		t.Errorf("a stopped service needs the start command: %v", got)
	}

	f.active = "active"
	if active, got := checkService(context.Background(), f.env(""), o, cfg); !active || byTitle(t, got, "answers").Level != LevelOK {
		t.Errorf("a healthy service: %v", got)
	}
	f.health = os.ErrDeadlineExceeded
	if _, got := checkService(context.Background(), f.env(""), o, cfg); byTitle(t, got, "does not answer").Level != LevelFail {
		t.Errorf("a service that does not answer is a failure: %v", got)
	}
}

func TestCheckNetworkOnTheListenAddress(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	f.addUser(o.ServiceUser)
	cfg := writeConfig(t, o, baseAnswers(o))

	if byTitle(t, checkNetwork(f.env(""), o, cfg, false), "this host only").Level != LevelOK {
		t.Error("loopback is fine")
	}

	cfg.Listen = "0.0.0.0:8080"
	if g := byTitle(t, checkNetwork(f.env(""), o, cfg, false), "without TLS"); g.Level != LevelWarn || !strings.Contains(g.Fix, "trustedProxies") {
		t.Errorf("an open listen address without TLS must warn: %+v", g)
	}
	cfg.TrustedProxies = []string{"10.0.0.1/32"}
	if byTitle(t, checkNetwork(f.env(""), o, cfg, false), "behind TLS").Level != LevelOK {
		t.Error("a trusted proxy counts as TLS in front")
	}

	cfg.Listen = "127.0.0.1:8080"
	f.listen = os.ErrExist
	if byTitle(t, checkNetwork(f.env(""), o, cfg, false), "Cannot bind").Level != LevelWarn {
		t.Error("a busy port must warn while the service is stopped")
	}
	if hasTitle(checkNetwork(f.env(""), o, cfg, true), "Cannot bind") {
		t.Error("the running service holds the port itself: no warning")
	}
}

func TestCheckNetworkOnTheTargets(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	f.addUser(o.ServiceUser)
	cfg := writeConfig(t, o, baseAnswers(o))

	if got := byTitle(t, checkNetwork(f.env(""), o, cfg, true), "scan targets are networks of this host"); got.Level != LevelOK {
		t.Errorf("a target on eth0: %+v", got)
	}

	cfg.Scan.Targets = []string{"10.9.0.0/24"}
	got := byTitle(t, checkNetwork(f.env(""), o, cfg, true), "No scan target")
	if got.Level != LevelWarn || !strings.Contains(got.Fix, "192.168.1.0/24") {
		t.Errorf("a target that matches nothing must warn and propose the real network: %+v", got)
	}
	cfg.Discovery.ProbeRoutedTargets = true
	if hasTitle(checkNetwork(f.env(""), o, cfg, true), "No scan target") {
		t.Error("routed targets are meant to be off-link")
	}

	cfg.Scan.Targets = []string{"10.0.0.0/16"}
	if !hasTitle(checkNetwork(f.env(""), o, cfg, true), "65 534") {
		t.Error("a /16 target needs a note about the scan time")
	}
}

func TestPingAllowed(t *testing.T) {
	f := newFake(t)
	for _, tc := range []struct {
		ping                    string
		gid                     uint32
		allowed, disabled, read bool
	}{
		{"1\t0\n", 1500, false, true, true},
		{"0\t2147483647\n", 1500, true, false, true},
		{"1000\t2000\n", 1500, true, false, true},
		{"1000\t2000\n", 3000, false, false, true},
		{"garbage", 1500, false, false, false},
		{"", 1500, false, false, false},
	} {
		f.ping = tc.ping
		a, d, k := pingAllowed(f.env(""), tc.gid)
		if a != tc.allowed || d != tc.disabled || k != tc.read {
			t.Errorf("pingAllowed(%q, %d) = %v %v %v", tc.ping, tc.gid, a, d, k)
		}
	}
}

func TestCheckICMP(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	if checkICMP(f.env(""), o) != nil {
		t.Error("without the account there is nothing to say")
	}
	acc := f.addUser(o.ServiceUser)
	got := checkICMP(f.env(""), o)
	if len(got) != 1 || got[0].Level != LevelWarn || !strings.Contains(got[0].Fix, "ping_group_range") {
		t.Errorf("a disabled range must warn with the sysctl fix: %v", got)
	}
	f.ping = "0\t2147483647\n"
	if got := checkICMP(f.env(""), o); len(got) != 1 || got[0].Level != LevelOK {
		t.Errorf("an open range is fine: %v (account gid %d)", got, acc.GID)
	}
}

func TestCheckFeaturesNmapAndChrome(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	cfg := writeConfig(t, o, baseAnswers(o))

	cfg.Discovery.Nmap = true
	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "nmap was not found"); g.Level != LevelWarn {
		t.Errorf("nmap on, binary missing: %+v", g)
	}

	f.bins["nmap"] = true
	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "without raw-socket rights"); g.Level != LevelWarn {
		t.Errorf("nmap on, no capability: %+v", g)
	}

	dir := filepath.Join(o.UnitDir, "netscribe.service.d")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "10.conf"), []byte("[Service]\nAmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "Nmap is on"); g.Level != LevelOK {
		t.Errorf("nmap with the capability: %+v", g)
	}

	cfg.Discovery.Nmap = false
	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "nmap is installed"); g.Level != LevelInfo {
		t.Errorf("an installed, unused nmap is a hint: %+v", g)
	}

	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "no browser"); g.Level != LevelInfo {
		t.Errorf("no browser is information: %+v", g)
	}
	f.bins["chromium"] = true
	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "has a browser"); g.Level != LevelOK {
		t.Errorf("a browser: %+v", g)
	}
}

func TestCheckFeaturesDocker(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	acc := f.addUser(o.ServiceUser)
	cfg := writeConfig(t, o, baseAnswers(o))
	cfg.Docker.Enabled = true

	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "no socket"); g.Level != LevelWarn {
		t.Errorf("no socket: %+v", g)
	}

	sock := cfg.Docker.Socket
	f.sockets[sock] = SocketInfo{Mode: 0o660, GID: acc.GID + 100, Group: "docker"}
	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "cannot use the Docker socket"); g.Level != LevelWarn {
		t.Errorf("a socket of a group the account is not in: %+v", g)
	}

	dir := filepath.Join(o.UnitDir, "netscribe.service.d")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "10.conf"), []byte("[Service]\nSupplementaryGroups=docker\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "adds the group docker"); g.Level != LevelOK {
		t.Errorf("a drop-in with the group: %+v", g)
	}

	f.sockets[sock] = SocketInfo{Mode: 0o660, GID: acc.GID}
	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "can use the Docker socket"); g.Level != LevelOK {
		t.Errorf("an account in the group: %+v", g)
	}
	f.sockets[sock] = SocketInfo{Mode: 0o666, GID: 5}
	if g := byTitle(t, checkFeatures(f.env(""), o, cfg), "open to every user"); g.Level != LevelOK {
		t.Errorf("a world-writable socket: %+v", g)
	}
}

func TestCheckFeaturesTellsAboutTheSNMPCommunity(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	cfg := writeConfig(t, o, baseAnswers(o))
	cfg.SNMP.Communities = []config.Secret{"public"}
	if !hasTitle(checkFeatures(f.env(""), o, cfg), "community public") {
		t.Error("the default community deserves a hint")
	}
	cfg.SNMP.Communities = []config.Secret{"s3cr3t-ro"}
	for _, g := range checkFeatures(f.env(""), o, cfg) {
		if strings.Contains(g.Title+g.Detail+g.Fix, "s3cr3t") {
			t.Errorf("a finding repeats the community: %+v", g)
		}
	}
}

func TestCheckAccounts(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	f.addUser(o.ServiceUser)
	if err := os.MkdirAll(filepath.Dir(o.BinPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.BinPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	f.userList = "USERNAME  ROLE  STATUS  LAST LOGIN\n"
	got := checkAccounts(context.Background(), f.env(""), o)
	if len(got) != 1 || got[0].Level != LevelWarn || !strings.Contains(got[0].Fix, "user add") {
		t.Errorf("an empty table needs the command that adds the administrator: %v", got)
	}
	f.userList += "alice  admin  enabled  never\nbob  viewer  enabled  never\n"
	if got := checkAccounts(context.Background(), f.env(""), o); len(got) != 1 || got[0].Title != "2 accounts exist" {
		t.Errorf("two accounts: %v", got)
	}
	f.uid = 1000
	if got := checkAccounts(context.Background(), f.env(""), o); got != nil {
		t.Errorf("only root can ask as the service account: %v", got)
	}
}

func TestDoctorFailsOnAFailureAndPrintsEveryArea(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	f.addUser(o.ServiceUser)
	writeConfig(t, o, baseAnswers(o))
	if err := os.WriteFile(o.ConfigPath, []byte("listen: nonsense\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Doctor(context.Background(), f.env(""), o, "netscribe test")
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("an invalid config must make doctor fail: %v", err)
	}
	for _, area := range []string{"System", "Install", "Config"} {
		if !strings.Contains(f.out.String(), area) {
			t.Errorf("the report lacks the area %s:\n%s", area, f.out.String())
		}
	}
}

func TestDoctorPassesOnAHealthyInstall(t *testing.T) {
	f := newFake(t)
	f.ping = "0\t2147483647\n"
	f.active = "active"
	f.userList = "USERNAME  ROLE  STATUS  LAST LOGIN\nalice  admin  enabled  never\n"
	o := f.opts()
	f.exe = o.BinPath
	f.addUser(o.ServiceUser)
	writeConfig(t, o, baseAnswers(o))
	if err := os.MkdirAll(o.StateDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(o.UnitDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(o.UnitDir, "netscribe.service"), []byte("[Unit]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(o.BinPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.BinPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Doctor(context.Background(), f.env(""), o, "netscribe test"); err != nil {
		t.Fatalf("a healthy install: %v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.out.String(), "0 failures") {
		t.Errorf("the summary line is missing:\n%s", f.out.String())
	}
}
