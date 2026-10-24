// Command netscribe documents a local network from live scans.
package main

import (
	"bufio"
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

	"golang.org/x/term"

	"github.com/FlexEbat/Netscribe/internal/api"
	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/collector"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/events"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/render"
	"github.com/FlexEbat/Netscribe/internal/store"
	"github.com/FlexEbat/Netscribe/web"
)

const usage = `usage: netscribe <command> [flags]

commands:
  serve     run the web panel
  scan      scan the targets from the config, or the CIDRs given as arguments
  devices   print the devices found so far
  export    write the network documentation: --format md|html|pdf|drawio --out <file>
  user      manage accounts: add, list, disable, passwd
  setup     guided install: checks, recommendations, config, service, first account
  doctor    check an install and say what to fix
  version   print the version of this build

run "netscribe <command> -h" for the flags of a command`

// streams are the process's input and output, so tests can replace them.
type streams struct {
	in  io.Reader
	out io.Writer
	err io.Writer
	// prompt asks for a secret on the terminal without echo.
	prompt func(label string) (string, error)
	// interactive is true when standard input is a terminal, so the server may ask
	// questions such as which port to use.
	interactive bool
}

func main() {
	os.Exit(run(os.Args[1:], streams{in: os.Stdin, out: os.Stdout, err: os.Stderr, prompt: terminalPrompt, interactive: term.IsTerminal(int(os.Stdin.Fd()))}))
}

func run(args []string, e streams) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(e.err, usage)
		return 2
	}
	commands := map[string]func([]string, streams) error{
		"serve":   serve,
		"scan":    scan,
		"devices": devices,
		"export":  export,
		"user":    user,
		"setup":   runSetup,
		"doctor":  runDoctor,
	}
	if cmd, ok := commands[args[0]]; ok {
		if err := cmd(args[1:], e); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			_, _ = fmt.Fprintf(e.err, "netscribe: %v\n", err)
			return 1
		}
		return 0
	}
	switch args[0] {
	case "-h", "--help", "help":
		_, _ = fmt.Fprintln(e.err, usage)
		return 0
	case "version", "--version":
		if len(args) > 1 {
			_, _ = fmt.Fprintln(e.err, "usage: netscribe version")
			return 2
		}
		_, _ = fmt.Fprintln(e.out, versionLine())
		return 0
	default:
		_, _ = fmt.Fprintf(e.err, "netscribe: unknown command %q\n\n%s\n", args[0], usage)
		return 2
	}
}

// commonFlags are the flags every command that reads the config accepts.
type commonFlags struct {
	fs         *flag.FlagSet
	configPath *string
	allowLoose *bool
	args       []string // positional arguments, flags allowed anywhere around them
}

func newCommonFlags(name string, stderr io.Writer) *commonFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return &commonFlags{
		fs:         fs,
		configPath: fs.String("config", "netscribe.yaml", "path to the config file"),
		allowLoose: fs.Bool("allow-loose-permissions", false, "run although the config file is readable by others"),
	}
}

// load parses the arguments and reads the config. It also installs the masking logger.
func (c *commonFlags) load(args []string, stderr io.Writer) (config.Config, *slog.Logger, error) {
	positional, err := parseInterspersed(c.fs, args)
	if err != nil {
		return config.Config{}, nil, err
	}
	c.args = positional
	logger := newLogger(stderr)
	slog.SetDefault(logger)
	cfg, err := config.Load(*c.configPath, *c.allowLoose)
	return cfg, logger, err
}

