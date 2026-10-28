package setup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
)

const (
	healthPoll = 250 * time.Millisecond
	dropInName = "10-setup.conf"
	sysctlName = "99-netscribe.conf"
)

// ErrStopped is what Setup returns when the person quit or declined an action before the
// end. What was done stays done, and running the setup again continues from there.
var ErrStopped = errors.New("setup stopped before the end")

// session holds what the steps of one run share.
type session struct {
	env Env
	o   Options
	ui  ui
	p   *prompter

	// confirmEach makes every change of the system ask first. It is on in a run that talks
	// to a person and off with --yes and --dry-run.
	confirmEach bool

	version     string
	acc         *Account
	accPlanned  bool           // a dry run: the account does not exist, its ids are not known
	cfg         *config.Config // the config on disk, when there is one
	ans         Answers
	dockerGroup string // the group to add to the unit for the Docker socket
}

// loadConfig reads the config on disk.
func (s *session) loadConfig() (*config.Config, error) {
	cfg, err := config.Load(s.o.ConfigPath, false)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func newSession(env Env, o Options) *session {
	u := ui{w: env.Out, color: env.Color}
	return &session{env: env, o: o, ui: u, p: &prompter{in: env.In, ui: u, yes: o.Yes}}
}

// do asks first when the run talks to a person, then makes the change. A no stops the run:
// the steps after it need the change.
func (s *session) do(desc string, fn func() error) error {
	if s.confirmEach && !s.o.DryRun {
		ok, err := s.p.confirm(capitalize(desc)+"?", true)
		if err != nil {
			return err
		}
		if !ok {
			s.ui.note("not done: %s", desc)
			return fmt.Errorf("%w: %s", errDeclined, desc)
		}
	}
	return s.doConfirmed(desc, fn)
}

// capitalize makes the first letter of a description a capital, for a question.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// doConfirmed makes a change that a question before it already covered, or announces it in
// a dry run.
func (s *session) doConfirmed(desc string, fn func() error) error {
	if s.o.DryRun {
		s.ui.planned(desc)
		return nil
	}
	s.ui.doing(desc)
	if err := fn(); err != nil {
		s.ui.failed(desc, err)
		return fmt.Errorf("%s: %w", desc, err)
	}
	s.ui.done(desc)
	return nil
}

// runChecked runs a command and puts what it printed into the error.
func (s *session) runChecked(ctx context.Context, c Command) error {
	out, err := s.env.Run(ctx, c)
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s: %w: %s", c.Name, err, msg)
		}
		return fmt.Errorf("%s: %w", c.Name, err)
	}
	return nil
}

// Setup walks through the install: it checks the machine, asks what it cannot decide,
// changes the system and ends with the checks of Doctor.
func Setup(ctx context.Context, env Env, o Options, version string) error {
	s := newSession(env, o)
	s.version = version
	s.confirmEach = env.Interactive && !o.Yes && !o.DryRun
	s.ui.banner("Netscribe setup", version)
	if o.DryRun {
		s.ui.note("dry run: the setup prints what it would do and changes nothing")
	}
	if s.confirmEach {
		s.ui.note("every change asks first. Enter takes the answer in brackets, q quits at any question.")
	}
	steps := []struct {
		title string
		fn    func(context.Context) error
	}{
		{"System", s.system},
		{"The netscribe command", s.command},
		{"Service account", s.account},
		{"Features", s.features},
		{"Configuration", s.configure},
		{"ICMP probes", s.icmp},
		{"First administrator", s.admin},
		{"Service", s.service},
		{"Result", s.summary},
	}
	for i, st := range steps {
		s.ui.step(i+1, len(steps), st.title)
		if err := st.fn(ctx); err != nil {
			if isExit(err) || errors.Is(err, errDeclined) {
				s.ui.printf("\n  %s\n", s.ui.paint("33", "Setup stopped at step "+fmt.Sprint(i+1)+" ("+st.title+"). What was done stays done; run netscribe setup again to continue."))
				return fmt.Errorf("%w: %w", ErrStopped, err)
			}
			return err
		}
	}
	return nil
}

