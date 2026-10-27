package setup

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/FlexEbat/Netscribe/internal/config"
)

type kind int

const (
	kText kind = iota
	kBool
	kInt
	kDuration
	kList
	kPorts
	kEnum
	kSecrets
	kTargets
)

// setting is one key of the config, with what a person needs to decide about it.
type setting struct {
	path    []string
	label   string
	kind    kind
	help    string
	def     string // the default, for the display
	hot     bool   // a running service applies a change without a restart
	choices []string
}

func (st setting) key() string { return strings.Join(st.path, ".") }

type group struct {
	name  string
	hint  string
	items []setting
}

func p(path string) []string { return strings.Split(path, ".") }

// settingGroups mirrors internal/config. The hot flag mirrors mergeHot in config/watch.go:
// scan, snmp, ssh, docker, history and export apply to a running service, every other key
// needs a restart. A test checks that every key here loads.
var settingGroups = []group{
	{name: "Scanning", hint: "what is scanned and how often", items: []setting{
		{path: p("scan.targets"), label: "Networks to scan", kind: kTargets, hot: true,
			help: "Private networks in CIDR form, a /16 at most. ARP discovery sees the networks this host is connected to."},
		{path: p("scan.interval"), label: "Scan every", kind: kDuration, hot: true, def: "15m",
			help: "A duration such as 15m or 1h, at least 1m. 0 turns the schedule off: scans run only on request."},
		{path: p("scan.ports"), label: "TCP ports to probe", kind: kPorts, hot: true, def: "22, 53, 80, 443, 445, 515, 554, 631, 3389, 8080, 9100",
			help: "Ports that decide what a host is: 22, 80, 443 and so on. A longer list makes a scan slower."},
		{path: p("allowPublicTargets"), label: "Allow public networks as targets", kind: kBool, def: "off",
			help: "Off keeps the scanner inside private ranges. Turn it on only for networks you own."},
		{path: p("discovery.probeRoutedTargets"), label: "Probe networks behind a router", kind: kBool, def: "off",
			help: "Lets targets that this host does not touch directly be probed with TCP and ICMP."},
	}},
	{name: "Discovery", hint: "how devices are told apart", items: []setting{
		{path: p("discovery.nmap"), label: "Identify devices with Nmap", kind: kBool, def: "off",
			help: "Asks an installed nmap about devices that nothing else could type. Needs raw-socket rights for the service."},
		{path: p("discovery.nmapScope"), label: "Which devices Nmap asks about", kind: kEnum, choices: []string{"untyped", "all"}, def: "untyped",
			help: "untyped: only devices nothing else could type. all: every device, slower and noisier."},
		{path: p("discovery.mdns"), label: "mDNS", kind: kBool, def: "on", help: "One PTR query in each local segment."},
		{path: p("discovery.ssdp"), label: "SSDP", kind: kBool, def: "on", help: "One M-SEARCH in each local segment."},
		{path: p("discovery.httpHeaders"), label: "HTTP banners", kind: kBool, def: "on", help: "A HEAD / request on the open web ports."},
		{path: p("discovery.routerTables"), label: "Router tables over SNMP", kind: kBool, def: "on", help: "Reads ARP and bridge tables of routers and switches."},
		{path: p("discovery.nmapPath"), label: "Path of nmap", kind: kText, def: "found in PATH",
			help: "An absolute path of the nmap program. Empty means look for nmap in PATH."},
	}},
	{name: "Docker", hint: "the container view", items: []setting{
		{path: p("docker.enabled"), label: "Read containers from Docker", kind: kBool, hot: true, def: "off",
			help: "The service reads container data through the Docker socket. It needs read access to the socket."},
		{path: p("docker.socket"), label: "Docker socket", kind: kText, hot: true, def: "/var/run/docker.sock", help: "The path of the Unix socket."},
	}},
	{name: "SNMP", hint: "device queries", items: []setting{
		{path: p("snmp.communities"), label: "Communities", kind: kSecrets, hot: true,
			help: "Read-only community strings, tried in order. Use a community that the devices restrict by ACL, not public."},
		{path: p("snmp.timeout"), label: "Timeout of a query", kind: kDuration, hot: true, def: "2s", help: "How long one SNMP request waits."},
	}},
	{name: "History", hint: "how long scans and the audit log are kept", items: []setting{
		{path: p("history.keepScans"), label: "Scans to keep", kind: kInt, hot: true, def: "200", help: "Older scans are pruned."},
		{path: p("audit.keepDays"), label: "Days to keep the audit log", kind: kInt, def: "365", help: "Entries of the audit log older than this are pruned."},
	}},
	{name: "Sign-in policy", hint: "accounts and sessions", items: []setting{
		{path: p("auth.minPasswordLength"), label: "Shortest password", kind: kInt, def: "12", help: "Longer is better. Twelve is the recommended minimum."},
		{path: p("auth.maxFailedLogins"), label: "Failed sign-ins before a lockout", kind: kInt, def: "5", help: "Counted per address and username."},
		{path: p("auth.lockoutDuration"), label: "Lockout time", kind: kDuration, def: "15m", help: "How long a locked pair has to wait."},
		{path: p("auth.sessionIdleTimeout"), label: "Idle session timeout", kind: kDuration, def: "8h", help: "A session with no request for this long ends."},
		{path: p("auth.sessionMaxAge"), label: "Longest session", kind: kDuration, def: "168h", help: "A session ends after this time, active or not."},
	}},
	{name: "Export", hint: "reports", items: []setting{
		{path: p("export.chromePath"), label: "Browser for PDF export", kind: kText, hot: true, def: "found in PATH",
			help: "The path of chromium or chrome. Empty means look for one in PATH."},
	}},
	{name: "SSH", hint: "reading hosts over SSH (the hosts themselves: SSH hosts)", items: []setting{
		{path: p("ssh.knownHosts"), label: "known_hosts file", kind: kText, hot: true, def: "~/.ssh/known_hosts",
			help: "Strict: a host that is not listed, or whose key changed, is skipped. Fill it with ssh-keyscan and check the fingerprint."},
	}},
}

