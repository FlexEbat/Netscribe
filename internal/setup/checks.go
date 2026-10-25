package setup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
)

// Options say where the install lives and what the person asked for.
type Options struct {
	ConfigPath  string
	StateDir    string
	BinPath     string
	UnitDir     string
	SysctlDir   string
	ServiceUser string

	Yes            bool
	DryRun         bool
	NoService      bool
	Admin          string
	PasswordStdin  bool
	Targets        []string
	Listen         string
	TLSCert        string
	TLSKey         string
	TrustedProxies []string
	// PlainHTTP accepts a listen address beyond loopback without TLS or a proxy.
	PlainHTTP bool
	// Once makes watch print one sample and stop. WatchEvery is the time between samples.
	Once       bool
	WatchEvery time.Duration
	// WatchCount stops watch after that many samples. Zero means no limit.
	WatchCount int
	// HealthTimeout is how long the setup waits for the panel to answer after the start.
	HealthTimeout time.Duration
}

// DefaultOptions are the paths of the packaged install, the same ones install.sh uses.
func DefaultOptions() Options {
	return Options{
		ConfigPath:  "/etc/netscribe/netscribe.yaml",
		StateDir:    "/var/lib/netscribe",
		BinPath:     "/usr/local/bin/netscribe",
		UnitDir:     "/etc/systemd/system",
		SysctlDir:   "/etc/sysctl.d",
		ServiceUser: "netscribe",
		Listen:      "127.0.0.1:8080",

		HealthTimeout: 20 * time.Second,
		WatchEvery:    2 * time.Second,
	}
}

const (
	areaSystem  = "System"
	areaInstall = "Install"
	areaConfig  = "Config"
	areaNetwork = "Network"
	areaFeature = "Features"
	areaService = "Service"

	minFreeBytes = 256 << 20
	pingRange    = "/proc/sys/net/ipv4/ping_group_range"
)

func finding(area string, l Level, title, detail, fix string) Finding {
	return Finding{Area: area, Level: l, Title: title, Detail: detail, Fix: fix}
}

// Diagnose runs every check and returns the findings in the order of the install.
func Diagnose(ctx context.Context, env Env, o Options) []Finding {
	out := checkSystem(env, o)
	out = append(out, checkInstall(env, o)...)
	cfg, cf := checkConfig(env, o)
	out = append(out, cf...)
	if cfg == nil {
		return out
	}
	active, sf := checkService(ctx, env, o, *cfg)
	out = append(out, sf...)
	out = append(out, checkNetwork(env, o, *cfg, active)...)
	out = append(out, checkFeatures(env, o, *cfg)...)
	out = append(out, checkAccounts(ctx, env, o)...)
	return out
}

func hasSystemd(env Env) bool {
	_, err := env.Stat("/run/systemd/system")
	return err == nil
}

// existingParent returns the closest directory of path that exists.
func existingParent(env Env, path string) string {
	for {
		if _, err := env.Stat(path); err == nil {
			return path
		}
		next := filepath.Dir(path)
		if next == path {
			return path
		}
		path = next
	}
}

func checkSystem(env Env, o Options) []Finding {
	var out []Finding
	if env.GOOS != "linux" {
		return append(out, finding(areaSystem, LevelFail, "Netscribe runs on Linux only", "this system is "+env.GOOS, ""))
	}
	switch env.GOARCH {
	case "amd64", "arm64":
		out = append(out, finding(areaSystem, LevelOK, "Linux "+env.GOARCH, "", ""))
	default:
		out = append(out, finding(areaSystem, LevelWarn, "No release binary for "+env.GOARCH, "this build was made by hand", ""))
	}
	if env.Getuid() == 0 {
		out = append(out, finding(areaSystem, LevelOK, "Running as root", "", ""))
	} else {
		out = append(out, finding(areaSystem, LevelWarn, "Not running as root",
			"the setup changes system files, and the checks cannot read a config that only the service account may read",
			"run it again with sudo"))
	}
	if hasSystemd(env) {
		out = append(out, finding(areaSystem, LevelOK, "systemd is running", "", ""))
	} else {
		out = append(out, finding(areaSystem, LevelInfo, "systemd is not running",
			"the setup installs no service and no unit",
			"start netscribe serve --config "+o.ConfigPath+" with your init system"))
	}
	if free, err := env.FreeBytes(existingParent(env, o.StateDir)); err == nil {
		title := fmt.Sprintf("%d MiB free for the data", free>>20)
		if free < minFreeBytes {
			out = append(out, finding(areaSystem, LevelWarn, title, "the database and the scan history grow over time", "free some space or move the state directory"))
		} else {
			out = append(out, finding(areaSystem, LevelOK, title, "", ""))
		}
	}
	return out
}

