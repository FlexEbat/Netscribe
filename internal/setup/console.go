package setup

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
)

// Console is the menu that "netscribe" opens on a terminal: the watcher, the checks, the
// settings, the accounts and the service in one place.
func Console(ctx context.Context, env Env, o Options, version string) error {
	s := newSession(env, o)
	s.version = version
	for {
		s.ui.banner("Netscribe", version)
		s.header(ctx)
		i, err := s.p.pick("What do you want to do?", []string{
			"Watch       live view of the service, the panel and the host",
			"Check       run every check and say what to fix",
			"Settings    change the configuration",
			"Accounts    list, add, reset a password, disable",
			"Service     start, stop, restart, log",
			"Setup       run the guided setup again",
			"Quit",
		}, 0)
		if err != nil {
			if errors.Is(err, errInput) {
				return nil // the terminal closed: leave quietly
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
			err = Configure(ctx, env, o, version)
		case 3:
			err = s.accounts(ctx)
		case 4:
			err = s.serviceMenu(ctx)
		case 5:
			err = Setup(ctx, env, o, version)
		default:
			return nil
		}
		if errors.Is(err, errInput) {
			return nil
		}
		if err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
		}
	}
}

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

func (s *session) accounts(ctx context.Context) error {
	acc, err := s.env.LookupUser(s.o.ServiceUser)
	if err != nil {
		return errors.New("the service account does not exist yet: run Setup first")
	}
	user := func(args ...string) Command {
		return Command{Name: s.o.BinPath, Args: append(append([]string{"user"}, args...), "--config", s.o.ConfigPath), As: &acc}
	}
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
		switch i {
		case 1:
			roles := []string{"viewer", "operator", "admin"}
			r, err := s.p.pick("Role", []string{"viewer: reads the topology", "operator: edits devices and starts scans", "admin: everything, including accounts"}, 0)
			if err != nil {
				return err
			}
			c = user("add", name, "--role", roles[r])
		case 2:
			c = user("passwd", name)
		case 3:
			c = user("disable", name)
		}
		c.Interactive = true
		if _, err := s.env.Run(ctx, c); err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
		}
	}
}

func (s *session) serviceMenu(ctx context.Context) error {
	systemctl := func(args ...string) Command { return Command{Name: "systemctl", Args: args} }
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
		args := map[int][]string{
			1: {"start", "netscribe"}, 2: {"stop", "netscribe"}, 3: {"restart", "netscribe"},
			4: {"kill", "--signal=HUP", "netscribe"}, 5: {"enable", "netscribe"}, 6: {"disable", "netscribe"},
		}[i]
		if err := s.runChecked(ctx, systemctl(args...)); err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
			continue
		}
		s.ui.done(strings.Join(args, " "))
	}
}