func findSetting(key string) (setting, bool) {
	for _, g := range settingGroups {
		for _, it := range g.items {
			if it.key() == key {
				return it, true
			}
		}
	}
	return setting{}, false
}

// display is what a person sees as the current value.
func display(doc *yamlDoc, st setting) string {
	n := doc.find(st.path)
	switch {
	case st.kind == kSecrets:
		if c := count(n); c > 0 {
			return fmt.Sprintf("%d set", c)
		}
		return "none"
	case n == nil || value(n) == "":
		if st.def != "" {
			return "default, " + st.def
		}
		return "not set"
	}
	return value(n)
}

func (s *session) openConfig() (*yamlDoc, error) {
	data, err := s.env.ReadFile(s.o.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w (run it with sudo, or run netscribe setup first)", s.o.ConfigPath, err)
	}
	return parseYAML(data)
}

// ownerOfFile returns the owner of the config, so a rewrite keeps it. Only root may change
// an owner, and only root needs to.
func (s *session) ownerOfFile(path string) *Account {
	if s.env.Getuid() != 0 {
		return nil
	}
	info, err := s.env.Stat(path)
	if err != nil {
		return nil
	}
	if uid, gid, ok := ownerOf(info); ok {
		return &Account{UID: uid, GID: gid}
	}
	return nil
}

// validate loads the text as a config, from a temporary file next to the real one.
func (s *session) validate(data []byte) error {
	tmp := filepath.Join(filepath.Dir(s.o.ConfigPath), ".netscribe-check.yaml")
	if err := s.env.WriteFile(tmp, data, 0o600, s.ownerOfFile(s.o.ConfigPath)); err != nil {
		return err
	}
	_, err := config.Load(tmp, false)
	_ = s.env.Remove(tmp) // a leftover check file is harmless and the next check replaces it
	return err
}

// saveConfig checks the new text, keeps the old one as a .bak and replaces the file.
func (s *session) saveConfig(data []byte) error {
	if err := s.validate(data); err != nil {
		return fmt.Errorf("the config would not be valid, nothing was saved: %w", err)
	}
	owner := s.ownerOfFile(s.o.ConfigPath)
	if old, err := s.env.ReadFile(s.o.ConfigPath); err == nil {
		if err := s.env.WriteFile(s.o.ConfigPath+".bak", old, 0o600, owner); err != nil {
			return fmt.Errorf("keep a backup: %w", err)
		}
	}
	if err := s.env.WriteFile(s.o.ConfigPath, data, 0o600, owner); err != nil {
		return err
	}
	if cfg, err := config.Load(s.o.ConfigPath, false); err == nil {
		s.cfg = &cfg
	}
	return nil
}