func checkInstall(env Env, o Options) []Finding {
	var out []Finding
	exe, _ := env.Executable()
	switch exe {
	case o.BinPath:
		out = append(out, finding(areaInstall, LevelOK, "The netscribe command is at "+o.BinPath, "", ""))
	default:
		if _, err := env.Stat(o.BinPath); err == nil {
			out = append(out, finding(areaInstall, LevelInfo, "The netscribe command at "+o.BinPath+" is another copy", "this one runs from "+exe, ""))
		} else {
			out = append(out, finding(areaInstall, LevelWarn, "The netscribe command is not registered", "this binary runs from "+exe,
				"netscribe setup copies it to "+o.BinPath))
		}
	}
	acc, err := env.LookupUser(o.ServiceUser)
	if err != nil {
		out = append(out, finding(areaInstall, LevelWarn, "No service account "+o.ServiceUser, "the panel should not run as root", "netscribe setup creates it"))
	} else {
		out = append(out, finding(areaInstall, LevelOK, fmt.Sprintf("Service account %s (uid %d)", acc.Name, acc.UID), "", ""))
	}
	info, err := env.Stat(o.StateDir)
	switch {
	case err != nil:
		out = append(out, finding(areaInstall, LevelWarn, "No state directory "+o.StateDir, "the database lives there", "netscribe setup creates it"))
	case info.Mode().Perm()&0o007 != 0:
		out = append(out, finding(areaInstall, LevelWarn, "The state directory is readable by every user", "the database holds password hashes and scan data",
			"chmod 750 "+o.StateDir))
	default:
		out = append(out, finding(areaInstall, LevelOK, "State directory "+o.StateDir, "", ""))
	}
	return out
}

// checkConfig returns the config when it loads, and nil when it does not exist or is invalid.
func checkConfig(env Env, o Options) (*config.Config, []Finding) {
	info, err := env.Stat(o.ConfigPath)
	if err != nil {
		return nil, []Finding{finding(areaConfig, LevelWarn, "No config at "+o.ConfigPath, "", "netscribe setup writes one")}
	}
	var out []Finding
	if acc, aerr := env.LookupUser(o.ServiceUser); aerr == nil {
		if uid, _, ok := ownerOf(info); ok && uid != acc.UID && uid != 0 {
			out = append(out, finding(areaConfig, LevelFail, "The service account cannot read the config",
				fmt.Sprintf("the file belongs to uid %d and has mode %04o", uid, info.Mode().Perm()),
				fmt.Sprintf("chown %s:%s %s", o.ServiceUser, o.ServiceUser, o.ConfigPath)))
		} else if ok && uid == 0 && acc.UID != 0 {
			out = append(out, finding(areaConfig, LevelFail, "The service account cannot read the config",
				"root owns the file, and its mode is "+fmt.Sprintf("%04o", info.Mode().Perm()),
				fmt.Sprintf("chown %s:%s %s", o.ServiceUser, o.ServiceUser, o.ConfigPath)))
		}
	}
	cfg, err := config.Load(o.ConfigPath, false)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return nil, append(out, finding(areaConfig, LevelWarn, "Cannot read the config as this user", "the file is private to the service account", "run it again with sudo"))
	case err != nil:
		fix := "edit " + o.ConfigPath + " as the service account: sudo -u " + o.ServiceUser + " ${EDITOR:-vi} " + o.ConfigPath
		if strings.Contains(err.Error(), "wider than 0600") {
			fix = "chmod 600 " + o.ConfigPath
		}
		return nil, append(out, finding(areaConfig, LevelFail, "The config is not valid", err.Error(), fix))
	}
	out = append(out, finding(areaConfig, LevelOK, "The config is valid", o.ConfigPath, ""))
	return &cfg, out
}

