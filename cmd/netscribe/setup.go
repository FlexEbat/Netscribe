package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/setup"
)

// runSetup is the guided install: it checks the machine, asks what it cannot decide and
// changes the system. See internal/setup.
func runSetup(args []string, e streams) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := setup.DefaultOptions()
	fs.StringVar(&o.ConfigPath, "config", o.ConfigPath, "path of the config file the setup writes or keeps")
	fs.BoolVar(&o.Yes, "yes", false, "take the recommended answer to every question")
	fs.BoolVar(&o.DryRun, "dry-run", false, "print what the setup would do and change nothing")
	fs.BoolVar(&o.NoService, "no-service", false, "do not install or start a systemd service")
	fs.StringVar(&o.Admin, "admin", "", "create the first administrator with this name when no account exists")
	fs.BoolVar(&o.PasswordStdin, "password-stdin", false, "read the password of the administrator from standard input")
	fs.StringVar(&o.Listen, "listen", o.Listen, "listen address of the panel in a new config")
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
	return setup.Setup(context.Background(), setupEnv(e), o, versionLine())
}

// runDoctor checks an install and changes nothing. It exits with 1 when a check failed.
func runDoctor(args []string, e streams) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := setup.DefaultOptions()
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
