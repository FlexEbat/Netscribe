package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/config"
)

// Console is the menu that "netscribe" opens on a terminal: the watcher, the checks, the
// scans, the settings, the accounts and the service in one place. Every item has a way
// out (b goes back, q quits), and every action that changes something asks first.
func Console(ctx context.Context, env Env, o Options, version string) error {
	s := newSession(env, o)
	s.version = version
	for {
		s.ui.banner("Netscribe", version)
		s.header(ctx)
		i, err := s.p.pick("What do you want to do?", []string{
			"Watch       live view of the service, the panel and the host",
			"Check       run every check and say what to fix",
			"Scan        scan the networks now",
			"Devices     the devices found so far",
			"Export      write the network documentation: Markdown, HTML, PDF, draw.io",
			"Settings    change the configuration",
			"Accounts    list, add, reset a password, disable",
			"Service     start, stop, restart, log",
			"Setup       run the guided setup again",
			"Commands    every netscribe command, for scripts and cron",
			"Quit",
		}, 0)
		if err != nil {
			if isExit(err) {
				return nil // q or a closed terminal: leave quietly
			}
			return err
		}
		switch i {
		case 0:
			err = s.watchFromMenu(ctx)
		case 1:
			err = Doctor(ctx, env, o, version)
			if err != nil {
				s.ui.printf("\n")
				err = nil // the report said what failed
			}
		case 2:
			err = s.scanMenu(ctx)
		case 3:
			err = s.devicesMenu(ctx)
		case 4:
			err = s.exportMenu(ctx)
		case 5:
			err = Configure(ctx, env, o, version)
		case 6:
			err = s.accounts(ctx)
		case 7:
			err = s.serviceMenu(ctx)
		case 8:
			err = s.setupFromMenu(ctx)
		case 9:
			s.ui.printf("%s\n", commandsHelp)
		default:
			return nil
		}
		if isExit(err) {
			return nil
		}
		if errors.Is(err, errDeclined) {
			err = nil // the refusal was shown where it was given
		}
		if err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
		}
	}
}

const commandsHelp = `
  netscribe                          this menu
  netscribe serve                    run the web panel
  netscribe scan [--nmap] [CIDR...]  scan the networks of the config, or the CIDRs given
  netscribe devices                  print the devices found so far
  netscribe export --format md|html|pdf|drawio --out FILE
  netscribe user add|list|disable|passwd NAME
  netscribe setup [--yes --dry-run --no-service ...]   guided install
  netscribe config [show|get KEY|set KEY VALUE|edit]   the configuration
  netscribe watch [--once]           live view, or one sample for monitors
  netscribe doctor                   check the install and say what to fix
  netscribe version
  Add --config FILE to the commands that read the config.`

// header prints one line about the service, so the menu starts with the answer to "is it up".
func (s *session) header(ctx context.Context) {
	state, _ := s.env.Run(ctx, Command{Name: "systemctl", Args: []string{"is-active", "netscribe"}})
	active := strings.TrimSpace(string(state))
	if active == "" {
		active = "unknown"
	}
	line := s.ui.dot(active == "active", active == "activating") + " service " + active
	if cfg, err := s.loadConfig(); err == nil {
		url := healthURL(*cfg)
		if active == "active" && url != "" {
			if err := s.env.Health(ctx, url); err == nil {
				line += "   panel answers at " + strings.TrimSuffix(url, "/healthz")
			} else {
				line += "   " + s.ui.paint("31", "panel does not answer")
			}
		} else {
			line += "   " + s.ui.dim(strings.TrimSuffix(url, "/healthz"))
		}
	} else {
		line += "   " + s.ui.paint("33", "no valid config: run Setup")
	}
	s.ui.printf("%s\n\n", line)
}

func (s *session) watchFromMenu(ctx context.Context) error {
	wctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	err := Watch(wctx, s.env, s.o, s.version)
	s.ui.printf("\n")
	return err
}

func (s *session) needRoot() error {
	if s.env.Getuid() != 0 {
		return errors.New("this changes the system: run netscribe with sudo")
	}
	return nil
}

// showCommand runs a command and prints what it printed, even when it exits with an error,
// because systemctl status exits with 3 for a stopped service and still says why.
func (s *session) showCommand(ctx context.Context, c Command) {
	out, err := s.env.Run(ctx, c)
	text := strings.TrimRight(string(out), "\n")
	if text != "" {
		s.ui.printf("%s\n", text)
	}
	if err != nil && text == "" {
		s.ui.printf("  %s %s: %v\n", s.ui.paint("31", "✗"), c.Name, err)
	}
}

// netscribeAs builds a call of this program as the service account, which owns the database.
func (s *session) netscribeAs(acc Account, args ...string) Command {
	return Command{Name: s.o.BinPath, Args: append(append([]string{}, args...), "--config", s.o.ConfigPath), As: &acc}
}

