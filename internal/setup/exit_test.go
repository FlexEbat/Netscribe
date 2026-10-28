package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/config"
)

func TestQuitWordLeavesAtAnyQuestion(t *testing.T) {
	for _, word := range []string{"q", "Q", "quit", "exit"} {
		f := newFake(t)
		s := newSession(f.env(word+"\n"), f.opts())
		if _, err := s.p.confirm("Go on?", true); !errors.Is(err, errQuit) {
			t.Errorf("confirm and %q: %v", word, err)
		}
		s = newSession(f.env(word+"\n"), f.opts())
		if _, err := s.p.ask("Name", "x", nil); !errors.Is(err, errQuit) {
			t.Errorf("ask and %q: %v", word, err)
		}
		s = newSession(f.env(word+"\n"), f.opts())
		if _, err := s.p.pick("Which?", []string{"a", "Back"}, 0); !errors.Is(err, errQuit) {
			t.Errorf("pick and %q: %v", word, err)
		}
		s = newSession(f.env(word+"\n"), f.opts())
		if _, err := s.p.choose("Which?", []string{"a", "b"}, []bool{true, true}); !errors.Is(err, errQuit) {
			t.Errorf("choose and %q: %v", word, err)
		}
	}
}

func TestBackWordPicksTheBackItemOnly(t *testing.T) {
	f := newFake(t)
	s := newSession(f.env("b\n"), f.opts())
	i, err := s.p.pick("Which?", []string{"a", "b", "Back"}, 0)
	if err != nil || i != 2 {
		t.Errorf("b on a menu with Back: %d, %v", i, err)
	}
	s = newSession(f.env("b\nq\n"), f.opts())
	if _, err := s.p.pick("Which?", []string{"a", "Quit"}, 0); !errors.Is(err, errQuit) {
		t.Errorf("a menu without Back does not take b: %v", err)
	}
}

func TestQuitStopsTheSetupAndSaysWhatStaysDone(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	err := Setup(context.Background(), f.env("y\nq\n"), o, "v") // copy the binary, then quit at the account
	if !errors.Is(err, ErrStopped) || !errors.Is(err, errQuit) {
		t.Fatalf("the setup must stop: %v", err)
	}
	if !strings.Contains(f.out.String(), "Setup stopped at step 3") {
		t.Errorf("the stop is not explained:\n%s", f.out.String())
	}
	if exists(o.ConfigPath) || len(f.users) != 0 {
		t.Error("nothing after the quit may be done")
	}
}

func TestDecliningAnActionStopsTheSetupBeforeIt(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	err := Setup(context.Background(), f.env("y\nn\n"), o, "v") // copy the binary, refuse the account
	if !errors.Is(err, ErrStopped) || !errors.Is(err, errDeclined) {
		t.Fatalf("a refusal stops the setup: %v", err)
	}
	if len(f.users) != 0 || strings.Contains(f.ran(), "useradd") {
		t.Errorf("the account was created after a no:\n%s", f.ran())
	}
}

