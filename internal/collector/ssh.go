package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	sshConcurrency    = 4 // hosts at one moment, section 9.6 of the spec
	sshConnectTimeout = 5 * time.Second
	sshCommandTimeout = 10 * time.Second
	sshMaxOutput      = 1 << 20 // bytes per command
	sshDefaultPort    = "22"
	maxLineRunes      = 255
)

// sshCommand names one of the commands the collector may run. The collector can only
// ask for a value of this type, so no text from the config or from the network ever
// reaches a remote shell.
type sshCommand int

const (
	cmdHostname sshCommand = iota
	cmdUname
	cmdIPAddr
	cmdListeners
)

// sshCommands is the complete list, in the order the collector runs it.
func sshCommands() []sshCommand {
	return []sshCommand{cmdHostname, cmdUname, cmdIPAddr, cmdListeners}
}

// text returns the command line, or "" for a value outside the list.
func (c sshCommand) text() string {
	switch c {
	case cmdHostname:
		return "hostname"
	case cmdUname:
		return "uname -sr"
	case cmdIPAddr:
		return "ip -o addr"
	case cmdListeners:
		return "ss -H -tln"
	}
	return ""
}

// sshEndpoint is everything needed to open a session to one host.
type sshEndpoint struct {
	Addr       string // host:port
	User       string
	KeyFile    string
	KnownHosts string
}

// sshRunner opens sessions. The real one talks SSH, tests use a fake.
type sshRunner interface {
	Connect(ctx context.Context, e sshEndpoint) (sshConn, error)
}

// sshConn runs the fixed commands on one host.
type sshConn interface {
	Run(ctx context.Context, c sshCommand) (string, error)
	Close() error
}

// SSH reads hostname, kernel, addresses and listening TCP ports from the hosts of
// ssh.hosts. It only logs in with a key, only runs the commands of sshCommands and
// skips a host it cannot verify.
type SSH struct {
	run sshRunner
	log *slog.Logger
}

// NewSSH returns a collector that connects over the network.
func NewSSH(log *slog.Logger) *SSH { return &SSH{run: netRunner{}, log: log} }

func (c *SSH) Name() string { return "ssh" }

// Collect asks every host of ssh.hosts. A host that cannot be reached or verified costs
// a warning, never the scan. The log names the address and the reason, never a key path.
func (c *SSH) Collect(ctx context.Context, in Input) (Result, error) {
	if len(in.Config.SSH.Hosts) == 0 {
		return Result{}, nil
	}
	live := map[string]model.Device{}
	for _, d := range in.Devices {
		if d.IP != "" {
			live[d.IP] = d
		}
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		res Result
		sem = make(chan struct{}, sshConcurrency)
	)
loop:
	for _, h := range in.Config.SSH.Hosts {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(h config.SSHHost) {
			defer wg.Done()
			defer func() { <-sem }()
			one, ok := c.collectHost(ctx, h, in.Config, live)
			if !ok {
				return
			}
			mu.Lock()
			res.Devices = append(res.Devices, one.Devices...)
			res.Interfaces = append(res.Interfaces, one.Interfaces...)
			res.Services = append(res.Services, one.Services...)
			res.ServicesPolled = append(res.ServicesPolled, one.ServicesPolled...)
			mu.Unlock()
		}(h)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	sortResult(&res)
	sort.SliceStable(res.Services, func(i, j int) bool {
		a, b := res.Services[i], res.Services[j]
		if a.Device.IP != b.Device.IP {
			return a.Device.IP < b.Device.IP
		}
		return a.Port < b.Port
	})
	return res, nil
}

func (c *SSH) collectHost(ctx context.Context, h config.SSHHost, cfg config.Config, live map[string]model.Device) (Result, bool) {
	skip := func(reason any) (Result, bool) {
		if ctx.Err() == nil {
			c.log.Warn("ssh host skipped", "host", h.Address, "reason", reason)
		}
		return Result{}, false
	}
	addr, ip, err := sshAddress(h.Address)
	if err != nil {
		return skip(err)
	}
	if !cfg.AllowPublicTargets && !ip.IsPrivate() {
		return skip("address is outside private ranges")
	}
	if strings.TrimSpace(h.User) == "" {
		return skip("user is empty")
	}

	key := model.DeviceKey{IP: ip.String()}
	if d, ok := live[key.IP]; ok {
		key.MAC = d.MAC
	}
	conn, err := c.run.Connect(ctx, sshEndpoint{Addr: addr, User: h.User, KeyFile: string(h.KeyFile), KnownHosts: cfg.SSH.KnownHosts})
	if err != nil {
		return skip(err)
	}
	defer func() { _ = conn.Close() }() // read-only session, nothing to flush

	out := map[sshCommand]string{}
	for _, cmd := range sshCommands() {
		if ctx.Err() != nil {
			return Result{}, false
		}
		text, err := conn.Run(ctx, cmd)
		if err != nil {
			c.log.Warn("ssh command failed", "host", h.Address, "command", cmd.text(), "reason", err)
			continue
		}
		out[cmd] = text
	}
	if len(out) == 0 {
		return Result{}, false
	}
	return buildSSHResult(key, out), true
}

// buildSSHResult turns the command outputs into facts. Listening services are reported
// as polled only when the listing command worked, so a failed command never closes
// services.
func buildSSHResult(key model.DeviceKey, out map[sshCommand]string) Result {
	res := Result{Devices: []model.DeviceInput{{
		Key:         key,
		Hostname:    cleanHostname(firstLine(out[cmdHostname])),
		Description: cleanLine(firstLine(out[cmdUname])),
		Source:      "ssh",
	}}}
	res.Interfaces = parseIPAddr(key, out[cmdIPAddr])
	if listing, ok := out[cmdListeners]; ok {
		res.Services = parseListeners(key, listing)
		res.ServicesPolled = []model.DeviceKey{key}
	}
	return res
}

// sshAddress splits "ip" or "ip:port" into a dial address. Names are not resolved: the
// scan sticks to the addresses the owner wrote down.
func sshAddress(s string) (string, netip.Addr, error) {
	s = strings.TrimSpace(s)
	host, port := s, sshDefaultPort
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, port = h, p
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return "", netip.Addr{}, errors.New("address is not an IP address")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", netip.Addr{}, errors.New("port is out of range")
	}
	return net.JoinHostPort(ip.String(), port), ip, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// cleanLine removes control characters and cuts the text to maxLineRunes characters.
// Everything a host prints is untrusted.
func cleanLine(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if model.UnsafeRune(r) {
			return -1
		}
		return r
	}, s))
	if r := []rune(s); len(r) > maxLineRunes {
		return string(r[:maxLineRunes])
	}
	return s
}