func (s *session) system(context.Context) error {
	s.ui.findings(checkSystem(s.env, s.o))
	if s.env.GOOS != "linux" {
		return errors.New("this system is not Linux: Netscribe runs on Linux only")
	}
	if s.env.Getuid() != 0 && !s.o.DryRun {
		return errors.New("the setup changes system files: run it with sudo")
	}
	if !s.env.Interactive && !s.o.Yes {
		return errors.New("the setup asks questions: run it in a terminal, or pass --yes to take the recommended answers")
	}
	return nil
}

func (s *session) command(context.Context) error {
	exe, err := s.env.Executable()
	if err != nil {
		return fmt.Errorf("find this program: %w", err)
	}
	if exe == s.o.BinPath {
		s.ui.done("netscribe is at " + s.o.BinPath)
		return nil
	}
	ok, err := s.p.confirm(fmt.Sprintf("Copy %s to %s, so that the command netscribe works everywhere?", exe, s.o.BinPath), true)
	if err != nil {
		return err
	}
	if !ok {
		s.ui.note("the unit starts %s: put the binary there before you start the service", s.o.BinPath)
		return nil
	}
	return s.doConfirmed("copy netscribe to "+s.o.BinPath, func() error { return s.env.CopyExecutable(exe, s.o.BinPath) })
}

