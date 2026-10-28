package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/FlexEbat/Netscribe/internal/setup"
)

// runSetup is the guided install: it checks the machine, asks what it cannot decide and
// changes the system. See internal/setup.
func runSetup(args []string, e streams) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := baseOptions()
	fs.StringVar(&o.ConfigPath, "config", o.ConfigPath, "path of the config file the setup writes or keeps")
	fs.BoolVar(&o.Yes, "yes", false, "take the recommended answer to every question")
	fs.BoolVar(&o.DryRun, "dry-run", false, "print what the setup would do and change nothing")
	fs.BoolVar(&o.NoService, "no-service", false, "do not install or start a systemd service")
	fs.StringVar(&o.Admin, "admin", "", "create the first administrator with this name when no account exists")
	fs.BoolVar(&o.PasswordStdin, "password-stdin", false, "read the password of the administrator from standard input")
	fs.StringVar(&o.Listen, "listen", o.Listen, "listen address of the panel in a new config, for example 127.0.0.1:8080 or 0.0.0.0:8080")
	fs.StringVar(&o.TLSCert, "tls-cert", "", "certificate (PEM) the panel serves, for a listen address beyond loopback")
	fs.StringVar(&o.TLSKey, "tls-key", "", "private key (PEM) of the certificate")
	proxies := fs.String("trusted-proxy", "", "address or network of the reverse proxy in front of the panel, separated by commas")
	fs.BoolVar(&o.PlainHTTP, "plain-http", false, "accept a listen address beyond loopback without TLS or a proxy")
	targets := fs.String("targets", "", "scan targets of a new config, CIDRs separated by commas; default: the networks of this host")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("setup takes no arguments")
	}
	if *targets != "" {
		o.Targets = strings.FieldsFunc(*targets, func(r rune) bool { return r == ',' || r == ' ' })
	}
	if *proxies != "" {
		o.TrustedProxies = strings.FieldsFunc(*proxies, func(r rune) bool { return r == ',' || r == ' ' })
	}
	return setup.Setup(context.Background(), setupEnv(e), o, versionLine())
}

// runDoctor checks an install and changes nothing. It exits with 1 when a check failed.
func runDoctor(args []string, e streams) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := baseOptions()
	fs.StringVar(&o.ConfigPath, "config", o.ConfigPath, "path of the config file to check")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("doctor takes no arguments")
	}
	return setup.Doctor(context.Background(), setupEnv(e), o, versionLine())
}

func setupEnv(e streams) setup.Env {
	color := e.interactive && os.Getenv("NO_COLOR") == ""
	return setup.NewEnv(e.in, e.out, e.interactive, color)
}

// runConsole opens the menu: watch, checks, scans, devices, export, settings, accounts and the service.
func runConsole(args []string, e streams) error {
	fs := flag.NewFlagSet("console", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := baseOptions()
	fs.StringVar(&o.ConfigPath, "config", o.ConfigPath, "path of the config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("console takes no arguments")
	}
	if !e.interactive {
		return errors.New("the console needs a terminal: use netscribe watch --once, doctor or config instead")
	}
	return setup.Console(context.Background(), setupEnv(e), o, versionLine())
}

// runWatch shows the state of the service and the host until Ctrl-C. With --once it prints
// one sample and exits with 1 when the service is not up, for cron and for monitors.
func runWatch(args []string, e streams) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := baseOptions()
	fs.StringVar(&o.ConfigPath, "config", o.ConfigPath, "path of the config file")
	fs.DurationVar(&o.WatchEvery, "interval", o.WatchEvery, "time between two samples")
	fs.BoolVar(&o.Once, "once", false, "print one sample and exit with 1 when the service is not up")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("watch takes no arguments")
	}
	if o.WatchEvery < 200*time.Millisecond {
		return errors.New("--interval must be at least 200ms")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return setup.Watch(ctx, setupEnv(e), o, versionLine())
}

const configUsage = `usage: netscribe config [--config FILE] [command]

  (none)         the interactive editor: every setting with its value and advice
  show           print the config, secrets hidden
  get KEY        print the value of one key
  set KEY VALUE  change one key, check the whole config first, keep a .bak
  edit           open the file in $EDITOR, check it before it replaces the file
`

// runConfig changes the configuration: interactively, key by key, or in an editor.
func runConfig(args []string, e streams) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := baseOptions()
	fs.StringVar(&o.ConfigPath, "config", o.ConfigPath, "path of the config file")
	if err := fs.Parse(interspersedFirst(args)); err != nil {
		return err
	}
	env, rest := setupEnv(e), fs.Args()
	ctx := context.Background()
	if len(rest) == 0 {
		if !e.interactive {
			_, _ = fmt.Fprint(e.err, configUsage)
			return errors.New("the interactive editor needs a terminal")
		}
		return setup.Configure(ctx, env, o, versionLine())
	}
	switch rest[0] {
	case "show":
		return setup.ConfigShow(env, o)
	case "edit":
		return setup.ConfigEdit(ctx, env, o)
	case "get":
		if len(rest) != 2 {
			return errors.New("usage: netscribe config get KEY")
		}
		return setup.ConfigGet(env, o, rest[1])
	case "set":
		if len(rest) < 3 {
			return errors.New("usage: netscribe config set KEY VALUE")
		}
		return setup.ConfigSet(ctx, env, o, rest[1], rest[2:])
	}
	_, _ = fmt.Fprint(e.err, configUsage)
	return fmt.Errorf("unknown command %q", rest[0])
}

// interspersedFirst moves --config FILE in front of the words, so that it works after them
// as well: netscribe config set scan.interval 1h --config FILE.
func interspersedFirst(args []string) []string {
	var flags, words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-config":
			flags = append(flags, a)
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		case strings.HasPrefix(a, "--config=") || strings.HasPrefix(a, "-config="):
			flags = append(flags, a)
		default:
			words = append(words, a)
		}
	}
	return append(flags, words...)
}