// serviceAccount finds the account of the service, or says what to do.
func (s *session) serviceAccount() (Account, error) {
	acc, err := s.env.LookupUser(s.o.ServiceUser)
	if err != nil {
		return Account{}, errors.New("the service account does not exist yet: run Setup first")
	}
	return acc, nil
}

// approve confirms one action. The answer no returns errDeclined, which the menus swallow.
func (s *session) approve(question string, def bool) error {
	ok, err := s.p.confirm(question, def)
	if err != nil {
		return err
	}
	if !ok {
		s.ui.note("cancelled: nothing was changed")
		return errDeclined
	}
	return nil
}

func (s *session) setupFromMenu(ctx context.Context) error {
	if err := s.approve("Run the guided setup again? It checks the machine, and every change it makes asks first.", false); err != nil {
		return err
	}
	return Setup(ctx, s.env, s.o, s.version)
}

// configTargets lists the networks of the config for a question, or says there are none.
func (s *session) configTargets() string {
	cfg, err := s.loadConfig()
	if err != nil || len(cfg.Scan.Targets) == 0 {
		return "the networks of the config"
	}
	return strings.Join(cfg.Scan.Targets, ", ")
}

func (s *session) scanMenu(ctx context.Context) error {
	acc, err := s.serviceAccount()
	if err != nil {
		return err
	}
	for {
		i, err := s.p.pick("Scan", []string{
			"The networks of the config",
			"The networks of the config, and ask Nmap about devices nothing else could type",
			"Other networks, which you name",
			"Back",
		}, 0)
		if err != nil || i == 3 {
			return err
		}
		if err := s.needRoot(); err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
			continue
		}
		args := []string{"scan"}
		what := s.configTargets()
		switch i {
		case 1:
			args = append(args, "--nmap")
		case 2:
			text, err := s.p.ask("Networks to scan, CIDRs separated by commas", "", func(v string) error { return validateTargets(splitList(v)) })
			if err != nil {
				return err
			}
			list := splitList(text)
			args = append(args, list...)
			what = strings.Join(list, ", ")
		}
		if err := s.approve("Scan "+what+" now? Hosts of these networks are probed with ARP, ICMP and TCP.", true); err != nil {
			if errors.Is(err, errDeclined) {
				continue
			}
			return err
		}
		c := s.netscribeAs(acc, args...)
		c.Interactive = true
		sctx, stop := signal.NotifyContext(ctx, os.Interrupt)
		_, runErr := s.env.Run(sctx, c) // Ctrl-C ends the scan, not the console
		stop()
		if runErr != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), runErr)
		}
	}
}

func (s *session) devicesMenu(ctx context.Context) error {
	acc, err := s.serviceAccount()
	if err != nil {
		return err
	}
	if err := s.needRoot(); err != nil {
		return err
	}
	s.showCommand(ctx, s.netscribeAs(acc, "devices"))
	return nil
}

// invokingUser is the person behind sudo, so that a file the console writes is theirs.
func (s *session) invokingUser() *Account {
	uid, err1 := strconv.ParseUint(s.env.Getenv("SUDO_UID"), 10, 32)
	gid, err2 := strconv.ParseUint(s.env.Getenv("SUDO_GID"), 10, 32)
	if err1 != nil || err2 != nil {
		return nil
	}
	return &Account{UID: uint32(uid), GID: uint32(gid)} //nolint:gosec // ParseUint with 32 bits cannot overflow
}

var exportFormats = []struct{ format, ext, label string }{
	{"md", ".md", "Markdown"},
	{"html", ".html", "HTML, one page"},
	{"pdf", ".pdf", "PDF, needs Chromium"},
	{"drawio", ".drawio", "draw.io diagram"},
}

func (s *session) exportMenu(ctx context.Context) error {
	acc, err := s.serviceAccount()
	if err != nil {
		return err
	}
	for {
		labels := make([]string, 0, len(exportFormats)+1)
		for _, f := range exportFormats {
			labels = append(labels, f.label)
		}
		labels = append(labels, "Back")
		i, err := s.p.pick("Export the network documentation as", labels, 0)
		if err != nil || i == len(exportFormats) {
			return err
		}
		if err := s.needRoot(); err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
			continue
		}
		f := exportFormats[i]
		dest, err := s.p.ask("Write the file to", "netscribe-network"+f.ext, func(v string) error {
			if strings.TrimSpace(v) == "" {
				return errors.New("name a file")
			}
			return nil
		})
		if err != nil {
			return err
		}
		if _, err := s.env.Stat(dest); err == nil {
			if err := s.approve(dest+" exists. Replace it?", false); err != nil {
				if errors.Is(err, errDeclined) {
					continue
				}
				return err
			}
		}
		if err := s.approve("Write the network documentation as "+f.label+" to "+dest+"?", true); err != nil {
			if errors.Is(err, errDeclined) {
				continue
			}
			return err
		}
		if err := s.export(ctx, acc, f.format, dest); err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
		}
	}
}

