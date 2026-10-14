package collector

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	fixtureIPAddr = `1: lo    inet 127.0.0.1/8 scope host lo\       valid_lft forever preferred_lft forever
1: lo    inet6 ::1/128 scope host \       valid_lft forever preferred_lft forever
2: eth0    inet 192.168.1.10/24 brd 192.168.1.255 scope global eth0\       valid_lft forever preferred_lft forever
2: eth0    inet6 fd00::10/64 scope global \       valid_lft forever preferred_lft forever
2: eth0    inet6 fe80::1/64 scope link \       valid_lft forever preferred_lft forever
3: br0.10@eth0    inet 10.0.10.1/24 scope global br0.10\       valid_lft forever preferred_lft forever
`
	fixtureSS = `LISTEN 0      4096      127.0.0.54:53         0.0.0.0:*
LISTEN 0      128          0.0.0.0:22         0.0.0.0:*
LISTEN 0      128             [::]:22            [::]:*
LISTEN 0      511                *:80               *:*
LISTEN 0      4096  127.0.0.53%lo:53         0.0.0.0:*
LISTEN 0      244        127.0.0.1:5432       0.0.0.0:*
LISTEN 0      4096           [::1]:631            [::]:*
`
)

func webHost() fakeSSHHost {
	return fakeSSHHost{out: map[sshCommand]string{
		cmdHostname:  "web01\n",
		cmdUname:     "Linux 6.8.0-45-generic\n",
		cmdIPAddr:    fixtureIPAddr,
		cmdListeners: fixtureSS,
	}}
}

// fakeSSHRunner answers from a table and records what it was asked to run.
type fakeSSHRunner struct {
	mu        sync.Mutex
	hosts     map[string]fakeSSHHost // dial address -> host
	ran       []sshCommand
	endpoints []sshEndpoint
}

type fakeSSHHost struct {
	out        map[sshCommand]string
	failCmd    map[sshCommand]error
	connectErr error
}

func (f *fakeSSHRunner) Connect(_ context.Context, e sshEndpoint) (sshConn, error) {
	f.mu.Lock()
	f.endpoints = append(f.endpoints, e)
	h, ok := f.hosts[e.Addr]
	f.mu.Unlock()
	if !ok {
		return nil, errors.New("connect: i/o timeout")
	}
	if h.connectErr != nil {
		return nil, h.connectErr
	}
	return &fakeSSHConn{f: f, h: h}, nil
}

type fakeSSHConn struct {
	f *fakeSSHRunner
	h fakeSSHHost
}

func (c *fakeSSHConn) Run(_ context.Context, cmd sshCommand) (string, error) {
	c.f.mu.Lock()
	c.f.ran = append(c.f.ran, cmd)
	c.f.mu.Unlock()
	if err := c.h.failCmd[cmd]; err != nil {
		return "", err
	}
	return c.h.out[cmd], nil
}

func (c *fakeSSHConn) Close() error { return nil }

func sshInput(addrs ...string) Input {
	cfg := testConfig()
	cfg.SSH.KnownHosts = "known_hosts"
	for _, a := range addrs {
		cfg.SSH.Hosts = append(cfg.SSH.Hosts, config.SSHHost{Address: a, User: "audit", KeyFile: "/keys/audit"})
	}
	return Input{Devices: liveDevices("192.168.1.10"), Config: cfg}
}

func logTo(buf *bytes.Buffer) *slog.Logger { return slog.New(slog.NewTextHandler(buf, nil)) }