// commit saves the document and tells whether the service applies it by itself.
func (s *session) commit(ctx context.Context, doc *yamlDoc, hot bool) error {
	data, err := doc.bytes()
	if err != nil {
		return err
	}
	if err := s.saveConfig(data); err != nil {
		return err
	}
	s.ui.done("saved " + s.o.ConfigPath + " (the old file is " + filepath.Base(s.o.ConfigPath) + ".bak)")
	return s.afterSave(ctx, hot)
}

// afterSave tells whether the running service takes the change by itself, and offers the
// restart when it does not.
func (s *session) afterSave(ctx context.Context, hot bool) error {
	if !hasSystemd(s.env) || !s.serviceActive(ctx) {
		return nil
	}
	if hot {
		s.ui.note("the running service applies this by itself in a moment")
		return nil
	}
	ok, err := s.p.confirm("This key needs a restart. Restart the service now?", true)
	if err != nil || !ok {
		return err
	}
	if err := s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"restart", "netscribe"}}); err != nil {
		return err
	}
	return s.waitHealthy(ctx)
}

func (s *session) serviceActive(ctx context.Context) bool {
	out, _ := s.env.Run(ctx, Command{Name: "systemctl", Args: []string{"is-active", "netscribe"}})
	return strings.TrimSpace(string(out)) == "active"
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "on", "yes", "y", "1":
		return true, nil
	case "false", "off", "no", "n", "0":
		return false, nil
	}
	return false, fmt.Errorf("%q is not on or off", v)
}

// node turns text from a person into the node of a setting. The final check is the load
// of the whole config, this only makes the right YAML type.
func node(st setting, values []string) (*yaml.Node, error) {
	one := strings.Join(values, " ")
	switch st.kind {
	case kBool:
		b, err := parseBool(one)
		if err != nil {
			return nil, err
		}
		return boolNode(b), nil
	case kInt:
		n, err := strconv.Atoi(strings.TrimSpace(one))
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%q is not a whole number of at least 0", one)
		}
		return scalarNode("!!int", strconv.Itoa(n)), nil
	case kDuration:
		d, err := time.ParseDuration(strings.TrimSpace(one))
		if err != nil || d < 0 {
			return nil, fmt.Errorf("%q is not a duration such as 30s, 15m or 12h", one)
		}
		return strNode(strings.TrimSpace(one)), nil
	case kEnum:
		for _, c := range st.choices {
			if c == strings.TrimSpace(one) {
				return strNode(c), nil
			}
		}
		return nil, fmt.Errorf("choose one of: %s", strings.Join(st.choices, ", "))
	case kPorts:
		list := splitList(strings.Join(values, ","))
		for _, v := range list {
			if n, err := strconv.Atoi(v); err != nil || n < 1 || n > 65535 {
				return nil, fmt.Errorf("%q is not a port from 1 to 65535", v)
			}
		}
		return listNode(list, true), nil
	case kList, kTargets:
		list := splitList(strings.Join(values, ","))
		return listNode(list, false), nil
	case kText:
		return strNode(strings.TrimSpace(one)), nil
	}
	return nil, errors.New("this key cannot be set from the command line: use netscribe config")
}

// ConfigShow prints the config with the secrets hidden.
func ConfigShow(env Env, o Options) error {
	s := newSession(env, o)
	doc, err := s.openConfig()
	if err != nil {
		return err
	}
	for _, path := range [][]string{p("snmp.communities")} {
		if n := doc.find(path); n != nil && n.Kind == yaml.SequenceNode {
			for _, c := range n.Content {
				c.Value, c.Tag, c.Style = "***", "!!str", 0
			}
		}
	}
	data, err := doc.bytes()
	if err != nil {
		return err
	}
	_, err = env.Out.Write(data)
	return err
}

// ConfigGet prints the value of one key.
func ConfigGet(env Env, o Options, key string) error {
	st, ok := findSetting(key)
	if key == "listen" {
		st, ok = setting{path: p("listen"), label: "Panel address", kind: kText, def: defaultListen}, true
	}
	if !ok {
		return unknownKey(key)
	}
	s := newSession(env, o)
	doc, err := s.openConfig()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(env.Out, display(doc, st))
	return err
}