// parseIPAddr reads `ip -o addr`: one line per address, "2: eth0    inet 10.0.0.2/24 ...".
// Loopback and IPv6 link-local addresses say nothing about the network and are dropped.
// The output has no link state, so an interface that carries an address counts as up.
func parseIPAddr(dev model.DeviceKey, out string) []model.InterfaceInput {
	var res []model.InterfaceInput
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || (f[2] != "inet" && f[2] != "inet6") {
			continue
		}
		name, _, _ := strings.Cut(f[1], "@") // "eth0.10@eth0" names its parent
		p, err := netip.ParsePrefix(f[3])
		if err != nil || name == "" {
			continue
		}
		a := p.Addr()
		if a.IsLoopback() || (a.Is6() && a.IsLinkLocalUnicast()) {
			continue
		}
		res = append(res, model.InterfaceInput{Device: dev, Name: cleanLine(name), IP: a.String(), Up: true})
		if len(res) >= maxInterfaces {
			break
		}
	}
	return res
}

// parseListeners reads `ss -H -tln`: "LISTEN 0 128 0.0.0.0:22 0.0.0.0:*". A port bound
// to loopback only is not a service of the network and is dropped, a port listening on
// both IPv4 and IPv6 counts once.
func parseListeners(dev model.DeviceKey, out string) []model.ServiceInput {
	seen := map[int]struct{}{}
	var res []model.ServiceInput
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "LISTEN" {
			continue
		}
		i := strings.LastIndexByte(f[3], ':')
		if i < 0 {
			continue
		}
		port, err := strconv.Atoi(f[3][i+1:])
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		host, _, _ := strings.Cut(strings.Trim(f[3][:i], "[]"), "%") // "127.0.0.53%lo"
		if a, err := netip.ParseAddr(host); err == nil && a.IsLoopback() {
			continue
		}
		if _, dup := seen[port]; dup {
			continue
		}
		seen[port] = struct{}{}
		res = append(res, model.ServiceInput{Device: dev, Port: port, Proto: "tcp", Name: portNameOf(port)})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Port < res[j].Port })
	return res
}

// The messages never carry a path: a log line must not leak where the keys live.
var (
	errKeyUnreadable = errors.New("key file is missing or unreadable")
	errKeyPerms      = errors.New("key file permissions are wider than 0600")
	errKeyParse      = errors.New("key file is not a private key without passphrase")
	errKnownHosts    = errors.New("known_hosts file is missing or unreadable")
	errSSHCommand    = errors.New("command is not in the list")
	errSSHOutput     = errors.New("output is longer than 1 MiB")
)