func (s *session) account(ctx context.Context) error {
	acc, err := s.env.LookupUser(s.o.ServiceUser)
	if err != nil {
		if err := s.do("create the system account "+s.o.ServiceUser, func() error { return s.createAccount(ctx) }); err != nil {
			return err
		}
		acc, err = s.env.LookupUser(s.o.ServiceUser)
		if err != nil && !s.o.DryRun {
			return fmt.Errorf("the account %s does not exist after it was created: %w", s.o.ServiceUser, err)
		}
		if err != nil {
			acc = Account{Name: s.o.ServiceUser}
			s.accPlanned = true
		}
	} else {
		s.ui.done(fmt.Sprintf("service account %s exists (uid %d)", acc.Name, acc.UID))
	}
	s.acc = &acc
	dirs := []string{s.o.StateDir, filepath.Dir(s.o.ConfigPath)}
	return s.do("create "+strings.Join(dirs, " and ")+" for the service account, mode 0750", func() error {
		for _, dir := range dirs {
			if err := s.env.MkdirAll(dir, 0o750, s.acc); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *session) createAccount(ctx context.Context) error {
	nologin := "/usr/sbin/nologin"
	if p, err := s.env.LookPath("nologin"); err == nil {
		nologin = p
	}
	name := s.o.ServiceUser
	if _, err := s.env.LookPath("useradd"); err == nil {
		return s.runChecked(ctx, Command{Name: "useradd", Args: []string{
			"--system", "--user-group", "--home-dir", s.o.StateDir, "--no-create-home", "--shell", nologin, name}})
	}
	if _, err := s.env.LookPath("adduser"); err == nil {
		if err := s.runChecked(ctx, Command{Name: "addgroup", Args: []string{"-S", name}}); err != nil {
			return err
		}
		return s.runChecked(ctx, Command{Name: "adduser", Args: []string{"-S", "-D", "-H", "-h", s.o.StateDir, "-s", nologin, "-G", name, name}})
	}
	return errors.New("neither useradd nor adduser exists: create the system account by hand and run the setup again")
}

// packageManagers mirror scripts/install.sh.
var packageManagers = []struct {
	bin     string
	refresh []string
	install []string
}{
	{"apt-get", []string{"apt-get", "update", "-qq"}, []string{"apt-get", "install", "-y", "-qq"}},
	{"dnf", nil, []string{"dnf", "install", "-y", "-q"}},
	{"yum", nil, []string{"yum", "install", "-y", "-q"}},
	{"zypper", nil, []string{"zypper", "--non-interactive", "--quiet", "install"}},
	{"pacman", nil, []string{"pacman", "-S", "--noconfirm", "--needed"}},
	{"apk", nil, []string{"apk", "add", "--no-cache", "--quiet"}},
	{"xbps-install", nil, []string{"xbps-install", "-Sy"}},
}

// installPackages installs one package with the package manager of the host. When the
// first name is unknown it tries the next one, because Debian calls Chromium "chromium"
// and Ubuntu "chromium-browser".
func (s *session) installPackages(ctx context.Context, names ...string) error {
	for _, pm := range packageManagers {
		if _, err := s.env.LookPath(pm.bin); err != nil {
			continue
		}
		env := []string{"DEBIAN_FRONTEND=noninteractive"}
		if pm.refresh != nil {
			if err := s.runChecked(ctx, Command{Name: pm.refresh[0], Args: pm.refresh[1:], Env: env}); err != nil {
				return err
			}
		}
		var last error
		for _, name := range names {
			args := append(append([]string{}, pm.install[1:]...), name)
			if last = s.runChecked(ctx, Command{Name: pm.install[0], Args: args, Env: env}); last == nil {
				return nil
			}
		}
		return last
	}
	return errors.New("no known package manager found")
}

func (s *session) offerInstall(ctx context.Context, what string, names ...string) bool {
	if err := s.doConfirmed("install "+what, func() error { return s.installPackages(ctx, names...) }); err != nil {
		s.ui.note("continuing without %s: install it with your package manager", what)
		return false
	}
	return true
}

func (s *session) features(ctx context.Context) error {
	if _, err := s.env.Stat(s.o.ConfigPath); err == nil {
		cfg, found := checkConfig(s.env, s.o)
		if cfg == nil {
			s.ui.findings(found)
			return errors.New("the existing config is not usable: fix it first, the setup never overwrites it")
		}
		s.cfg = cfg
		s.ans = Answers{Docker: cfg.Docker.Enabled, DockerSocket: cfg.Docker.Socket, Nmap: cfg.Discovery.Nmap}
		s.ui.findings(checkFeatures(s.env, s.o, *cfg))
		if cfg.Discovery.Nmap {
			if _, err := s.env.LookPath("nmap"); err != nil && cfg.Discovery.NmapPath == "" {
				ok, err := s.p.confirm("Install Nmap, which the config uses?", true)
				if err != nil {
					return err
				}
				if ok {
					s.offerInstall(ctx, "nmap", "nmap")
				}
			}
		}
		s.dockerNeeds()
		return nil
	}
	return s.askFeatures(ctx)
}

func (s *session) askFeatures(ctx context.Context) error {
	s.ans.DockerSocket = "/var/run/docker.sock"

	_, have := s.env.LookPath("nmap")
	q := "Identify devices that nothing else could type with Nmap? It needs raw-socket rights for the service."
	if have != nil {
		q = "Install Nmap and use it to identify devices that nothing else could type? It needs raw-socket rights for the service."
	}
	nmap, err := s.p.confirm(q, false)
	if err != nil {
		return err
	}
	if nmap && have != nil {
		nmap = s.offerInstall(ctx, "nmap", "nmap")
	}
	s.ans.Nmap = nmap

	if _, ok := findChrome(s.env, ""); !ok {
		ok, err := s.p.confirm("Install Chromium for PDF export? Without it only Markdown, HTML and draw.io export work.", false)
		if err != nil {
			return err
		}
		if ok {
			s.offerInstall(ctx, "chromium", "chromium", "chromium-browser")
		}
	} else {
		s.ui.done("PDF export has a browser")
	}

	if _, err := s.env.StatSocket(s.ans.DockerSocket); err != nil {
		s.ui.note("no Docker socket at %s: the container view stays off", s.ans.DockerSocket)
		return nil
	}
	docker, err := s.p.confirm("Read container data from Docker?", true)
	if err != nil {
		return err
	}
	s.ans.Docker = docker
	s.dockerNeeds()
	return nil
}

// dockerNeeds decides whether the unit has to add the group of the Docker socket.
func (s *session) dockerNeeds() {
	s.dockerGroup = ""
	if !s.ans.Docker {
		return
	}
	sock, err := s.env.StatSocket(s.ans.DockerSocket)
	if err != nil || sock.Mode.Perm()&0o006 == 0o006 || sock.Group == "" {
		return
	}
	if s.acc != nil {
		if s.acc.GID == sock.GID {
			return
		}
		for _, g := range s.acc.Groups {
			if g == sock.GID {
				return
			}
		}
	}
	s.dockerGroup = sock.Group
}

func (s *session) configure(context.Context) error {
	if s.cfg != nil {
		s.ui.done("kept the config at " + s.o.ConfigPath)
		return nil
	}
	targets, err := s.chooseTargets()
	if err != nil {
		return err
	}
	listen, err := s.pickListen(s.o.Listen, false)
	if err != nil {
		return err
	}
	interval, err := s.p.ask("Scan every", "15m", func(v string) error {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Minute {
			return errors.New("use a duration of at least 1m, such as 15m or 1h")
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.ans.Targets, s.ans.Listen, s.ans.Interval = targets, listen.Listen, interval
	s.ans.TLSCert, s.ans.TLSKey, s.ans.Proxies = listen.TLSCert, listen.TLSKey, listen.Proxies
	s.ans.Database = filepath.Join(s.o.StateDir, "netscribe.db")
	if s.ans.DockerSocket == "" {
		s.ans.DockerSocket = "/var/run/docker.sock"
	}
	text := RenderConfig(s.ans)
	if s.o.DryRun {
		s.ui.planned("write " + s.o.ConfigPath + " with mode 0600:")
		for _, line := range strings.Split(strings.TrimRight(string(text), "\n"), "\n") {
			s.ui.printf("        %s\n", s.ui.dim(line))
		}
		return nil
	}
	if err := s.do("write "+s.o.ConfigPath+" with mode 0600", func() error {
		return s.env.WriteFile(s.o.ConfigPath, text, 0o600, s.acc)
	}); err != nil {
		return err
	}
	cfg, err := config.Load(s.o.ConfigPath, false)
	if err != nil {
		return fmt.Errorf("the config the setup wrote does not load, this is a bug: %w", err)
	}
	s.cfg = &cfg
	return nil
}

func (s *session) chooseTargets() ([]string, error) {
	valid := validateTargets
	if len(s.o.Targets) > 0 {
		return s.o.Targets, valid(s.o.Targets)
	}
	var cands []Candidate
	if ifaces, err := s.env.Interfaces(); err == nil {
		cands = Candidates(ifaces)
	}
	if len(cands) > 0 {
		labels := make([]string, len(cands))
		def := make([]bool, len(cands))
		for i, c := range cands {
			labels[i] = c.Prefix.String() + "  on " + c.Iface
			if c.Note != "" {
				labels[i] += ", " + c.Note
			}
			def[i] = true
		}
		picked, err := s.p.choose("Which networks should Netscribe scan?", labels, def)
		if err != nil {
			return nil, err
		}
		var out []string
		for i, ok := range picked {
			if ok {
				out = append(out, cands[i].Prefix.String())
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if s.o.Yes {
		return nil, errors.New("no private network found on this host: pass --targets 192.168.1.0/24")
	}
	text, err := s.p.ask("Networks to scan, CIDRs separated by commas", "", func(v string) error {
		return valid(splitList(v))
	})
	if err != nil {
		return nil, err
	}
	return splitList(text), nil
}

func splitList(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' })
}

func (s *session) icmp(ctx context.Context) error {
	gid := s.acc.GID
	allowed, disabled, known := pingAllowed(s.env, gid)
	switch {
	case !known:
		s.ui.note("cannot read %s: skipped", pingRange)
		return nil
	case allowed:
		s.ui.done("the service group may send ICMP echo")
		return nil
	case !disabled:
		s.ui.findings(checkICMP(s.env, s.o))
		s.ui.note("the current range allows other groups, so the setup leaves it to you")
		return nil
	}
	who := fmt.Sprintf("the group %s (gid %d)", s.o.ServiceUser, gid)
	if s.accPlanned {
		who = "the group " + s.o.ServiceUser + " once the account exists"
	}
	ok, err := s.p.confirm("ICMP echo is off for every group. Allow it for "+who+"? Without it hosts that drop TCP stay unseen.", true)
	if err != nil || !ok {
		return err
	}
	value := fmt.Sprintf("%d %d", gid, gid)
	path := filepath.Join(s.o.SysctlDir, sysctlName)
	if err := s.doConfirmed("write "+path, func() error {
		if err := s.env.MkdirAll(s.o.SysctlDir, 0o755, nil); err != nil { //nolint:gosec // sysctl.d is readable by everyone
			return err
		}
		return s.env.WriteFile(path, []byte("# Written by \"netscribe setup\": the service group may send ICMP echo.\nnet.ipv4.ping_group_range = "+value+"\n"), 0o644, nil) //nolint:gosec // a sysctl file is readable by everyone
	}); err != nil {
		return err
	}
	return s.doConfirmed("apply it now", func() error {
		return s.runChecked(ctx, Command{Name: "sysctl", Args: []string{"-w", "net.ipv4.ping_group_range=" + value}})
	})
}

func (s *session) admin(ctx context.Context) error {
	if s.o.DryRun {
		s.ui.planned("create the first administrator with netscribe user add")
		return nil
	}
	if n, ok := countAccounts(ctx, s.env, s.o); ok && n > 0 {
		s.ui.done(fmt.Sprintf("%d accounts exist: no administrator to create", n))
		return nil
	}
	name := s.o.Admin
	if name == "" {
		if s.o.Yes {
			s.ui.note("skipped: add --admin NAME to create the first administrator")
			return nil
		}
		var err error
		if name, err = s.p.ask("Name of the first administrator", "admin", nil); err != nil {
			return err
		}
	}
	for {
		c := Command{Name: s.o.BinPath, Args: []string{"user", "add", name, "--role", "admin", "--config", s.o.ConfigPath}, As: s.acc}
		switch {
		case s.o.PasswordStdin:
			c.Args = append(c.Args, "--password-stdin")
			c.Stdin = s.env.In
		case s.env.Interactive:
			c.Interactive = true
		default:
			return errors.New("the password needs a terminal: run in one, or pass --password-stdin")
		}
		s.ui.doing("create the administrator " + name)
		err := s.runChecked(ctx, c)
		if err == nil {
			s.ui.done("administrator " + name + " created")
			return nil
		}
		s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
		if !s.env.Interactive || s.o.Yes {
			return err
		}
		again, perr := s.p.confirm("Try again?", true)
		if perr != nil {
			return perr
		}
		if !again {
			return err
		}
	}
}

func (s *session) service(ctx context.Context) error {
	if s.o.NoService || !hasSystemd(s.env) {
		s.ui.note("no service installed. Start the panel with: %s serve --config %s", s.o.BinPath, s.o.ConfigPath)
		return nil
	}
	unit, err := RenderUnit(s.o)
	if err != nil {
		return err
	}
	changed := false
	write := func(path, text string) error {
		old, _ := s.env.ReadFile(path)
		if string(old) == text {
			s.ui.done(path + " is up to date")
			return nil
		}
		changed = true
		return s.do("write "+path, func() error {
			if err := s.env.MkdirAll(filepath.Dir(path), 0o755, nil); err != nil { //nolint:gosec // systemd directories are readable by everyone
				return err
			}
			return s.env.WriteFile(path, []byte(text), 0o644, nil) //nolint:gosec // a unit file is readable by everyone
		})
	}
	if err := write(filepath.Join(s.o.UnitDir, "netscribe.service"), unit); err != nil {
		return err
	}
	if drop := RenderDropIn(s.dockerGroup, s.ans.Nmap); drop != "" {
		if err := write(filepath.Join(s.o.UnitDir, "netscribe.service.d", dropInName), drop); err != nil {
			return err
		}
	}
	// Part of the service step the person just agreed to: no separate question.
	err = s.doConfirmed("install the helper that lets the panel update and restart the service", func() error {
		c, err := s.installPanelUnits(ctx)
		changed = changed || c
		return err
	})
	if err != nil && !errors.Is(err, errDeclined) {
		return err
	}
	if changed {
		if err := s.doConfirmed("reload systemd", func() error {
			return s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"daemon-reload"}})
		}); err != nil {
			return err
		}
	}
	start, err := s.p.confirm("Start the panel now and at every boot?", true)
	if err != nil {
		return err
	}
	if !start {
		s.ui.note("start it later with: sudo systemctl enable --now netscribe")
		return nil
	}
	verb := []string{"enable", "--now", "netscribe"}
	if state, _ := s.env.Run(ctx, Command{Name: "systemctl", Args: []string{"is-active", "netscribe"}}); changed && strings.TrimSpace(string(state)) == "active" {
		verb = []string{"restart", "netscribe"}
	}
	if err := s.doConfirmed("systemctl "+strings.Join(verb, " "), func() error {
		return s.runChecked(ctx, Command{Name: "systemctl", Args: verb})
	}); err != nil {
		return err
	}
	if s.o.DryRun {
		return nil
	}
	return s.waitHealthy(ctx)
}

// waitHealthy polls the panel until it answers. A service that fails on its config exits
// within a second, so the wait is short.
func (s *session) waitHealthy(ctx context.Context) error {
	cfg := config.Config{Listen: s.ans.Listen}
	if s.cfg != nil {
		cfg = *s.cfg
	}
	url := healthURL(cfg)
	s.ui.doing("wait for the panel at " + strings.TrimSuffix(url, "/healthz"))
	ctx, cancel := context.WithTimeout(ctx, s.o.HealthTimeout)
	defer cancel()
	tick := time.NewTicker(healthPoll)
	defer tick.Stop()
	var last error
	for {
		if last = s.env.Health(ctx, url); last == nil {
			s.ui.done("the panel answers")
			return nil
		}
		select {
		case <-ctx.Done():
			s.ui.failed("the panel does not answer", last)
			return fmt.Errorf("the panel does not answer at %s: %w; see journalctl -u netscribe -n 50 --no-pager", url, last)
		case <-tick.C:
		}
	}
}

func (s *session) summary(ctx context.Context) error {
	if s.o.DryRun {
		s.ui.note("dry run finished: nothing was changed")
		return nil
	}
	list := Diagnose(ctx, s.env, s.o)
	var warn, fail int
	for _, f := range list {
		switch f.Level {
		case LevelWarn:
			warn++
		case LevelFail:
			fail++
		}
		if f.Level != LevelOK {
			s.ui.finding(f)
		}
	}
	s.ui.printf("\n  %d checks, %d warnings, %d failures\n", len(list), warn, fail)
	if s.cfg != nil {
		url := strings.TrimSuffix(healthURL(*s.cfg), "/healthz")
		s.ui.section("Open the panel")
		s.ui.printf("  %s\n", s.ui.paint("1;36", url))
		host, port, _ := net.SplitHostPort(s.cfg.Listen)
		if isLoopback(host) {
			s.ui.note("from another machine: ssh -L %s:%s:%s USER@THIS_HOST, then open http://127.0.0.1:%s", port, host, port, port)
		}
	}
	s.ui.section("Later")
	s.ui.printf("  netscribe doctor              check this install again\n")
	s.ui.printf("  netscribe user list           accounts of the panel\n")
	s.ui.printf("  journalctl -u netscribe -f    the log of the service\n")
	if fail > 0 {
		return fmt.Errorf("%d checks failed", fail)
	}
	if !s.env.Interactive || s.o.Yes {
		return nil
	}
	s.ui.printf("\n  From now on the command %s opens the console: watch, checks, scans, export, settings, accounts, service.\n", s.ui.bold("netscribe"))
	open, err := s.p.confirm("Open the console now?", true)
	if isExit(err) || err == nil && !open {
		return nil
	}
	if err != nil {
		return err
	}
	return Console(ctx, s.env, s.o, s.version)
}

// Doctor runs every check and prints the findings by area. It changes nothing and
// returns an error when a check failed.
func Doctor(ctx context.Context, env Env, o Options, version string) error {
	u := ui{w: env.Out, color: env.Color}
	u.banner("Netscribe doctor", version)
	list := Diagnose(ctx, env, o)
	var ok, warn, fail int
	area := ""
	for _, f := range list {
		if f.Area != area {
			area = f.Area
			u.section(area)
		}
		u.finding(f)
		switch f.Level {
		case LevelOK:
			ok++
		case LevelWarn:
			warn++
		case LevelFail:
			fail++
		}
	}
	u.printf("\n  %d ok, %d warnings, %d failures\n", ok, warn, fail)
	if fail > 0 {
		return fmt.Errorf("%d checks failed", fail)
	}
	return nil
}