func unknownKey(key string) error {
	var keys []string
	for _, g := range settingGroups {
		for _, it := range g.items {
			keys = append(keys, it.key())
		}
	}
	return fmt.Errorf("unknown key %q: the keys are listen, %s", key, strings.Join(keys, ", "))
}

// ConfigSet changes one key from the command line. Secrets are not accepted here, because
// an argument shows in the process list: netscribe config asks for them without echo.
func ConfigSet(ctx context.Context, env Env, o Options, key string, values []string) error {
	s := newSession(env, o)
	s.p.yes = true // nothing to ask: the only question is the restart, and the answer is yes
	doc, err := s.openConfig()
	if err != nil {
		return err
	}
	if key == "listen" {
		if len(values) != 1 {
			return errors.New("listen takes one address, such as 127.0.0.1:8080")
		}
		if err := config.ValidateListen(values[0]); err != nil {
			return err
		}
		if err := doc.set([]string{"listen"}, strNode(values[0])); err != nil {
			return err
		}
		return s.commit(ctx, doc, false)
	}
	st, ok := findSetting(key)
	if !ok {
		return unknownKey(key)
	}
	if st.kind == kSecrets {
		return fmt.Errorf("%s holds secrets: run netscribe config and choose it, the value is typed without echo", key)
	}
	if len(values) == 0 {
		return fmt.Errorf("%s needs a value", key)
	}
	n, err := node(st, values)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if err := doc.set(st.path, n); err != nil {
		return err
	}
	return s.commit(ctx, doc, st.hot)
}

// ConfigEdit opens the file in the editor of the person. The result is checked before it
// replaces the file, and the person may go back and fix it.
func ConfigEdit(ctx context.Context, env Env, o Options) error {
	s := newSession(env, o)
	current, err := s.env.ReadFile(o.ConfigPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", o.ConfigPath, err)
	}
	editor := firstNonEmpty(env.Getenv("VISUAL"), env.Getenv("EDITOR"))
	if editor == "" {
		for _, e := range []string{"nano", "vi"} {
			if _, err := env.LookPath(e); err == nil {
				editor = e
				break
			}
		}
	}
	if editor == "" {
		return errors.New("no editor found: set the EDITOR variable")
	}
	tmp := filepath.Join(filepath.Dir(o.ConfigPath), ".netscribe-edit.yaml")
	owner := s.ownerOfFile(o.ConfigPath)
	text := current
	for {
		if err := env.WriteFile(tmp, text, 0o600, owner); err != nil {
			return err
		}
		if _, err := env.Run(ctx, Command{Name: editor, Args: []string{tmp}, Interactive: true}); err != nil {
			_ = env.Remove(tmp) // the editor failed: the draft is of no use
			return fmt.Errorf("%s: %w", editor, err)
		}
		edited, err := env.ReadFile(tmp)
		_ = env.Remove(tmp) // the text is in memory now
		if err != nil {
			return err
		}
		if string(edited) == string(current) {
			s.ui.note("no change")
			return nil
		}
		verr := s.validate(edited)
		if verr == nil {
			if err := s.approve("Save your changes to "+o.ConfigPath+"? The old file is kept as .bak.", true); err != nil {
				return err
			}
			if verr = s.saveConfig(edited); verr == nil {
				s.ui.done("saved " + o.ConfigPath)
				s.ui.note("keys under scan, snmp, ssh, docker, history and export apply by themselves, the others need: sudo systemctl restart netscribe")
				return nil
			}
		} else {
			verr = fmt.Errorf("the config would not be valid, nothing was saved: %w", verr)
		}
		s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), verr)
		again, err := s.p.confirm("Edit the file again?", true)
		if err != nil || !again {
			return err
		}
		text = edited
	}
}

func firstNonEmpty(list ...string) string {
	for _, v := range list {
		if v != "" {
			return v
		}
	}
	return ""
}