// expandHome turns a leading "~/" into the home directory of the service user.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// loadSigner reads a private key. A file that group or others can touch is refused
// before it is read, and so is a key that needs a passphrase: there is no password login.
func loadSigner(path string) (ssh.Signer, error) {
	f, err := os.Open(expandHome(path)) //nolint:gosec // the path comes from the config file, which only the owner can write
	if err != nil {
		return nil, errKeyUnreadable
	}
	defer func() { _ = f.Close() }() // opened read-only

	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errKeyUnreadable
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, errKeyPerms
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return nil, errKeyUnreadable
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil, errKeyParse
	}
	return signer, nil
}

// hostKeyPolicy loads known_hosts. The policy is strict: a host that is not listed and a
// host whose key changed are both refused, nothing is added to the file.
func hostKeyPolicy(path string) (ssh.HostKeyCallback, error) {
	check, err := knownhosts.New(expandHome(path))
	if err != nil {
		return nil, errKnownHosts
	}
	return check, nil
}

// hostKeyAlgorithms lists the signature algorithms for the key types known_hosts holds.
func hostKeyAlgorithms(want []knownhosts.KnownKey) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(a string) {
		if _, ok := seen[a]; !ok {
			seen[a] = struct{}{}
			out = append(out, a)
		}
	}
	for _, k := range want {
		if k.Key == nil {
			continue
		}
		if t := k.Key.Type(); t == ssh.KeyAlgoRSA {
			add(ssh.KeyAlgoRSASHA512)
			add(ssh.KeyAlgoRSASHA256)
		} else {
			add(t)
		}
	}
	return out
}

// netRunner connects over TCP with the x/crypto SSH client.
type netRunner struct{}

func (netRunner) Connect(ctx context.Context, e sshEndpoint) (sshConn, error) {
	signer, err := loadSigner(e.KeyFile)
	if err != nil {
		return nil, err
	}
	check, err := hostKeyPolicy(e.KnownHosts)
	if err != nil {
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            e.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: check,
		Timeout:         sshConnectTimeout,
	}
	client, err := dialSSH(ctx, e.Addr, cfg)
	var ke *knownhosts.KeyError
	if errors.As(err, &ke) && len(ke.Want) > 0 {
		// The host presented a key type known_hosts has no entry for, while it lists
		// others. Ask for the listed types once, as OpenSSH does, and check again: a key
		// that really changed fails the second time too.
		cfg.HostKeyAlgorithms = hostKeyAlgorithms(ke.Want)
		client, err = dialSSH(ctx, e.Addr, cfg)
	}
	if err != nil {
		return nil, err
	}
	return &netConn{client: client}, nil
}

// dialSSH opens the TCP connection and runs the handshake within sshConnectTimeout.
func dialSSH(ctx context.Context, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	d := net.Dialer{Timeout: sshConnectTimeout}
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	// The deadline bounds the handshake, the hook ends it when the scan is canceled.
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })
	if err := nc.SetDeadline(time.Now().Add(sshConnectTimeout)); err != nil {
		stop()
		_ = nc.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	sc, chans, reqs, err := ssh.NewClientConn(nc, addr, cfg)
	stop()
	if err != nil {
		_ = nc.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}
	if err := nc.SetDeadline(time.Time{}); err != nil {
		_ = sc.Close()
		return nil, fmt.Errorf("connect: %w", err)
	}
	return ssh.NewClient(sc, chans, reqs), nil
}

type netConn struct{ client *ssh.Client }

func (c *netConn) Close() error { return c.client.Close() }

// Run executes one command of the list in a fresh session without a terminal.
func (c *netConn) Run(ctx context.Context, cmd sshCommand) (string, error) {
	text := cmd.text()
	if text == "" {
		return "", errSSHCommand
	}
	sess, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("open session: %w", err)
	}
	// Closing ends the goroutine below when the command outlives its timeout.
	defer func() { _ = sess.Close() }()

	out := &limitedBuffer{max: sshMaxOutput}
	sess.Stdout = out
	sess.Stderr = io.Discard
	ctx, cancel := context.WithTimeout(ctx, sshCommandTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Run(text) }()
	select {
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("run command: %w", err)
		}
	case <-ctx.Done():
		return "", fmt.Errorf("run command: %w", ctx.Err())
	}
	if out.over {
		return "", errSSHOutput
	}
	return out.buf.String(), nil
}

// limitedBuffer keeps the first max bytes and notes that more arrived. It accepts all
// input so the remote command never blocks on a full channel.
type limitedBuffer struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.over {
		return len(p), nil
	}
	n := len(p)
	if room := b.max - b.buf.Len(); n > room {
		b.over = true
		p = p[:room]
	}
	b.buf.Write(p) // a bytes.Buffer write cannot fail
	// Report everything as taken: a short write would fail the copy.
	return n, nil
}