func serve(args []string, e streams) error {
	flags := newCommonFlags("serve", e.err)
	listenFlag := flags.fs.String("listen", "", "address to listen on, instead of listen from the config")
	autoPort := flags.fs.Bool("auto-port", false, "use the next free port when the port is already in use")
	cfg, logger, err := flags.load(args, e.err)
	if err != nil {
		return err
	}
	fileCfg := cfg // the reload watcher compares against what the file says, not the flag
	if *listenFlag != "" {
		if err := config.ValidateListen(*listenFlag); err != nil {
			return fmt.Errorf("--listen %q: %w", *listenFlag, err)
		}
		cfg.Listen = *listenFlag
	}
	if cfg.TLS.CertFile != "" {
		// A bad certificate would otherwise show only after the workers have started.
		if _, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile); err != nil {
			return fmt.Errorf("load tls certificate: %w", err)
		}
	}

	// The port is taken before anything else starts: a busy port ends the command here,
	// with no scan or watcher running behind a server that never came up.
	ln, err := bindListener(cfg.Listen, bindOptions{auto: *autoPort, ask: lineAsker(e), warn: logger.Warn})
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }() // Shutdown closes it first, a second close only reports that
	cfg.Listen = ln.Addr().String()
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
	closed, err := db.FailRunningScans(context.Background(), "interrupted: the server stopped before the scan ended")
	if err != nil {
		return err
	}
	if closed > 0 {
		logger.Warn("closed scans that an earlier run left unfinished", "count", closed)
	}

	static, err := web.Dist()
	if err != nil {
		return fmt.Errorf("open web interface: %w", err)
	}
	svc, err := auth.NewService(db, cfg.Auth, nil)
	if err != nil {
		return err
	}
	proxies, err := parseProxies(cfg.TrustedProxies)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// SIGHUP reloads the config. It is caught here, before anything runs, because an
	// uncaught SIGHUP ends the process.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)

	if _, err := configuredTargets(cfg); err != nil {
		return err
	}
	live := config.NewWatcher(*flags.configPath, *flags.allowLoose, fileCfg, logger)
	hub := events.NewHub()
	scanner := collector.NewScanner(db, cfg, logger)
	scanner.SetEvents(hub)
	router := api.NewRouter(api.Options{
		Static: static, Auth: svc, Audit: db, AuditLog: db, Repo: db, Events: hub,
		Containers: db, Docker: scanner.Docker(), Nmap: scanner,
		Identifier:     deviceIdentifier{scanner: scanner, repo: db, hub: hub, ctx: ctx, log: logger},
		ExportOptions:  func() render.Options { return render.Options{ChromePath: live.Current().Export.ChromePath} },
		Scanner:        scanStarter{scanner: scanner, ctx: ctx, config: live.Current},
		TrustedProxies: proxies, Logger: logger,
	})
	srv := newServer(cfg, router)
	go maintain(ctx, db, cfg.Audit.KeepDays, func() int { return live.Current().History.KeepScans }, time.Hour, logger)
	// The watcher asks for the targets at every connect, so a reload that changes
	// scan.targets reaches it through Docker().Retarget, which SetConfig calls.
	liveTargets := func() []netip.Prefix {
		list, err := configuredTargets(live.Current())
		if err != nil {
			return nil // a reloaded config is validated, so this is not expected
		}
		return list
	}
	go scanner.Docker().Watch(ctx, db, liveTargets, func() { hub.Publish("topology.changed", struct{}{}) })
	go scanner.Schedule(ctx, configuredTargets)
	reloaded, failed := configHooks(scanner, hub, db, logger)
	go func() {
		if err := live.Run(ctx, hup, func(c config.Config) { reloaded(ctx, c) }, func(err error) { failed(ctx, err) }); err != nil {
			logger.Error("config reload is off, restart to apply changes", "err", err)
		}
	}()

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Listen, "tls", cfg.TLS.CertFile != "")
		if cfg.TLS.CertFile != "" {
			errc <- srv.ServeTLS(ln, cfg.TLS.CertFile, cfg.TLS.KeyFile)
			return
		}
		errc <- srv.Serve(ln)
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

