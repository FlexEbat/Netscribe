package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/config"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test paths
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func yesOptions(f *fakeSys) Options {
	o := f.opts()
	o.Yes = true
	o.Admin = "alice"
	o.PasswordStdin = true
	return o
}

func TestSetupFreshInstallWithYes(t *testing.T) {
	f := newFake(t)
	o := yesOptions(f)
	env := f.env("S3cure-passphrase\n")
	if err := Setup(context.Background(), env, o, "netscribe test"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}

	if len(f.copied) != 1 || f.copied[0][1] != o.BinPath {
		t.Errorf("the binary was not registered: %v", f.copied)
	}
	cfg, err := config.Load(o.ConfigPath, false)
	if err != nil {
		t.Fatalf("the written config does not load: %v", err)
	}
	if got := strings.Join(cfg.Scan.Targets, ","); got != "192.168.1.0/24" {
		t.Errorf("targets %q: the Docker bridge must not be proposed", got)
	}
	if info, _ := os.Stat(o.ConfigPath); info.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v", info.Mode().Perm())
	}
	if unit := readFile(t, filepath.Join(o.UnitDir, "netscribe.service")); !strings.Contains(unit, "ExecStart="+o.BinPath+" serve --config "+o.ConfigPath) {
		t.Errorf("the unit does not point at this install:\n%s", unit)
	}
	if got := readFile(t, filepath.Join(o.SysctlDir, sysctlName)); !strings.Contains(got, "ping_group_range") {
		t.Errorf("sysctl file: %q", got)
	}

	ran := f.ran()
	for _, want := range []string{
		"useradd --system --user-group --home-dir " + o.StateDir,
		"systemctl daemon-reload",
		"systemctl enable --now netscribe",
		"sysctl -w net.ipv4.ping_group_range=",
		"user add alice --role admin --config " + o.ConfigPath + " --password-stdin",
	} {
		if !strings.Contains(ran, want) {
			t.Errorf("command %q was not run:\n%s", want, ran)
		}
	}
	for _, c := range f.cmds {
		if len(c.Args) > 1 && c.Args[0] == "user" && c.Args[1] == "add" && (c.As == nil || c.As.Name != o.ServiceUser) {
			t.Error("the account must be created by the service account, or the database is owned by root")
		}
	}
	out := f.out.String()
	for _, want := range []string{"Open the panel", "http://127.0.0.1:8080", "netscribe doctor", "0 failures"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
}