func TestSSHFillsHostFromCommandOutput(t *testing.T) {
	f := &fakeSSHRunner{hosts: map[string]fakeSSHHost{"192.168.1.10:22": webHost()}}
	c := &SSH{run: f, log: quietLogger()}

	res, err := c.Collect(context.Background(), sshInput("192.168.1.10"))
	if err != nil {
		t.Fatal(err)
	}
	key := model.DeviceKey{MAC: "aa:aa:aa:aa:aa:01", IP: "192.168.1.10"}
	if len(res.Devices) != 1 {
		t.Fatalf("devices = %+v", res.Devices)
	}
	d := res.Devices[0]
	if d.Key != key || d.Hostname != "web01" || d.Description != "Linux 6.8.0-45-generic" || d.Source != "ssh" {
		t.Errorf("device = %+v", d)
	}

	var ifaces []string
	for _, i := range res.Interfaces {
		if i.Device != key || !i.Up {
			t.Errorf("interface = %+v", i)
		}
		ifaces = append(ifaces, i.Name+"="+i.IP)
	}
	if got, want := fmt.Sprint(ifaces), "[eth0=192.168.1.10 eth0=fd00::10 br0.10=10.0.10.1]"; got != want {
		t.Errorf("interfaces = %s, want %s", got, want)
	}

	var svcs []string
	for _, s := range res.Services {
		svcs = append(svcs, fmt.Sprintf("%d/%s=%s", s.Port, s.Proto, s.Name))
	}
	if got, want := fmt.Sprint(svcs), "[22/tcp=ssh 80/tcp=http]"; got != want {
		t.Errorf("services = %s, want %s (loopback listeners and the IPv6 twin of 22 are not services)", got, want)
	}
	if len(res.ServicesPolled) != 1 || res.ServicesPolled[0] != key {
		t.Errorf("polled = %+v", res.ServicesPolled)
	}
}

func TestSSHHostOutsideDiscoveryGetsAnIPOnlyKey(t *testing.T) {
	f := &fakeSSHRunner{hosts: map[string]fakeSSHHost{"192.168.5.7:2222": webHost()}}
	c := &SSH{run: f, log: quietLogger()}

	res, err := c.Collect(context.Background(), sshInput("192.168.5.7:2222"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 1 || res.Devices[0].Key != (model.DeviceKey{IP: "192.168.5.7"}) {
		t.Errorf("devices = %+v", res.Devices)
	}
}

func TestSSHSkipsUnreachableHostAndKeepsTheRest(t *testing.T) {
	var logs bytes.Buffer
	f := &fakeSSHRunner{hosts: map[string]fakeSSHHost{
		"192.168.1.10:22": webHost(),
		"192.168.1.20:22": {connectErr: errors.New("handshake: knownhosts: key is unknown")},
		// 192.168.1.30 is not in the table: the dial times out
	}}
	c := &SSH{run: f, log: logTo(&logs)}

	res, err := c.Collect(context.Background(), sshInput("192.168.1.20", "192.168.1.30", "192.168.1.10"))
	if err != nil {
		t.Fatalf("a host that cannot be reached failed the scan: %v", err)
	}
	if len(res.Devices) != 1 || res.Devices[0].Key.IP != "192.168.1.10" {
		t.Errorf("devices = %+v, want only the reachable host", res.Devices)
	}
	for _, want := range []string{"192.168.1.20", "key is unknown", "192.168.1.30", "i/o timeout"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, logs.String())
		}
	}
}