// healthURL is the address of the health check of the panel, as seen from this host.
func healthURL(cfg config.Config) string {
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return ""
	}
	if ip, err := netip.ParseAddr(host); host == "" || err == nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	scheme := "http"
	if cfg.TLS.CertFile != "" {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, port) + "/healthz"
}

func checkService(ctx context.Context, env Env, o Options, cfg config.Config) (active bool, out []Finding) {
	if !hasSystemd(env) {
		return false, nil
	}
	if _, err := env.Stat(filepath.Join(o.UnitDir, "netscribe.service")); err != nil {
		return false, []Finding{finding(areaService, LevelWarn, "The systemd unit is not installed", "", "netscribe setup installs it")}
	}
	state, _ := env.Run(ctx, Command{Name: "systemctl", Args: []string{"is-active", "netscribe"}})
	if strings.TrimSpace(string(state)) != "active" {
		return false, []Finding{finding(areaService, LevelWarn, "The service is not running", "state: "+strings.TrimSpace(string(state)),
			"sudo systemctl enable --now netscribe")}
	}
	if enabled, _ := env.Run(ctx, Command{Name: "systemctl", Args: []string{"is-enabled", "netscribe"}}); strings.TrimSpace(string(enabled)) != "enabled" {
		out = append(out, finding(areaService, LevelInfo, "The service does not start at boot", "", "sudo systemctl enable netscribe"))
	}
	url := healthURL(cfg)
	if err := env.Health(ctx, url); err != nil {
		return true, append(out, finding(areaService, LevelFail, "The service runs but does not answer", err.Error(),
			"journalctl -u netscribe -n 50 --no-pager"))
	}
	return true, append(out, finding(areaService, LevelOK, "The panel answers at "+strings.TrimSuffix(url, "/healthz"), "", ""))
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func checkNetwork(env Env, o Options, cfg config.Config, active bool) []Finding {
	var out []Finding
	host, _, _ := net.SplitHostPort(cfg.Listen)
	switch {
	case isLoopback(host):
		out = append(out, finding(areaNetwork, LevelOK, "The panel listens on "+cfg.Listen+", this host only",
			"reach it from elsewhere through a TLS reverse proxy or ssh -L", ""))
	case cfg.TLS.CertFile == "" && len(cfg.TrustedProxies) == 0:
		out = append(out, finding(areaNetwork, LevelWarn, "The panel listens on "+cfg.Listen+" without TLS",
			"passwords and session cookies cross the network in clear text",
			"set tls.certFile and tls.keyFile, or put a TLS reverse proxy in front and list it in trustedProxies"))
	default:
		out = append(out, finding(areaNetwork, LevelOK, "The panel listens on "+cfg.Listen+" behind TLS", "", ""))
	}
	if !active {
		if err := env.CanListen(cfg.Listen); err != nil {
			out = append(out, finding(areaNetwork, LevelWarn, "Cannot bind "+cfg.Listen, err.Error(),
				"another program holds the port: sudo ss -ltnp, then change listen in the config"))
		}
	}

	local, err := env.Interfaces()
	if err != nil {
		return out
	}
	var nets []netip.Prefix
	for _, ifc := range local {
		if ifc.Loopback || !ifc.Up || isContainerIface(ifc.Name) {
			continue
		}
		for _, a := range ifc.Addrs {
			if a.Addr().Is4() {
				nets = append(nets, a.Masked())
			}
		}
	}
	reached := 0
	for _, t := range cfg.Scan.Targets {
		p, err := config.ParseTarget(t, cfg.AllowPublicTargets)
		if err != nil {
			continue
		}
		if p.Bits() <= 16 {
			out = append(out, finding(areaNetwork, LevelInfo, "Target "+t+" holds up to 65 534 addresses", "a scan of it takes long", ""))
		}
		for _, n := range nets {
			if overlaps(p, n) {
				reached++
				break
			}
		}
	}
	if len(cfg.Scan.Targets) > 0 && reached == 0 && !cfg.Discovery.ProbeRoutedTargets {
		fix := "set scan.targets in the config"
		if c := Candidates(local); len(c) > 0 {
			fix += ", for example [" + c[0].Prefix.String() + "]"
		}
		out = append(out, finding(areaNetwork, LevelWarn, "No scan target is a network of this host",
			"ARP discovery sees only the networks the host is connected to", fix))
	} else if reached > 0 {
		out = append(out, finding(areaNetwork, LevelOK, fmt.Sprintf("%d of %d scan targets are networks of this host", reached, len(cfg.Scan.Targets)), "", ""))
	}
	return append(out, checkICMP(env, o)...)
}

// pingAllowed reports whether the group may open ICMP echo sockets, and whether the
// setting is readable. A range whose start is above its end allows nobody.
func pingAllowed(env Env, gid uint32) (allowed, disabled, known bool) {
	data, err := env.ReadFile(pingRange)
	if err != nil {
		return false, false, false
	}
	f := strings.Fields(string(data))
	if len(f) != 2 {
		return false, false, false
	}
	lo, err1 := strconv.ParseInt(f[0], 10, 64)
	hi, err2 := strconv.ParseInt(f[1], 10, 64)
	if err1 != nil || err2 != nil {
		return false, false, false
	}
	return lo <= int64(gid) && int64(gid) <= hi, lo > hi, true
}

func checkICMP(env Env, o Options) []Finding {
	acc, err := env.LookupUser(o.ServiceUser)
	if err != nil {
		return nil
	}
	allowed, _, known := pingAllowed(env, acc.GID)
	switch {
	case !known:
		return nil
	case allowed:
		return []Finding{finding(areaNetwork, LevelOK, "The service group may send ICMP echo", "", "")}
	default:
		return []Finding{finding(areaNetwork, LevelWarn, "The service group may not send ICMP echo",
			"host discovery falls back to TCP probes, so hosts that drop TCP stay unseen",
			fmt.Sprintf("sudo sysctl -w net.ipv4.ping_group_range=\"%d %d\"\nnetscribe setup makes the change permanent", acc.GID, acc.GID))}
	}
}

// unitText returns the unit and its drop-ins as one text, to look for a setting in it.
func unitText(env Env, o Options) string {
	var b strings.Builder
	if data, err := env.ReadFile(filepath.Join(o.UnitDir, "netscribe.service")); err == nil {
		b.Write(data)
	}
	dir := filepath.Join(o.UnitDir, "netscribe.service.d")
	entries, err := env.ReadDir(dir)
	if err != nil {
		return b.String()
	}
	for _, e := range entries {
		if data, err := env.ReadFile(filepath.Join(dir, e.Name())); err == nil {
			b.WriteString("\n")
			b.Write(data)
		}
	}
	return b.String()
}

// hasSetting reports whether an uncommented line of the unit text starts with key and
// holds value.
func hasSetting(text, key, value string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, key+"=") && strings.Contains(line, value) {
			return true
		}
	}
	return false
}

