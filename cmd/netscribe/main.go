// Command netscribe documents a local network from live scans.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/FlexEbat/Netscribe/internal/api"
	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
	"github.com/FlexEbat/Netscribe/web"
)

const usage = `usage: netscribe <command> [flags]

commands:
  serve     run the web panel
  scan      scan the targets from the config, or the CIDRs given as arguments
  devices   print the devices found so far

run "netscribe <command> -h" for the flags of a command`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	commands := map[string]func([]string, io.Writer, io.Writer) error{
		"serve":   serve,
		"scan":    scan,
		"devices": devices,
	}
	if cmd, ok := commands[args[0]]; ok {
		if err := cmd(args[1:], stdout, stderr); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			_, _ = fmt.Fprintf(stderr, "netscribe: %v\n", err)
			return 1
		}
		return 0
	}
	switch args[0] {
	case "-h", "--help", "help":
		_, _ = fmt.Fprintln(stderr, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "netscribe: unknown command %q\n\n%s\n", args[0], usage)
		return 2
	}
}

// commonFlags are the flags every command that reads the config accepts.
type commonFlags struct {
	fs         *flag.FlagSet
	configPath *string
	allowLoose *bool
}

func newCommonFlags(name string, stderr io.Writer) commonFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return commonFlags{
		fs:         fs,
		configPath: fs.String("config", "netscribe.yaml", "path to the config file"),
		allowLoose: fs.Bool("allow-loose-permissions", false, "run although the config file is readable by others"),
	}
}

// load parses the arguments and reads the config. It also installs the masking logger.
func (c commonFlags) load(args []string, stderr io.Writer) (config.Config, *slog.Logger, error) {
	if err := c.fs.Parse(args); err != nil {
		return config.Config{}, nil, err
	}
	logger := newLogger(stderr)
	slog.SetDefault(logger)
	cfg, err := config.Load(*c.configPath, *c.allowLoose)
	return cfg, logger, err
}

func serve(args []string, _, stderr io.Writer) error {
	cfg, logger, err := newCommonFlags("serve", stderr).load(args, stderr)
	if err != nil {
		return err
	}
	if insecureListen(cfg) {
		logger.Warn("listening beyond loopback without TLS or a trusted proxy: traffic is not encrypted", "listen", cfg.Listen)
	}

	db, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("close database", "err", err)
		}
	}()

	static, err := web.Dist()
	if err != nil {
		return fmt.Errorf("open web interface: %w", err)
	}
	srv := newServer(cfg, api.NewRouter(api.Options{Static: static}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Listen, "tls", cfg.TLS.CertFile != "")
		if cfg.TLS.CertFile != "" {
			errc <- srv.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
			return
		}
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// newServer applies the timeouts and limits of section 9.5 and the TLS floor of section 9.4.
func newServer(cfg config.Config, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Listen,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

// insecureListen reports a listen address outside loopback that has neither
// built-in TLS nor a trusted reverse proxy in front of it.
func insecureListen(cfg config.Config) bool {
	if cfg.TLS.CertFile != "" || len(cfg.TrustedProxies) > 0 {
		return false
	}
	host, _, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return true
	}
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

var sensitiveKeyParts = []string{"password", "token", "cookie", "authorization", "community", "communities", "secret"}

// newLogger returns a text logger that masks attributes whose key names a secret.
// It backs up config.Secret for values that never went through that type.
func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			key := strings.ToLower(a.Key)
			for _, part := range sensitiveKeyParts {
				if strings.Contains(key, part) {
					return slog.String(a.Key, "***")
				}
			}
			return a
		},
	}))
}

// scan runs one scan. Targets come from the arguments or, without any, from the config.
// Both go through the same checks, so an argument cannot reach beyond the allowed ranges.
func scan(args []string, stdout, stderr io.Writer) error {
	flags := newCommonFlags("scan", stderr)
	cfg, logger, err := flags.load(args, stderr)
	if err != nil {
		return err
	}
	targets, err := scanTargets(flags.fs.Args(), cfg)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("close database", "err", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	result, err := collector.NewScanner(st, cfg, logger).Run(ctx, targets)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return errors.New("scan canceled")
		}
		return fmt.Errorf("scan failed: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "scan %d: %s, %d devices\n", result.ID, result.Status, result.DeviceCount)
	return err
}

func scanTargets(args []string, cfg config.Config) ([]netip.Prefix, error) {
	raw := args
	if len(raw) == 0 {
		raw = cfg.Scan.Targets
	}
	if len(raw) == 0 {
		return nil, errors.New("no scan targets: set scan.targets in the config or pass a CIDR")
	}
	targets := make([]netip.Prefix, 0, len(raw))
	for _, t := range raw {
		p, err := config.ParseTarget(t, cfg.AllowPublicTargets)
		if err != nil {
			return nil, fmt.Errorf("scan target %q: %w", t, err)
		}
		targets = append(targets, p)
	}
	return targets, nil
}

// devices prints the stored devices as a table.
func devices(args []string, stdout, stderr io.Writer) error {
	cfg, logger, err := newCommonFlags("devices", stderr).load(args, stderr)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("close database", "err", err)
		}
	}()

	list, err := st.ListDevices(context.Background(), store.DeviceFilter{})
	if err != nil {
		return err
	}
	return printDevices(stdout, list)
}

func printDevices(w io.Writer, list []model.Device) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "IP\tMAC\tHOSTNAME\tONLINE")
	for _, d := range list {
		online := "no"
		if d.Online {
			online = "yes"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", printable(d.IP), printable(d.MAC), printable(d.Hostname), online)
	}
	return tw.Flush()
}

// printable replaces control characters, because hostnames come from the network
// and could carry terminal escape sequences.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, s)
}