// Configure is the interactive editor: groups of settings, each with its value, an
// explanation and a recommendation.
func Configure(ctx context.Context, env Env, o Options, version string) error {
	s := newSession(env, o)
	s.ui.banner("Netscribe settings", version)
	for {
		doc, err := s.openConfig()
		if err != nil {
			return err
		}
		s.ui.section("Settings of " + o.ConfigPath)
		labels := []string{"Panel access      " + s.ui.dim(accessSummary(doc))}
		for _, g := range settingGroups {
			labels = append(labels, fmt.Sprintf("%-17s %s", g.name, s.ui.dim(g.hint)))
		}
		labels = append(labels, "SSH hosts        "+s.ui.dim(fmt.Sprintf("%d set", count(doc.find(p("ssh.hosts"))))),
			"Edit the file in your editor", "Show the file, secrets hidden", "Restore the previous config", "Back")
		i, err := s.p.pick("What do you want to change?", labels, len(labels)-1)
		if err != nil {
			return err
		}
		switch {
		case i == 0:
			err = s.editAccess(ctx, doc)
		case i <= len(settingGroups):
			err = s.editGroup(ctx, settingGroups[i-1])
		case i == len(settingGroups)+1:
			err = s.editSSHHosts(ctx)
		case i == len(settingGroups)+2:
			err = ConfigEdit(ctx, env, o)
		case i == len(settingGroups)+3:
			err = ConfigShow(env, o)
		case i == len(settingGroups)+4:
			err = s.restoreBackup(ctx)
		default:
			return nil
		}
		if isExit(err) {
			return err
		}
		if errors.Is(err, errDeclined) {
			continue
		}
		if err != nil {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
		}
	}
}

func accessSummary(doc *yamlDoc) string {
	listen := value(doc.find(p("listen")))
	if listen == "" {
		listen = defaultListen
	}
	switch {
	case value(doc.find(p("tls.certFile"))) != "":
		return listen + ", TLS"
	case count(doc.find(p("trustedProxies"))) > 0:
		return listen + ", behind a proxy"
	}
	return listen
}

// editAccess changes the listen address together with the TLS or proxy setting that goes
// with it, so the two never disagree.
func (s *session) editAccess(ctx context.Context, doc *yamlDoc) error {
	current := value(doc.find(p("listen")))
	if current == "" {
		current = defaultListen
	}
	choice, err := s.pickListen(current, s.serviceActive(ctx))
	if err != nil {
		return err
	}
	if err := s.approve("Change the panel address from "+current+" to "+choice.Listen+"?", true); err != nil {
		return err
	}
	if err := doc.set(p("listen"), strNode(choice.Listen)); err != nil {
		return err
	}
	doc.remove(p("tls"))
	doc.remove(p("trustedProxies"))
	if choice.TLSCert != "" {
		if err := doc.set(p("tls.certFile"), strNode(choice.TLSCert)); err != nil {
			return err
		}
		if err := doc.set(p("tls.keyFile"), strNode(choice.TLSKey)); err != nil {
			return err
		}
	}
	if len(choice.Proxies) > 0 {
		if err := doc.set(p("trustedProxies"), listNode(choice.Proxies, false)); err != nil {
			return err
		}
	}
	return s.commit(ctx, doc, false)
}

func (s *session) editGroup(ctx context.Context, g group) error {
	for {
		doc, err := s.openConfig()
		if err != nil {
			return err
		}
		labels := make([]string, 0, len(g.items)+1)
		for _, it := range g.items {
			labels = append(labels, fmt.Sprintf("%-34s %s", it.label, s.ui.dim(display(doc, it))))
		}
		labels = append(labels, "Back")
		s.ui.section(g.name)
		i, err := s.p.pick("Which setting?", labels, len(labels)-1)
		if err != nil || i == len(g.items) {
			return err
		}
		if err := s.editSetting(ctx, doc, g.items[i]); err != nil {
			if isExit(err) {
				return err
			}
			if errors.Is(err, errDeclined) {
				continue
			}
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
		}
	}
}