var chromeNames = []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "chrome"}

func findChrome(env Env, configured string) (string, bool) {
	if configured != "" {
		_, err := env.Stat(configured)
		return configured, err == nil
	}
	for _, n := range chromeNames {
		if p, err := env.LookPath(n); err == nil {
			return p, true
		}
	}
	return "", false
}

func checkFeatures(env Env, o Options, cfg config.Config) []Finding {
	var out []Finding
	text := unitText(env, o)

	nmap := cfg.Discovery.NmapPath
	found := false
	if nmap != "" {
		_, err := env.Stat(nmap)
		found = err == nil
	} else if p, err := env.LookPath("nmap"); err == nil {
		nmap, found = p, true
	}
	switch {
	case cfg.Discovery.Nmap && !found:
		out = append(out, finding(areaFeature, LevelWarn, "discovery.nmap is on but nmap was not found", "devices stay untyped", "install nmap, or set discovery.nmap: false"))
	case cfg.Discovery.Nmap && !hasSetting(text, "AmbientCapabilities", "CAP_NET_RAW"):
		out = append(out, finding(areaFeature, LevelWarn, "Nmap runs without raw-socket rights",
			"the operating system detection needs CAP_NET_RAW", "netscribe setup writes a systemd drop-in with the rights"))
	case cfg.Discovery.Nmap:
		out = append(out, finding(areaFeature, LevelOK, "Nmap is on: "+nmap, "", ""))
	case found:
		out = append(out, finding(areaFeature, LevelInfo, "nmap is installed and discovery.nmap is off",
			"Nmap can type devices that nothing else could", "set discovery.nmap: true"))
	}

	if p, ok := findChrome(env, cfg.Export.ChromePath); ok {
		out = append(out, finding(areaFeature, LevelOK, "PDF export has a browser: "+p, "", ""))
	} else {
		out = append(out, finding(areaFeature, LevelInfo, "PDF export has no browser", "Markdown, HTML and draw.io export work without one",
			"install chromium, or set export.chromePath"))
	}

	if cfg.Docker.Enabled {
		out = append(out, checkDocker(env, o, cfg, text)...)
	}
	if len(cfg.SNMP.Communities) == 1 && cfg.SNMP.Communities[0] == "public" {
		out = append(out, finding(areaFeature, LevelInfo, "SNMP uses the community public", "a well-known community is read by anyone who can reach the device",
			"use a read-only community and restrict it by ACL on the device"))
	}
	return out
}