func TestEveryChangeOfTheSetupAsksFirst(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	input := strings.Join([]string{"y", "y", "y", "n", "n", "", "", "", "y", "", "alice", "y", "", "n"}, "\n") + "\n"
	if err := Setup(context.Background(), f.env(input), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	out := f.out.String()
	for _, want := range []string{"Create the system account netscribe?", "Write " + o.ConfigPath + " with mode 0600?", "Write " + filepath.Join(o.UnitDir, "netscribe.service") + "?"} {
		if !strings.Contains(out, want) {
			t.Errorf("no question %q:\n%s", want, out)
		}
	}
}

func TestConsoleQuitsOnQ(t *testing.T) {
	f, o := installed(t)
	if err := Console(context.Background(), f.env("q\n"), o, "v"); err != nil {
		t.Errorf("q is not an error: %v", err)
	}
}

func TestConsoleBackWordLeavesASubmenuWithoutAnAction(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	for _, entry := range []string{"7", "8", "6", "3", "5"} { // accounts, service, settings, scan, export
		if err := Console(context.Background(), f.env(menu(entry, "b", "12")), o, "v"); err != nil {
			t.Fatalf("menu %s: %v\n%s", entry, err, f.out.String())
		}
	}
	for _, c := range f.cmds {
		if c.Name == "systemctl" && c.Args[0] != "is-active" && c.Args[0] != "show" {
			t.Errorf("a submenu left with b ran %s %v", c.Name, c.Args)
		}
	}
}

func TestConsoleQuitsFromInsideASubmenu(t *testing.T) {
	f, o := installed(t)
	if err := Console(context.Background(), f.env(menu("8", "q")), o, "v"); err != nil {
		t.Errorf("q inside the service menu: %v", err)
	}
	if strings.Contains(f.out.String(), "What do you want to do?\n") && strings.Count(f.out.String(), "What do you want to do?") > 1 {
		t.Error("q must leave the whole console, not return to the menu")
	}
}

func TestConsoleServiceActionsAskAndANoChangesNothing(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	// stop (3): no; restart (4): no; disable at boot (7): no; leave
	if err := Console(context.Background(), f.env(menu("8", "3", "n", "4", "n", "7", "n", "9", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"systemctl stop", "systemctl restart", "systemctl disable"} {
		if strings.Contains(f.ran(), bad) {
			t.Errorf("%q ran after a no:\n%s", bad, f.ran())
		}
	}
	if !strings.Contains(f.out.String(), "Stop the service?") {
		t.Errorf("the stop was not confirmed:\n%s", f.out.String())
	}
}

func TestConsoleAccountsAskBeforeTheyChangeAnything(t *testing.T) {
	f, o := installed(t)
	if err := writeBin(o); err != nil {
		t.Fatal(err)
	}
	// add bob as viewer: no; disable bob: no
	if err := Console(context.Background(), f.env(menu("7", "2", "bob", "1", "n", "4", "bob", "n", "5", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.ran(), "user add") || strings.Contains(f.ran(), "user disable") {
		t.Errorf("an account changed after a no:\n%s", f.ran())
	}
}

func TestConsoleRunsAScanAfterConfirmation(t *testing.T) {
	f, o := installed(t)
	if err := Console(context.Background(), f.env(menu("3", "1", "y", "4", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	var scan Command
	for _, c := range f.cmds {
		if len(c.Args) > 0 && c.Args[0] == "scan" {
			scan = c
		}
	}
	if scan.Name != o.BinPath || !scan.Interactive || scan.As == nil || scan.As.Name != o.ServiceUser {
		t.Errorf("the scan must run as the service account and show its progress: %+v", scan)
	}
	if !strings.Contains(strings.Join(scan.Args, " "), "--config "+o.ConfigPath) {
		t.Errorf("the scan lacks the config: %v", scan.Args)
	}
}

func TestConsoleScanWithNmapAndOtherNetworks(t *testing.T) {
	f, o := installed(t)
	if err := Console(context.Background(), f.env(menu("3", "2", "y", "3", "10.1.0.0/24, 10.2.0.0/24", "y", "4", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	ran := f.ran()
	for _, want := range []string{"scan --nmap --config", "scan 10.1.0.0/24 10.2.0.0/24 --config"} {
		if !strings.Contains(ran, want) {
			t.Errorf("%q was not run:\n%s", want, ran)
		}
	}
}

func TestConsoleScanRefusesAPublicNetworkAndAsksAgain(t *testing.T) {
	f, o := installed(t)
	if err := Console(context.Background(), f.env(menu("3", "3", "8.8.8.0/24", "10.1.0.0/24", "n", "4", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "private") {
		t.Errorf("a public network was not refused:\n%s", f.out.String())
	}
	if strings.Contains(f.ran(), "scan ") {
		t.Errorf("a scan ran after a no:\n%s", f.ran())
	}
}

func TestConsoleScanNeedsRoot(t *testing.T) {
	f, o := installed(t)
	f.uid = 1000
	if err := Console(context.Background(), f.env(menu("3", "1", "4", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.ran(), "scan ") || !strings.Contains(f.out.String(), "run netscribe with sudo") {
		t.Errorf("a scan without root:\n%s\n%s", f.ran(), f.out.String())
	}
}

func TestConsoleDevicesListsWithoutAsking(t *testing.T) {
	f, o := installed(t)
	if err := Console(context.Background(), f.env(menu("4", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.ran(), "devices --config") {
		t.Errorf("devices were not listed:\n%s", f.ran())
	}
}

func TestConsoleExportWritesTheFileForThePerson(t *testing.T) {
	f, o := installed(t)
	f.vars["SUDO_UID"], f.vars["SUDO_GID"] = "1000", "1000"
	dest := filepath.Join(t.TempDir(), "network.md")
	env := f.env(menu("5", "1", dest, "y", "5", "11"))
	var owner *Account
	write := env.WriteFile
	env.WriteFile = func(p string, d []byte, m os.FileMode, a *Account) error {
		if p == dest {
			owner = a
		}
		return write(p, d, m, a)
	}
	env.Run = func(ctx context.Context, c Command) ([]byte, error) {
		if len(c.Args) > 0 && c.Args[0] == "export" {
			for i, a := range c.Args {
				if a == "--out" {
					if err := os.WriteFile(c.Args[i+1], []byte("# the network\n"), 0o600); err != nil {
						return nil, err
					}
				}
			}
		}
		return f.run(ctx, c)
	}
	if err := Console(context.Background(), env, o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if got := readFile(t, dest); got != "# the network\n" {
		t.Errorf("the file holds %q", got)
	}
	if owner == nil || owner.UID != 1000 {
		t.Errorf("the file must belong to the person behind sudo: %+v", owner)
	}
	if !strings.Contains(f.ran(), "export --format md --out") {
		t.Errorf("export did not run:\n%s", f.ran())
	}
}

func TestConsoleExportAsksBeforeItReplacesAFile(t *testing.T) {
	f, o := installed(t)
	dest := filepath.Join(t.TempDir(), "network.md")
	if err := os.WriteFile(dest, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Console(context.Background(), f.env(menu("5", "1", dest, "n", "5", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if readFile(t, dest) != "keep me" || strings.Contains(f.ran(), "export ") {
		t.Errorf("the file was replaced after a no:\n%s", f.ran())
	}
}

func TestConfigureAsksBeforeItSavesAndANoChangesNothing(t *testing.T) {
	f, o := installed(t)
	before := readFile(t, o.ConfigPath)
	if err := Configure(context.Background(), f.env(menu("2", "2", "45m", "n", "6", "15")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if readFile(t, o.ConfigPath) != before || exists(o.ConfigPath+".bak") {
		t.Error("the config changed after a no")
	}
	if !strings.Contains(f.out.String(), "Save Scan every:") {
		t.Errorf("the save was not confirmed:\n%s", f.out.String())
	}
}

func TestConfigureRestoresThePreviousConfig(t *testing.T) {
	f, o := installed(t)
	if err := ConfigSet(context.Background(), f.env(""), o, "scan.interval", []string{"45m"}); err != nil {
		t.Fatal(err)
	}
	if err := Configure(context.Background(), f.env(menu("14", "y", "15")), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if cfg, _ := config.Load(o.ConfigPath, false); cfg.Scan.Interval.Std().Minutes() != 15 {
		t.Errorf("interval %v: the previous config was not restored", cfg.Scan.Interval.Std())
	}
	if !strings.Contains(readFile(t, o.ConfigPath+".bak"), "45m") {
		t.Error("the replaced config must stay as the .bak, so the swap can be undone")
	}
	// a no leaves it alone, and no backup is no error
	f2, o2 := installed(t)
	if err := Configure(context.Background(), f2.env(menu("14", "15")), o2, "v"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f2.out.String(), "no previous config") {
		t.Errorf("a missing backup was not explained:\n%s", f2.out.String())
	}
}

func TestConfigureAddsAndRemovesSSHHosts(t *testing.T) {
	f, o := installed(t)
	add := menu("11", "1", "192.168.1.10", "audit", "/home/a/key", "y", "3", "15")
	if err := Configure(context.Background(), f.env(add), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	cfg, err := config.Load(o.ConfigPath, false)
	if err != nil || len(cfg.SSH.Hosts) != 1 || cfg.SSH.Hosts[0].Address != "192.168.1.10" || cfg.SSH.Hosts[0].User != "audit" {
		t.Fatalf("hosts %+v, %v", cfg.SSH.Hosts, err)
	}
	remove := menu("11", "2", "1", "n", "2", "1", "y", "3", "15") // a no first, then a yes
	if err := Configure(context.Background(), f.env(remove), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if cfg, _ := config.Load(o.ConfigPath, false); len(cfg.SSH.Hosts) != 0 {
		t.Errorf("the host is still there: %+v", cfg.SSH.Hosts)
	}
}

func TestConfigureRefusesABadSSHAddress(t *testing.T) {
	f, o := installed(t)
	if err := Configure(context.Background(), f.env(menu("11", "1", "not-an-ip", "192.168.1.11:2222", "audit", "/k", "y", "3", "15")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "use an IP address") {
		t.Errorf("the bad address was accepted:\n%s", f.out.String())
	}
	if cfg, _ := config.Load(o.ConfigPath, false); len(cfg.SSH.Hosts) != 1 || cfg.SSH.Hosts[0].Address != "192.168.1.11:2222" {
		t.Errorf("hosts %+v", cfg.SSH.Hosts)
	}
}

func TestConsoleCommandsListsEveryCommand(t *testing.T) {
	f, o := installed(t)
	if err := Console(context.Background(), f.env(menu("11", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"serve", "scan", "devices", "export", "user", "setup", "config", "watch", "doctor", "update", "version"} {
		if !strings.Contains(f.out.String(), "netscribe "+want) {
			t.Errorf("the command list lacks %q", want)
		}
	}
}