func (s *session) editSetting(ctx context.Context, doc *yamlDoc, st setting) error {
	s.ui.note("%s", st.help)
	if !st.hot {
		s.ui.note("a change needs a restart of the service")
	}
	cur := doc.find(st.path)
	var n *yaml.Node
	switch st.kind {
	case kBool:
		on := value(cur) == "true" || (value(cur) == "" && st.def == "on")
		v, err := s.p.confirm(st.label+"?", on)
		if err != nil {
			return err
		}
		n = boolNode(v)
	case kEnum:
		def := 0
		for i, c := range st.choices {
			if c == value(cur) || (value(cur) == "" && c == st.def) {
				def = i
			}
		}
		i, err := s.p.pick(st.label, st.choices, def)
		if err != nil {
			return err
		}
		n = strNode(st.choices[i])
	case kTargets:
		targets, err := s.chooseTargets()
		if err != nil {
			return err
		}
		n = listNode(targets, false)
	case kSecrets:
		return s.editSecrets(ctx, doc, st)
	default:
		def := value(cur)
		text, err := s.p.ask(st.label+" (empty: back to the default)", def, func(v string) error {
			if v == "" {
				return nil
			}
			_, err := node(st, []string{v})
			return err
		})
		if err != nil {
			return err
		}
		if text == "" {
			if err := s.approve("Set "+st.label+" back to its default?", true); err != nil {
				return err
			}
			doc.remove(st.path)
			return s.commit(ctx, doc, st.hot)
		}
		if n, err = node(st, []string{text}); err != nil {
			return err
		}
	}
	if err := s.approve(fmt.Sprintf("Save %s: %s → %s?", st.label, display(doc, st), value(n)), true); err != nil {
		return err
	}
	if err := doc.set(st.path, n); err != nil {
		return err
	}
	if err := s.commit(ctx, doc, st.hot); err != nil {
		return err
	}
	if st.key() == "discovery.nmap" && value(n) == "true" {
		return s.syncDropIn(ctx)
	}
	return nil
}

func (s *session) editSecrets(ctx context.Context, doc *yamlDoc, st setting) error {
	i, err := s.p.pick(fmt.Sprintf("%d communities are set", count(doc.find(st.path))),
		[]string{"Add one", "Replace all with one", "Remove all", "Back"}, 3)
	if err != nil || i == 3 {
		return err
	}
	if i == 2 {
		if err := s.approve("Remove every community? Devices that need one stop answering.", false); err != nil {
			return err
		}
		doc.remove(st.path)
		return s.commit(ctx, doc, st.hot)
	}
	s.ui.printf("  %s Community string (not shown while you type): ", s.ui.paint("36", "?"))
	secret, err := s.env.ReadSecret()
	if err != nil {
		return err
	}
	if strings.TrimSpace(secret) == "" {
		return errors.New("a community cannot be empty")
	}
	if err := s.approve("Save the community? It is stored in the config, mode 0600.", true); err != nil {
		return err
	}
	var items []string
	if i == 0 {
		if n := doc.find(st.path); n != nil {
			for _, c := range n.Content {
				items = append(items, c.Value)
			}
		}
	}
	items = append(items, strings.TrimSpace(secret))
	if err := doc.set(st.path, listNode(items, false)); err != nil {
		return err
	}
	return s.commit(ctx, doc, st.hot)
}

// syncDropIn writes the systemd drop-in that the enabled features need, from the config
// on disk, and reloads systemd. It is the same drop-in that setup writes.
func (s *session) syncDropIn(ctx context.Context) error {
	if !hasSystemd(s.env) || s.cfg == nil {
		return nil
	}
	s.ans = Answers{Docker: s.cfg.Docker.Enabled, DockerSocket: s.cfg.Docker.Socket, Nmap: s.cfg.Discovery.Nmap}
	if acc, err := s.env.LookupUser(s.o.ServiceUser); err == nil {
		s.acc = &acc
	}
	s.dockerNeeds()
	drop := RenderDropIn(s.dockerGroup, s.ans.Nmap)
	if drop == "" {
		return nil
	}
	path := filepath.Join(s.o.UnitDir, "netscribe.service.d", dropInName)
	if old, _ := s.env.ReadFile(path); string(old) == drop {
		return nil
	}
	ok, err := s.p.confirm("Nmap needs raw-socket rights. Write the systemd drop-in that grants them?", true)
	if err != nil || !ok {
		return err
	}
	if err := s.doConfirmed("write "+path, func() error {
		if err := s.env.MkdirAll(filepath.Dir(path), 0o755, nil); err != nil { //nolint:gosec // systemd directories are readable by everyone
			return err
		}
		return s.env.WriteFile(path, []byte(drop), 0o644, nil) //nolint:gosec // a unit file is readable by everyone
	}); err != nil {
		return err
	}
	if err := s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"daemon-reload"}}); err != nil {
		return err
	}
	if s.serviceActive(ctx) {
		return s.runChecked(ctx, Command{Name: "systemctl", Args: []string{"restart", "netscribe"}})
	}
	return nil
}