func checkDocker(env Env, o Options, cfg config.Config, unit string) []Finding {
	sock, err := env.StatSocket(cfg.Docker.Socket)
	if err != nil {
		return []Finding{finding(areaFeature, LevelWarn, "docker.enabled is on but there is no socket at "+cfg.Docker.Socket,
			"the container view stays empty", "start Docker, or set docker.enabled: false")}
	}
	if sock.Mode.Perm()&0o006 == 0o006 {
		return []Finding{finding(areaFeature, LevelOK, "The Docker socket is open to every user", "", "")}
	}
	if acc, err := env.LookupUser(o.ServiceUser); err == nil {
		member := acc.GID == sock.GID
		for _, g := range acc.Groups {
			member = member || g == sock.GID
		}
		if member {
			return []Finding{finding(areaFeature, LevelOK, "The service account can use the Docker socket", "", "")}
		}
	}
	if sock.Group != "" && hasSetting(unit, "SupplementaryGroups", sock.Group) {
		return []Finding{finding(areaFeature, LevelOK, "The unit adds the group "+sock.Group+" for the Docker socket", "", "")}
	}
	return []Finding{finding(areaFeature, LevelWarn, "The service account cannot use the Docker socket",
		fmt.Sprintf("the socket belongs to group %q", sock.Group), "netscribe setup writes a systemd drop-in with SupplementaryGroups")}
}

// countAccounts runs "netscribe user list" as the service account and counts the rows.
func countAccounts(ctx context.Context, env Env, o Options) (int, bool) {
	acc, err := env.LookupUser(o.ServiceUser)
	if err != nil || env.Getuid() != 0 {
		return 0, false
	}
	if _, err := env.Stat(o.BinPath); err != nil {
		return 0, false
	}
	out, err := env.Run(ctx, Command{Name: o.BinPath, Args: []string{"user", "list", "--config", o.ConfigPath}, As: &acc})
	if err != nil {
		return 0, false
	}
	rows := 0
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(l) != "" {
			rows++
		}
	}
	return max(rows-1, 0), true // the first row is the header
}

func checkAccounts(ctx context.Context, env Env, o Options) []Finding {
	n, ok := countAccounts(ctx, env, o)
	switch {
	case !ok:
		return nil
	case n == 0:
		return []Finding{finding(areaConfig, LevelWarn, "No account can sign in", "the panel starts with an empty account table",
			"sudo -u "+o.ServiceUser+" "+o.BinPath+" user add NAME --role admin --config "+o.ConfigPath)}
	default:
		return []Finding{finding(areaConfig, LevelOK, fmt.Sprintf("%d accounts exist", n), "", "")}
	}
}