// lineAsker returns a function that asks one question on the terminal, or nil when
// standard input is not a terminal and nobody can answer.
func lineAsker(e streams) func(string) (string, error) {
	if !e.interactive {
		return nil
	}
	r := bufio.NewReader(e.in)
	return func(prompt string) (string, error) {
		_, _ = fmt.Fprint(e.err, prompt)
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return "", err
		}
		return line, nil
	}
}

// scanStarter runs scans for the API over the targets of the config in force. A scan
// lives as long as the server does, not as long as the request that started it.
type scanStarter struct {
	scanner *collector.Scanner
	ctx     context.Context
	config  func() config.Config
}

func (s scanStarter) Start() (model.Scan, error) {
	targets, err := configuredTargets(s.config())
	if err != nil {
		return model.Scan{}, err
	}
	return s.scanner.Start(s.ctx, targets)
}

// StartWithNmap is Start with nmap switched on for this scan.
func (s scanStarter) StartWithNmap() (model.Scan, error) {
	targets, err := configuredTargets(s.config())
	if err != nil {
		return model.Scan{}, err
	}
	return s.scanner.StartWithNmap(s.ctx, targets)
}

// deviceIdentifier runs nmap for one device on behalf of the API. The work goes on after
// the request ended, for as long as the server lives.
type deviceIdentifier struct {
	scanner identifyStarter
	repo    identityStore
	hub     *events.Hub
	ctx     context.Context
	log     *slog.Logger
}

// identifyStarter starts the nmap run. *collector.Scanner satisfies it.
type identifyStarter interface {
	StartIdentify(ctx context.Context, ip string, done func(collector.IdentifyResult, error)) error
}

// identityStore is the part of the store the identification needs.
type identityStore interface {
	GetDevice(ctx context.Context, id int64) (model.Device, error)
	ApplyIdentity(ctx context.Context, id int64, osGuess string, kind model.DeviceKind) (model.Device, error)
}

func (d deviceIdentifier) Identify(id int64) error {
	dev, err := d.repo.GetDevice(d.ctx, id)
	if err != nil {
		return err
	}
	return d.scanner.StartIdentify(d.ctx, dev.IP, func(res collector.IdentifyResult, err error) {
		if err != nil {
			if d.ctx.Err() == nil {
				d.log.Warn("identify device", "device", id, "err", err)
			}
			return
		}
		if _, err := d.repo.ApplyIdentity(d.ctx, id, res.OS, res.Kind); err != nil {
			if !errors.Is(err, model.ErrNotFound) {
				d.log.Error("store identity", "device", id, "err", err)
			}
			return
		}
		d.hub.Publish("topology.changed", struct{}{})
	})
}

// configApplier takes a reloaded config. *collector.Scanner satisfies it.
type configApplier interface {
	SetConfig(cfg config.Config)
}

// restrictedPublisher sends events to readers who hold a right. *events.Hub satisfies it.
type restrictedPublisher interface {
	PublishFor(perm model.Permission, name string, data any)
}

// configError is the data of a config.error event.
type configError struct {
	Error string `json:"error"`
}

// configHooks returns what runs after the config file was reloaded or refused. A reload
// is applied, announced to the users who may manage the server and written to the audit
// log. A refused file changes nothing but the announcement and the log. The error text
// comes from config.Load, which keeps secret values out of it.
func configHooks(scanner configApplier, hub restrictedPublisher, audit api.Auditor, log *slog.Logger) (reloaded func(context.Context, config.Config), failed func(context.Context, error)) {
	record := func(ctx context.Context, action, result, detail string) {
		err := audit.AddAudit(ctx, model.AuditEntry{
			Action: action, Result: result, Username: "system", Entity: "config", EntityID: "netscribe.yaml", Detail: detail,
		})
		if err != nil {
			log.Error("write audit entry", "action", action, "err", err)
		}
	}
	reloaded = func(ctx context.Context, cfg config.Config) {
		scanner.SetConfig(cfg)
		hub.PublishFor(model.PermUsersManage, "config.reloaded", struct{}{})
		record(ctx, "config_reload", "ok", "")
	}
	failed = func(ctx context.Context, err error) {
		hub.PublishFor(model.PermUsersManage, "config.error", configError{Error: err.Error()})
		record(ctx, "config_reload_failed", "error", err.Error())
	}
	return reloaded, failed
}