// export has the service account write the report into its own directory, then copies it
// to where the person asked, owned by the person.
func (s *session) export(ctx context.Context, acc Account, format, dest string) error {
	tmp := filepath.Join(s.o.StateDir, ".export-"+strconv.FormatInt(s.env.Now().UnixNano(), 36))
	s.ui.doing("export " + format)
	if err := s.runChecked(ctx, s.netscribeAs(acc, "export", "--format", format, "--out", tmp)); err != nil {
		return err
	}
	data, err := s.env.ReadFile(tmp)
	_ = s.env.Remove(tmp) // the copy below is the only one that stays
	if err != nil {
		return err
	}
	if err := s.env.WriteFile(dest, data, 0o600, s.invokingUser()); err != nil {
		return err
	}
	s.ui.done(fmt.Sprintf("wrote %s (%d bytes)", dest, len(data)))
	return nil
}

func (s *session) accounts(ctx context.Context) error {
	acc, err := s.serviceAccount()
	if err != nil {
		return err
	}
	user := func(args ...string) Command { return s.netscribeAs(acc, append([]string{"user"}, args...)...) }
	for {
		i, err := s.p.pick("Accounts", []string{"List", "Add", "Reset a password", "Disable", "Back"}, 0)
		if err != nil || i == 4 {
			return err
		}
		if err := s.needRoot(); err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
			continue
		}
		if i == 0 {
			s.showCommand(ctx, user("list"))
			continue
		}
		name, err := s.p.ask("Account name", "", func(v string) error {
			if strings.TrimSpace(v) == "" {
				return errors.New("name the account")
			}
			return nil
		})
		if err != nil {
			return err
		}
		var c Command
		var question string
		def := true
		switch i {
		case 1:
			roles := []string{"viewer", "operator", "admin"}
			r, err := s.p.pick("Role", []string{"viewer: reads the topology", "operator: edits devices and starts scans", "admin: everything, including accounts"}, 0)
			if err != nil {
				return err
			}
			c = user("add", name, "--role", roles[r])
			question = "Add the account " + name + " as " + roles[r] + "? You type its password next."
		case 2:
			c = user("passwd", name)
			question = "Reset the password of " + name + "? Its sessions are closed. You type the new password next."
		case 3:
			c = user("disable", name)
			question = "Disable " + name + "? The account cannot sign in and its sessions are closed."
			def = false
		}
		if err := s.approve(question, def); err != nil {
			if errors.Is(err, errDeclined) {
				continue
			}
			return err
		}
		c.Interactive = true
		if _, err := s.env.Run(ctx, c); err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
		}
	}
}

func (s *session) serviceMenu(ctx context.Context) error {
	systemctl := func(args ...string) Command { return Command{Name: "systemctl", Args: args} }
	type action struct {
		args     []string
		question string
		def      bool
	}
	actions := map[int]action{
		1: {[]string{"start", "netscribe"}, "Start the service?", true},
		2: {[]string{"stop", "netscribe"}, "Stop the service? The panel goes down and no scan runs.", false},
		3: {[]string{"restart", "netscribe"}, "Restart the service? The panel is down for a moment and a running scan is lost.", true},
		4: {[]string{"kill", "--signal=HUP", "netscribe"}, "Make the service read the config again?", true},
		5: {[]string{"enable", "netscribe"}, "Start the service at every boot?", true},
		6: {[]string{"disable", "netscribe"}, "Stop starting the service at boot? It keeps running until you stop it.", false},
	}
	for {
		i, err := s.p.pick("Service", []string{
			"Status", "Start", "Stop", "Restart", "Reload the config", "Start at boot: on", "Start at boot: off", "Log", "Back",
		}, 0)
		if err != nil || i == 8 {
			return err
		}
		switch i {
		case 0:
			s.showCommand(ctx, systemctl("status", "netscribe", "--no-pager", "-l"))
			continue
		case 7:
			s.showCommand(ctx, Command{Name: "journalctl", Args: []string{"-u", "netscribe", "-n", "80", "--no-pager", "-o", "short-iso"}})
			continue
		}
		if err := s.needRoot(); err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
			continue
		}
		a := actions[i]
		if err := s.approve(a.question, a.def); err != nil {
			if errors.Is(err, errDeclined) {
				continue
			}
			return err
		}
		if err := s.runChecked(ctx, systemctl(a.args...)); err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
			continue
		}
		s.ui.done(strings.Join(a.args, " "))
	}
}

// validateTargets checks scan targets the way the config does.
func validateTargets(list []string) error {
	if len(list) == 0 {
		return errors.New("name at least one network")
	}
	for _, t := range list {
		if _, err := config.ParseTarget(t, false); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
	}
	return nil
}