// restoreBackup swaps the config with the .bak that the last save left, after the same
// check as any save. The file it replaces becomes the new .bak, so the swap can be undone.
func (s *session) restoreBackup(ctx context.Context) error {
	bak := s.o.ConfigPath + ".bak"
	old, err := s.env.ReadFile(bak)
	if err != nil {
		s.ui.note("there is no previous config: %s does not exist", bak)
		return nil
	}
	if err := s.approve("Replace the config with the previous one from "+bak+"? The config now in use becomes the .bak.", false); err != nil {
		return err
	}
	if err := s.saveConfig(old); err != nil {
		return err
	}
	s.ui.done("restored the previous config; the one replaced is " + filepath.Base(bak))
	return s.afterSave(ctx, false)
}

// editSSHHosts lists, adds and removes the hosts that the SSH collector reads.
func (s *session) editSSHHosts(ctx context.Context) error {
	path := p("ssh.hosts")
	for {
		doc, err := s.openConfig()
		if err != nil {
			return err
		}
		var labels []string
		if n := doc.find(path); n != nil {
			for _, h := range n.Content {
				labels = append(labels, value(child(h, "user"))+"@"+value(child(h, "address")))
			}
		}
		s.ui.section("SSH hosts")
		if len(labels) == 0 {
			s.ui.note("no host is set: the SSH collector reads nothing")
		}
		for _, l := range labels {
			s.ui.printf("      %s\n", l)
		}
		i, err := s.p.pick("SSH hosts", []string{"Add a host", "Remove a host", "Back"}, 2)
		if err != nil || i == 2 {
			return err
		}
		if i == 0 {
			err = s.addSSHHost(ctx, doc)
		} else {
			err = s.removeSSHHost(ctx, doc, labels)
		}
		if isExit(err) {
			return err
		}
		if err != nil && !errors.Is(err, errDeclined) {
			s.ui.printf("  %s %v\n", s.ui.paint("31", "✗"), err)
		}
	}
}

func (s *session) addSSHHost(ctx context.Context, doc *yamlDoc) error {
	s.ui.note("Use an account that can only read: the scan runs hostname, uname -sr, ip -o addr and ss -H -tln.")
	address, err := s.p.ask("Address of the host, with :port when it is not 22", "", func(v string) error {
		if _, err := netip.ParseAddr(v); err == nil {
			return nil
		}
		if _, err := netip.ParseAddrPort(v); err != nil {
			return errors.New("use an IP address, such as 192.168.1.10 or 192.168.1.10:2222")
		}
		return nil
	})
	if err != nil {
		return err
	}
	notEmpty := func(v string) error {
		if strings.TrimSpace(v) == "" {
			return errors.New("this cannot be empty")
		}
		return nil
	}
	user, err := s.p.ask("User to sign in as", "audit", notEmpty)
	if err != nil {
		return err
	}
	key, err := s.p.ask("Private key file (keys only, mode 0600, no passphrase)", "~/.ssh/netscribe_ed25519", notEmpty)
	if err != nil {
		return err
	}
	if err := s.approve(fmt.Sprintf("Add the SSH host %s@%s with the key %s?", user, address, key), true); err != nil {
		return err
	}
	host := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{
		strNode("address"), strNode(address), strNode("user"), strNode(user), strNode("keyFile"), strNode(key),
	}}
	seq := doc.find(p("ssh.hosts"))
	if seq == nil || seq.Kind != yaml.SequenceNode {
		if err := doc.set(p("ssh.hosts"), &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}); err != nil {
			return err
		}
		seq = doc.find(p("ssh.hosts"))
	}
	seq.Content = append(seq.Content, host)
	return s.commit(ctx, doc, true)
}

func (s *session) removeSSHHost(ctx context.Context, doc *yamlDoc, labels []string) error {
	if len(labels) == 0 {
		s.ui.note("no host to remove")
		return nil
	}
	i, err := s.p.pick("Which host?", append(append([]string{}, labels...), "Back"), len(labels))
	if err != nil || i == len(labels) {
		return err
	}
	if err := s.approve("Remove the SSH host "+labels[i]+"? It is not read any more.", false); err != nil {
		return err
	}
	seq := doc.find(p("ssh.hosts"))
	seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
	if len(seq.Content) == 0 {
		doc.remove(p("ssh.hosts"))
	}
	return s.commit(ctx, doc, true)
}