func TestSetupAnswersQuestionsOnATerminal(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	input := strings.Join([]string{
		"y",     // copy the binary
		"y",     // create the service account
		"y",     // create the directories
		"n",     // nmap
		"n",     // chromium
		"",      // networks: the marked ones
		"",      // listen address
		"30m",   // interval
		"y",     // write the config
		"",      // allow ICMP
		"alice", // administrator
		"y",     // write the unit
		"",      // start the panel
	}, "\n") + "\n"
	if err := Setup(context.Background(), f.env(input), o, "netscribe test"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	cfg, err := config.Load(o.ConfigPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Scan.Interval.Std().Minutes() != 30 {
		t.Errorf("interval %v", cfg.Scan.Interval.Std())
	}
	var add Command
	for _, c := range f.cmds {
		if len(c.Args) > 1 && c.Args[0] == "user" && c.Args[1] == "add" {
			add = c
		}
	}
	if !add.Interactive || strings.Contains(strings.Join(add.Args, " "), "--password-stdin") {
		t.Errorf("on a terminal the password is typed into user add itself: %+v", add)
	}
}

func TestSetupDryRunChangesNothing(t *testing.T) {
	f := newFake(t)
	f.uid = 1000
	o := yesOptions(f)
	o.DryRun = true
	if err := Setup(context.Background(), f.env(""), o, "netscribe test"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if len(f.copied) != 0 || exists(o.ConfigPath) || exists(o.StateDir) || exists(o.UnitDir) || exists(o.SysctlDir) {
		t.Error("a dry run wrote something")
	}
	for _, c := range f.cmds {
		if c.Name != "systemctl" || c.Args[0] != "is-active" {
			t.Errorf("a dry run ran %s %v", c.Name, c.Args)
		}
	}
	out := f.out.String()
	if strings.Contains(out, "gid 0") || !strings.Contains(out, "once the account exists") {
		t.Errorf("a dry run does not know the group id of an account that does not exist yet:\n%s", out)
	}
	for _, want := range []string{"would", "192.168.1.0/24", "listen: 127.0.0.1:8080", "nothing was changed"} {
		if !strings.Contains(out, want) {
			t.Errorf("the plan lacks %q:\n%s", want, out)
		}
	}
}

func TestSetupRefusesWithoutRootOrTerminal(t *testing.T) {
	f := newFake(t)
	f.uid = 1000
	if err := Setup(context.Background(), f.env(""), yesOptions(f), "v"); err == nil || !strings.Contains(err.Error(), "sudo") {
		t.Errorf("without root: %v", err)
	}

	f = newFake(t)
	env := f.env("")
	env.Interactive = false
	o := f.opts()
	if err := Setup(context.Background(), env, o, "v"); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Errorf("without a terminal and without --yes: %v", err)
	}
}

func TestSetupKeepsAValidConfigAndStopsOnAnInvalidOne(t *testing.T) {
	f := newFake(t)
	o := yesOptions(f)
	f.addUser(o.ServiceUser)
	writeConfig(t, o, Answers{Listen: "127.0.0.1:9191", Database: filepath.Join(o.StateDir, "db"), Targets: []string{"10.1.1.0/24"}, Interval: "1h", DockerSocket: "/var/run/docker.sock"})
	before := readFile(t, o.ConfigPath)
	if err := Setup(context.Background(), f.env("pw\n"), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if readFile(t, o.ConfigPath) != before {
		t.Error("the setup overwrote the config")
	}
	if !strings.Contains(f.out.String(), "http://127.0.0.1:9191") {
		t.Errorf("the summary must name the configured address:\n%s", f.out.String())
	}

	f = newFake(t)
	o = yesOptions(f)
	f.addUser(o.ServiceUser)
	if err := os.MkdirAll(filepath.Dir(o.ConfigPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.ConfigPath, []byte("listen: nonsense\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Setup(context.Background(), f.env(""), o, "v")
	if err == nil || !strings.Contains(err.Error(), "never overwrites") {
		t.Errorf("an invalid config must stop the setup: %v", err)
	}
	if readFile(t, o.ConfigPath) != "listen: nonsense\n" {
		t.Error("the invalid config was changed")
	}
}

func TestSetupRunsTwiceWithoutChangingTheSecondTime(t *testing.T) {
	f := newFake(t)
	o := yesOptions(f)
	f.ping = "0\t2147483647\n"
	if err := Setup(context.Background(), f.env("pw\n"), o, "v"); err != nil {
		t.Fatalf("first run: %v\n%s", err, f.out.String())
	}
	f.exe = o.BinPath
	f.cmds, f.copied = nil, nil
	f.out.Reset()
	if err := Setup(context.Background(), f.env("pw\n"), o, "v"); err != nil {
		t.Fatalf("second run: %v\n%s", err, f.out.String())
	}
	if strings.Contains(f.ran(), "useradd") || strings.Contains(f.ran(), "user add") || strings.Contains(f.ran(), "daemon-reload") || len(f.copied) != 0 {
		t.Errorf("the second run changed things:\n%s", f.ran())
	}
	if !strings.Contains(f.out.String(), "is up to date") {
		t.Errorf("the second run should say the unit is current:\n%s", f.out.String())
	}
}

func TestSetupReportsAPanelThatDoesNotAnswer(t *testing.T) {
	f := newFake(t)
	o := yesOptions(f)
	f.health = os.ErrDeadlineExceeded
	err := Setup(context.Background(), f.env("pw\n"), o, "v")
	if err == nil || !strings.Contains(err.Error(), "journalctl -u netscribe") {
		t.Errorf("a silent panel must lead to the log: %v", err)
	}
}

func TestSetupRefusesAnUnusableDefaultWhenItCannotAsk(t *testing.T) {
	f := newFake(t)
	o := yesOptions(f)
	f.listen = os.ErrExist
	if err := Setup(context.Background(), f.env(""), o, "v"); err == nil || !strings.Contains(err.Error(), "not usable") {
		t.Errorf("a busy port with --yes: %v", err)
	}

	f = newFake(t)
	o = yesOptions(f)
	o.Listen = "0.0.0.0:8080"
	if err := Setup(context.Background(), f.env(""), o, "v"); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Errorf("an open address without TLS must not be taken silently: %v", err)
	}
}

func TestSetupChecksTheTargetsOfTheFlag(t *testing.T) {
	f := newFake(t)
	o := yesOptions(f)
	o.Targets = []string{"8.8.8.0/24"}
	if err := Setup(context.Background(), f.env(""), o, "v"); err == nil {
		t.Error("a public target must be refused")
	}

	f = newFake(t)
	f.ifaces = nil
	o = yesOptions(f)
	if err := Setup(context.Background(), f.env(""), o, "v"); err == nil || !strings.Contains(err.Error(), "--targets") {
		t.Errorf("no network found and no flag: %v", err)
	}

	f = newFake(t)
	f.ifaces = nil
	o = yesOptions(f)
	o.Targets = []string{"10.7.0.0/24"}
	if err := Setup(context.Background(), f.env("pw\n"), o, "v"); err != nil {
		t.Fatalf("a target from the flag: %v", err)
	}
	if cfg, _ := config.Load(o.ConfigPath, false); len(cfg.Scan.Targets) != 1 || cfg.Scan.Targets[0] != "10.7.0.0/24" {
		t.Errorf("targets %v", cfg.Scan.Targets)
	}
}

func TestSetupWritesTheDropInForDockerAndNmap(t *testing.T) {
	f := newFake(t)
	o := yesOptions(f)
	f.sockets["/var/run/docker.sock"] = SocketInfo{Mode: 0o660, GID: 4242, Group: "docker"}
	if err := Setup(context.Background(), f.env("pw\n"), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	drop := readFile(t, filepath.Join(o.UnitDir, "netscribe.service.d", dropInName))
	if !strings.Contains(drop, "SupplementaryGroups=docker") || strings.Contains(drop, "CAP_NET_RAW") {
		t.Errorf("drop-in:\n%s", drop)
	}
	if cfg, _ := config.Load(o.ConfigPath, false); !cfg.Docker.Enabled {
		t.Error("Docker was found and is the default answer")
	}
}

func TestSetupOffersNmapOnATerminal(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	input := strings.Join([]string{"y", "y", "y", "y", "n", "", "", "", "y", "", "alice", "y", "y", ""}, "\n") + "\n"
	if err := Setup(context.Background(), f.env(input), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.ran(), "apt-get install -y -qq nmap") {
		t.Errorf("nmap was not installed:\n%s", f.ran())
	}
	if cfg, _ := config.Load(o.ConfigPath, false); !cfg.Discovery.Nmap {
		t.Error("discovery.nmap must be on")
	}
	if drop := readFile(t, filepath.Join(o.UnitDir, "netscribe.service.d", dropInName)); !strings.Contains(drop, "CAP_NET_RAW") {
		t.Errorf("the unit must grant raw sockets for nmap:\n%s", drop)
	}
}

func TestSetupLeavesAForeignPingRangeToThePerson(t *testing.T) {
	f := newFake(t)
	o := yesOptions(f)
	f.ping = "20000\t30000\n"
	if err := Setup(context.Background(), f.env("pw\n"), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if exists(filepath.Join(o.SysctlDir, sysctlName)) || strings.Contains(f.ran(), "sysctl") {
		t.Error("a range that other groups use must not be replaced")
	}
}

func TestSetupWithoutSystemdWritesNoUnit(t *testing.T) {
	f := newFake(t)
	f.systemd = false
	o := yesOptions(f)
	if err := Setup(context.Background(), f.env("pw\n"), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if exists(o.UnitDir) || !strings.Contains(f.out.String(), "serve --config") {
		t.Errorf("no systemd: no unit, but the command to start by hand:\n%s", f.out.String())
	}
}

func TestInstallPackagesTriesTheNextName(t *testing.T) {
	f := newFake(t)
	env := f.env("")
	env.Run = func(_ context.Context, c Command) ([]byte, error) {
		f.cmds = append(f.cmds, c)
		if len(c.Args) > 0 && c.Args[len(c.Args)-1] == "chromium" {
			return []byte("E: Unable to locate package chromium"), os.ErrNotExist
		}
		return nil, nil
	}
	s := newSession(env, f.opts())
	if err := s.installPackages(context.Background(), "chromium", "chromium-browser"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.ran(), "update -qq") || !strings.Contains(f.ran(), "chromium-browser") {
		t.Errorf("commands:\n%s", f.ran())
	}

	f.bins = map[string]bool{}
	if err := s.installPackages(context.Background(), "nmap"); err == nil {
		t.Error("no package manager must be an error")
	}
}