// configuredTargets parses scan.targets. An empty list is allowed: scans then answer
// collector.ErrNoTargets until targets are configured.
func configuredTargets(cfg config.Config) ([]netip.Prefix, error) {
	targets := make([]netip.Prefix, 0, len(cfg.Scan.Targets))
	for _, t := range cfg.Scan.Targets {
		p, err := config.ParseTarget(t, cfg.AllowPublicTargets)
		if err != nil {
			return nil, fmt.Errorf("scan target %q: %w", t, err)
		}
		targets = append(targets, p)
	}
	return targets, nil
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
func scan(args []string, e streams) error {
	flags := newCommonFlags("scan", e.err)
	useNmap := flags.fs.Bool("nmap", false, "ask nmap about devices nothing else could type, for this scan only")
	cfg, logger, err := flags.load(args, e.err)
	if err != nil {
		return err
	}
	if *useNmap {
		cfg.Discovery.Nmap = true
	}
	targets, err := scanTargets(flags.args, cfg)
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
	_, err = fmt.Fprintf(e.out, "scan %d: %s, %d devices\n", result.ID, result.Status, result.DeviceCount)
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
func devices(args []string, e streams) error {
	cfg, logger, err := newCommonFlags("devices", e.err).load(args, e.err)
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
	return printDevices(e.out, list)
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

// parseInterspersed parses flags that may appear before, between and after positional
// arguments, which the standard flag package does not do on its own.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func parseProxies(list []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// pruner is the part of the store that housekeeping needs.
type pruner interface {
	PruneSessions(ctx context.Context, now time.Time) error
	PruneAudit(ctx context.Context, keepDays int) error
	PruneScans(ctx context.Context, keep int) (int64, error)
}

// maintain removes expired sessions and old audit entries, once at start and then every interval.
// keepScans is asked on every run, because history.keepScans changes with the config.
func maintain(ctx context.Context, p pruner, keepDays int, keepScans func() int, interval time.Duration, log *slog.Logger) {
	run := func() {
		if err := p.PruneSessions(ctx, time.Now()); err != nil && ctx.Err() == nil {
			log.Error("prune sessions", "err", err)
		}
		if err := p.PruneAudit(ctx, keepDays); err != nil && ctx.Err() == nil {
			log.Error("prune audit log", "err", err)
		}
		if _, err := p.PruneScans(ctx, keepScans()); err != nil && ctx.Err() == nil {
			log.Error("prune scans", "err", err)
		}
	}
	run()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			run()
		}
	}
}

// terminalPrompt reads a secret from the controlling terminal without echoing it.
func terminalPrompt(label string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("standard input is not a terminal: pass --password-stdin to read the password from a pipe")
	}
	_, _ = fmt.Fprint(os.Stderr, label)
	b, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// readPassword takes the password from standard input (one line) or asks twice on the terminal.
// There is deliberately no flag that carries the password itself: it would end up in the
// shell history and in the process list.
func readPassword(e streams, fromStdin bool) (string, error) {
	if fromStdin {
		line, err := bufio.NewReader(e.in).ReadString('\n')
		if err != nil && (err != io.EOF || line == "") {
			return "", errors.New("no password on standard input")
		}
		pw := strings.TrimRight(line, "\r\n")
		if pw == "" {
			return "", errors.New("the password is empty")
		}
		return pw, nil
	}
	first, err := e.prompt("Password: ")
	if err != nil {
		return "", err
	}
	second, err := e.prompt("Repeat password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("the passwords do not match")
	}
	return first, nil
}

// user manages accounts from the command line, the only way to create the first administrator.
func user(args []string, e streams) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		_, _ = fmt.Fprintln(e.err, "usage: netscribe user <add|list|disable|passwd> [flags]")
		return flag.ErrHelp
	}
	sub, rest := args[0], args[1:]
	flags := newCommonFlags("user "+sub, e.err)
	role := flags.fs.String("role", "", "role of the new account: viewer, operator or admin")
	fromStdin := flags.fs.Bool("password-stdin", false, "read the password from standard input instead of the terminal")
	cfg, logger, err := flags.load(rest, e.err)
	if err != nil {
		return err
	}

	switch sub {
	case "add", "disable", "passwd":
		if len(flags.args) != 1 {
			return fmt.Errorf("user %s needs exactly one user name", sub)
		}
	case "list":
		if len(flags.args) != 0 {
			return errors.New("user list takes no arguments")
		}
	default:
		return fmt.Errorf("unknown user command %q: use add, list, disable or passwd", sub)
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
	svc, err := auth.NewService(st, cfg.Auth, nil)
	if err != nil {
		return err
	}
	ctx := context.Background()
	name := ""
	if len(flags.args) == 1 {
		name = flags.args[0]
	}

	switch sub {
	case "add":
		r, err := auth.ParseRole(*role)
		if err != nil {
			return errors.New("--role is required: viewer, operator or admin")
		}
		pw, err := readPassword(e, *fromStdin)
		if err != nil {
			return err
		}
		u, err := svc.CreateUser(ctx, auth.NewUser{Username: name, Role: r, Password: pw})
		if errors.Is(err, model.ErrConflict) {
			return fmt.Errorf("user %q already exists", name)
		}
		if err != nil {
			return err
		}
		auditCLI(ctx, st, logger, "user_create", u.Username, "role "+string(u.Role))
		_, err = fmt.Fprintf(e.out, "user %s created with role %s\n", u.Username, u.Role)
		return err

	case "list":
		users, err := svc.ListUsers(ctx)
		if err != nil {
			return err
		}
		return printUsers(e.out, users)

	case "disable":
		u, err := svc.SetDisabled(ctx, name, true)
		if errors.Is(err, model.ErrNotFound) {
			return fmt.Errorf("no user %q", name)
		}
		if err != nil {
			return err
		}
		auditCLI(ctx, st, logger, "user_update", u.Username, "disabled")
		_, err = fmt.Fprintf(e.out, "user %s disabled, its sessions are closed\n", u.Username)
		return err

	default: // passwd
		pw, err := readPassword(e, *fromStdin)
		if err != nil {
			return err
		}
		err = svc.ResetPassword(ctx, name, pw, false)
		if errors.Is(err, model.ErrNotFound) {
			return fmt.Errorf("no user %q", name)
		}
		if err != nil {
			return err
		}
		auditCLI(ctx, st, logger, "user_password_reset", name, "")
		_, err = fmt.Fprintf(e.out, "password of %s changed, its sessions are closed\n", name)
		return err
	}
}

// auditCLI records an account change made on the command line. Failing to record it
// is reported but does not undo the change.
func auditCLI(ctx context.Context, a api.Auditor, log *slog.Logger, action, username, detail string) {
	err := a.AddAudit(ctx, model.AuditEntry{
		Action: action, Result: "ok", Username: "cli", Entity: "user", EntityID: username, Detail: detail,
	})
	if err != nil {
		log.Error("write audit entry", "action", action, "err", err)
	}
}

func printUsers(w io.Writer, users []model.User) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "USERNAME\tROLE\tSTATUS\tLAST LOGIN")
	for _, u := range users {
		status := "enabled"
		if u.Disabled {
			status = "disabled"
		}
		last := "never"
		if u.LastLoginAt != nil {
			last = u.LastLoginAt.UTC().Format("2006-01-02 15:04:05")
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", printable(u.Username), u.Role, status, last)
	}
	return tw.Flush()
}
