package setup

import (
	"context"
	"strings"
	"testing"
)

func TestConsoleQuitsAndShowsTheState(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	if err := Console(context.Background(), f.env(menu("12")), o, "vtest"); err != nil {
		t.Fatal(err)
	}
	out := f.out.String()
	for _, want := range []string{"Netscribe", "service active", "panel answers at http://127.0.0.1:8080", "Watch", "Check", "Scan", "Devices", "Export", "Settings", "Accounts", "Service", "Setup", "Commands", "Quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("the console lacks %q:\n%s", want, out)
		}
	}
}

func TestConsoleLeavesQuietlyWhenTheTerminalCloses(t *testing.T) {
	f, o := installed(t)
	if err := Console(context.Background(), f.env(""), o, "v"); err != nil {
		t.Errorf("closing the terminal is not an error: %v", err)
	}
}

func TestConsoleWithoutAConfigPointsToSetup(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	if err := Console(context.Background(), f.env(menu("12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "run Setup") {
		t.Errorf("no config: %s", f.out.String())
	}
}

func TestConsoleWatchAndCheck(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	o.WatchCount = 1
	if err := Console(context.Background(), f.env(menu("1", "2", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	out := f.out.String()
	if !strings.Contains(out, "Netscribe watch") || !strings.Contains(out, "Netscribe doctor") {
		t.Errorf("the watcher and the checks must run from the menu:\n%s", out)
	}
}

func TestConsoleServiceActions(t *testing.T) {
	f, o := installed(t)
	f.active = "active"
	// Service: restart (4), reload (5), start at boot (6), each confirmed, then status (1), log (8), back (9), quit
	if err := Console(context.Background(), f.env(menu("8", "4", "y", "5", "y", "6", "y", "1", "8", "9", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	ran := f.ran()
	for _, want := range []string{"systemctl restart netscribe", "systemctl kill --signal=HUP netscribe", "systemctl enable netscribe", "systemctl status netscribe", "journalctl -u netscribe -n 80"} {
		if !strings.Contains(ran, want) {
			t.Errorf("%q was not run:\n%s", want, ran)
		}
	}
}

func TestConsoleServiceNeedsRootToChangeThings(t *testing.T) {
	f, o := installed(t)
	f.uid = 1000
	if err := Console(context.Background(), f.env(menu("8", "3", "9", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.ran(), "systemctl stop") || !strings.Contains(f.out.String(), "run netscribe with sudo") {
		t.Errorf("a stop without root:\n%s\n%s", f.ran(), f.out.String())
	}
}

func TestConsoleAccounts(t *testing.T) {
	f, o := installed(t)
	f.userList = "USERNAME  ROLE  STATUS  LAST LOGIN\nalice  admin  enabled  never\n"
	if err := writeBin(o); err != nil {
		t.Fatal(err)
	}
	// Accounts: list (1), add (2) bob as admin (3), reset (3) bob, disable (4) bob, each confirmed, back (5), quit
	input := menu("7", "1", "2", "bob", "3", "y", "3", "bob", "y", "4", "bob", "y", "5", "12")
	if err := Console(context.Background(), f.env(input), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	if !strings.Contains(f.out.String(), "alice  admin") {
		t.Errorf("the list was not printed:\n%s", f.out.String())
	}
	ran := f.ran()
	for _, want := range []string{"user add bob --role admin --config", "user passwd bob --config", "user disable bob --config"} {
		if !strings.Contains(ran, want) {
			t.Errorf("%q was not run:\n%s", want, ran)
		}
	}
	for _, c := range f.cmds {
		if len(c.Args) > 1 && c.Args[0] == "user" && c.Args[1] != "list" && !c.Interactive {
			t.Errorf("%v must be interactive: the password is typed into it", c.Args)
		}
		if len(c.Args) > 1 && c.Args[0] == "user" && (c.As == nil || c.As.Name != o.ServiceUser) {
			t.Errorf("%v must run as the service account", c.Args)
		}
	}
}

func TestConsoleAccountsNeedTheServiceAccount(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	if err := Console(context.Background(), f.env(menu("7", "12")), o, "v"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "run Setup first") {
		t.Errorf("%s", f.out.String())
	}
}

func TestSetupOffersTheConsoleAtTheEnd(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	input := menu("y", "y", "y", "n", "n", "", "", "", "y", "", "alice", "y", "", "y", "12")
	if err := Setup(context.Background(), f.env(input), o, "v"); err != nil {
		t.Fatalf("%v\n%s", err, f.out.String())
	}
	out := f.out.String()
	if !strings.Contains(out, "opens the console") || !strings.Contains(out, "What do you want to do?") {
		t.Errorf("the console was not offered or not opened:\n%s", out)
	}

	f = newFake(t)
	o = f.opts()
	input = menu("y", "y", "y", "n", "n", "", "", "", "y", "", "alice", "y", "", "n")
	if err := Setup(context.Background(), f.env(input), o, "v"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.out.String(), "What do you want to do?") {
		t.Error("the console opened after a no")
	}
}