func TestSSHRefusesAddressesItShouldNotTouch(t *testing.T) {
	f := &fakeSSHRunner{hosts: map[string]fakeSSHHost{"8.8.8.8:22": webHost()}}
	c := &SSH{run: f, log: quietLogger()}

	in := sshInput("8.8.8.8", "web.lan", "192.168.1.9:99999")
	in.Config.SSH.Hosts = append(in.Config.SSH.Hosts, config.SSHHost{Address: "192.168.1.8", KeyFile: "/keys/audit"}) // no user
	res, err := c.Collect(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 0 || len(f.endpoints) != 0 {
		t.Errorf("devices = %+v, connections = %+v, want none", res.Devices, f.endpoints)
	}

	in = sshInput("8.8.8.8")
	in.Config.AllowPublicTargets = true
	res, err = c.Collect(context.Background(), in)
	if err != nil || len(res.Devices) != 1 {
		t.Errorf("allowPublicTargets: devices = %+v, err = %v", res.Devices, err)
	}
}

func TestSSHKeepsWhatWorkedWhenACommandFails(t *testing.T) {
	h := webHost()
	h.failCmd = map[sshCommand]error{cmdListeners: errors.New("exit status 127")}
	f := &fakeSSHRunner{hosts: map[string]fakeSSHHost{"192.168.1.10:22": h}}
	c := &SSH{run: f, log: quietLogger()}

	res, err := c.Collect(context.Background(), sshInput("192.168.1.10"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Devices) != 1 || res.Devices[0].Hostname != "web01" || len(res.Interfaces) == 0 {
		t.Errorf("res = %+v", res)
	}
	if len(res.Services) != 0 || len(res.ServicesPolled) != 0 {
		t.Errorf("a failed listing must not close services: services = %+v, polled = %+v", res.Services, res.ServicesPolled)
	}
}

func TestSSHRunsOnlyTheFixedCommands(t *testing.T) {
	f := &fakeSSHRunner{hosts: map[string]fakeSSHHost{"192.168.1.10:22": webHost(), "192.168.1.11:22": webHost()}}
	c := &SSH{run: f, log: quietLogger()}
	if _, err := c.Collect(context.Background(), sshInput("192.168.1.10", "192.168.1.11")); err != nil {
		t.Fatal(err)
	}

	allowed := map[string]bool{"hostname": true, "uname -sr": true, "ip -o addr": true, "ss -H -tln": true}
	counts := map[string]int{}
	for _, cmd := range f.ran {
		if !allowed[cmd.text()] {
			t.Errorf("ran %q, which is not in the list", cmd.text())
		}
		counts[cmd.text()]++
	}
	for text := range allowed {
		if counts[text] != 2 {
			t.Errorf("%q ran %d times, want once per host", text, counts[text])
		}
	}
}

func TestSSHCommandTexts(t *testing.T) {
	want := map[sshCommand]string{
		cmdHostname: "hostname", cmdUname: "uname -sr", cmdIPAddr: "ip -o addr", cmdListeners: "ss -H -tln",
	}
	if len(sshCommands()) != len(want) {
		t.Fatalf("commands = %v", sshCommands())
	}
	for _, c := range sshCommands() {
		if c.text() != want[c] {
			t.Errorf("command %d = %q, want %q", c, c.text(), want[c])
		}
	}
	if got := sshCommand(99).text(); got != "" {
		t.Errorf("a value outside the list has the text %q", got)
	}
}

func TestSSHRunsFourHostsAtATime(t *testing.T) {
	hosts := map[string]fakeSSHHost{}
	var addrs []string
	for i := 1; i <= 12; i++ {
		ip := fmt.Sprintf("192.168.1.%d", 100+i)
		hosts[ip+":22"] = webHost()
		addrs = append(addrs, ip)
	}
	var now, peak atomic.Int32
	c := &SSH{run: &slowRunner{inner: &fakeSSHRunner{hosts: hosts}, now: &now, peak: &peak}, log: quietLogger()}
	if _, err := c.Collect(context.Background(), sshInput(addrs...)); err != nil {
		t.Fatal(err)
	}
	if p := peak.Load(); p > sshConcurrency || p < 2 {
		t.Errorf("peak concurrency = %d, want 2 to %d", p, sshConcurrency)
	}
}

type slowRunner struct {
	inner     *fakeSSHRunner
	now, peak *atomic.Int32
}

func (r *slowRunner) Connect(ctx context.Context, e sshEndpoint) (sshConn, error) {
	n := r.now.Add(1)
	defer r.now.Add(-1)
	for {
		p := r.peak.Load()
		if n <= p || r.peak.CompareAndSwap(p, n) {
			break
		}
	}
	time.Sleep(20 * time.Millisecond)
	return r.inner.Connect(ctx, e)
}

func TestSSHStopsOnCancel(t *testing.T) {
	f := &fakeSSHRunner{hosts: map[string]fakeSSHHost{"192.168.1.10:22": webHost()}}
	c := &SSH{run: f, log: quietLogger()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Collect(ctx, sshInput("192.168.1.10")); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestSSHWithoutHostsDoesNothing(t *testing.T) {
	f := &fakeSSHRunner{}
	res, err := (&SSH{run: f, log: quietLogger()}).Collect(context.Background(), sshInput())
	if err != nil || len(res.Devices) != 0 || len(f.endpoints) != 0 {
		t.Errorf("res = %+v, err = %v, connections = %v", res, err, f.endpoints)
	}
}

func TestParseIPAddr(t *testing.T) {
	dev := model.DeviceKey{IP: "192.168.1.10"}
	got := parseIPAddr(dev, fixtureIPAddr+"garbage line\n2: eth1    inet nonsense scope global eth1\n")
	if len(got) != 3 {
		t.Fatalf("interfaces = %+v, want eth0 twice and br0.10, no loopback, no link-local", got)
	}
	if got[2].Name != "br0.10" {
		t.Errorf("vlan name = %q, the parent after @ must be cut", got[2].Name)
	}
}

func TestParseListeners(t *testing.T) {
	got := parseListeners(model.DeviceKey{IP: "192.168.1.10"}, fixtureSS+"LISTEN 0 1 0.0.0.0:99999 0.0.0.0:*\nESTAB 0 0 1.2.3.4:5 6.7.8.9:10\nshort\n")
	if len(got) != 2 || got[0].Port != 22 || got[1].Port != 80 {
		t.Errorf("services = %+v", got)
	}
}

func TestSSHAddress(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.1.10":      "192.168.1.10:22",
		" 192.168.1.10 ":    "192.168.1.10:22",
		"192.168.1.10:2222": "192.168.1.10:2222",
		"[fd00::1]:22":      "[fd00::1]:22",
	} {
		got, _, err := sshAddress(in)
		if err != nil || got != want {
			t.Errorf("sshAddress(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "host.lan", "192.168.1.10:0", "192.168.1.10:x", "192.168.1.10:70000"} {
		if _, _, err := sshAddress(bad); err == nil {
			t.Errorf("sshAddress(%q) accepted", bad)
		}
	}
}

func TestBuildSSHResultCleansWhatTheHostPrints(t *testing.T) {
	res := buildSSHResult(model.DeviceKey{IP: "192.168.1.10"}, map[sshCommand]string{
		cmdHostname: "web\x1b[31m01\n",
		cmdUname:    "Linux\x00 " + strings.Repeat("x", 400) + "\n",
	})
	d := res.Devices[0]
	if strings.ContainsAny(d.Hostname, "\x1b\x00") || strings.ContainsAny(d.Description, "\x1b\x00") {
		t.Errorf("control characters survived: %q %q", d.Hostname, d.Description)
	}
	if n := len([]rune(d.Description)); n > maxLineRunes {
		t.Errorf("description has %d characters", n)
	}
}

func TestLimitedBufferKeepsTheStartAndNotesOverflow(t *testing.T) {
	b := &limitedBuffer{max: 10}
	if n, err := b.Write([]byte("0123456")); n != 7 || err != nil || b.over {
		t.Fatalf("first write: n = %d, err = %v, over = %v", n, err, b.over)
	}
	if n, err := b.Write([]byte("789abc")); n != 6 || err != nil || !b.over {
		t.Fatalf("second write: n = %d, err = %v, over = %v, all input must be accepted", n, err, b.over)
	}
	if n, err := b.Write([]byte("more")); n != 4 || err != nil {
		t.Fatalf("third write: n = %d, err = %v", n, err)
	}
	if got := b.buf.String(); got != "0123456789" {
		t.Errorf("kept %q", got)
	}
}

func writeTestKey(t *testing.T, mode os.FileMode) string {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "netscribe_secret_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadSignerAcceptsOwnerOnlyKeys(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400} {
		if _, err := loadSigner(writeTestKey(t, mode)); err != nil {
			t.Errorf("mode %04o: %v", mode, err)
		}
	}
}

func TestLoadSignerRefusesWideKeysWithoutNamingThePath(t *testing.T) {
	for _, mode := range []os.FileMode{0o640, 0o644, 0o604, 0o660} {
		path := writeTestKey(t, mode)
		_, err := loadSigner(path)
		if !errors.Is(err, errKeyPerms) {
			t.Errorf("mode %04o: err = %v, want the permissions error", mode, err)
			continue
		}
		if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), filepath.Base(path)) {
			t.Errorf("the error names the key file: %v", err)
		}
	}
}

func TestLoadSignerRefusesMissingAndBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := loadSigner(filepath.Join(dir, "absent")); !errors.Is(err, errKeyUnreadable) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := loadSigner(dir); !errors.Is(err, errKeyUnreadable) {
		t.Errorf("directory: %v", err)
	}
	junk := filepath.Join(dir, "junk")
	if err := os.WriteFile(junk, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSigner(junk); !errors.Is(err, errKeyParse) {
		t.Errorf("junk: %v", err)
	}
}

func TestRealRunnerRefusesAWideKeyBeforeDialing(t *testing.T) {
	var logs bytes.Buffer
	path := writeTestKey(t, 0o644)
	in := sshInput("192.168.1.10")
	in.Config.SSH.Hosts[0].KeyFile = config.Secret(path)
	c := &SSH{run: netRunner{}, log: logTo(&logs)}

	res, err := c.Collect(context.Background(), in)
	if err != nil {
		t.Fatalf("a refused key failed the scan: %v", err)
	}
	if len(res.Devices) != 0 {
		t.Errorf("devices = %+v, want the host skipped", res.Devices)
	}
	if !strings.Contains(logs.String(), "192.168.1.10") || !strings.Contains(logs.String(), "wider than 0600") {
		t.Errorf("the warning is missing:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), filepath.Base(path)) || strings.Contains(logs.String(), filepath.Dir(path)) {
		t.Errorf("the log names the key path:\n%s", logs.String())
	}
}

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestHostKeyPolicyIsStrict(t *testing.T) {
	known, other := testPublicKey(t), testPublicKey(t)
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(knownhosts.Line([]string{"192.168.1.10"}, known)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	check, err := hostKeyPolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	addr := func(ip string) net.Addr { return &net.TCPAddr{IP: net.ParseIP(ip), Port: 22} }

	if err := check("192.168.1.10:22", addr("192.168.1.10"), known); err != nil {
		t.Errorf("a listed host with its key was refused: %v", err)
	}
	if err := check("192.168.1.10:22", addr("192.168.1.10"), other); err == nil {
		t.Error("a host with a changed key was accepted")
	}
	if err := check("192.168.1.11:22", addr("192.168.1.11"), known); err == nil {
		t.Error("an unknown host was accepted")
	}
	after, err := os.ReadFile(path) //nolint:gosec // a path inside the test directory
	if err != nil || bytes.Count(after, []byte("\n")) != 1 {
		t.Errorf("known_hosts changed: %q, %v", after, err)
	}
}

func TestHostKeyPolicyNeedsTheFile(t *testing.T) {
	if _, err := hostKeyPolicy(filepath.Join(t.TempDir(), "absent")); !errors.Is(err, errKnownHosts) {
		t.Errorf("err = %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got, want := expandHome("~/.ssh/known_hosts"), filepath.Join(home, ".ssh", "known_hosts"); got != want {
		t.Errorf("expandHome = %q, want %q", got, want)
	}
	if got := expandHome("/etc/ssh/known_hosts"); got != "/etc/ssh/known_hosts" {
		t.Errorf("an absolute path changed to %q", got)
	}
}

func TestHostKeyAlgorithmsFollowKnownHosts(t *testing.T) {
	ed := testPublicKey(t) // ssh-ed25519
	got := hostKeyAlgorithms([]knownhosts.KnownKey{{Key: ed}, {Key: ed}, {}})
	if len(got) != 1 || got[0] != ssh.KeyAlgoED25519 {
		t.Errorf("algorithms = %v, want ssh-ed25519 once", got)
	}
	if got := hostKeyAlgorithms(nil); len(got) != 0 {
		t.Errorf("algorithms = %v, want none", got)
	}
}
