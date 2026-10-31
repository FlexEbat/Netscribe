package setup

import (
	"bufio"
	"bytes"
	"context"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSys is a machine for the tests: the files live in a temporary directory, and
// everything that would change the real system is recorded instead.
type fakeSys struct {
	t        *testing.T
	uid      int
	systemd  bool
	exe      string
	bins     map[string]bool // programs found by LookPath
	users    map[string]Account
	ifaces   []NetIface
	sockets  map[string]SocketInfo
	ping     string
	listen   error
	health   error
	active   string
	userList string
	cmds     []Command
	copied   [][2]string
	out      *bytes.Buffer
	secrets  []string
	vars     map[string]string
	clock    time.Time
	proc     map[string]string // contents of /proc files
	logLines string
}

func newFake(t *testing.T) *fakeSys {
	t.Helper()
	return &fakeSys{
		t:       t,
		systemd: true,
		bins:    map[string]bool{"useradd": true, "apt-get": true},
		users:   map[string]Account{},
		sockets: map[string]SocketInfo{},
		ping:    "1\t0\n",
		active:  "inactive",
		out:     &bytes.Buffer{},
		vars:    map[string]string{},
		clock:   time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
		proc:    map[string]string{},
		ifaces: []NetIface{
			{Name: "lo", Up: true, Loopback: true, Addrs: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/8")}},
			{Name: "eth0", Up: true, Addrs: []netip.Prefix{netip.MustParsePrefix("192.168.1.20/24")}},
			{Name: "docker0", Up: true, Addrs: []netip.Prefix{netip.MustParsePrefix("172.17.0.1/16")}},
		},
	}
}

// opts returns options whose paths are inside the temporary directory.
func (f *fakeSys) opts() Options {
	root := f.t.TempDir()
	o := DefaultOptions()
	o.ConfigPath = filepath.Join(root, "etc", "netscribe", "netscribe.yaml")
	o.StateDir = filepath.Join(root, "var", "lib", "netscribe")
	o.BinPath = filepath.Join(root, "usr", "local", "bin", "netscribe")
	o.UnitDir = filepath.Join(root, "etc", "systemd", "system")
	o.SysctlDir = filepath.Join(root, "etc", "sysctl.d")
	o.HealthTimeout = 50e6 // 50 ms
	return o
}

func (f *fakeSys) addUser(name string) Account {
	a := Account{Name: name, UID: uint32(os.Getuid()), GID: uint32(os.Getgid())} //nolint:gosec // ids of the test process
	f.users[name] = a
	return a
}

func (f *fakeSys) env(in string) Env {
	f.t.Helper()
	exe := f.exe
	if exe == "" {
		exe = filepath.Join(f.t.TempDir(), "netscribe")
	}
	return Env{
		In:          newReader(in),
		Out:         f.out,
		Interactive: true,
		GOOS:        "linux",
		GOARCH:      "amd64",
		Now: func() time.Time {
			f.clock = f.clock.Add(7 * time.Millisecond)
			return f.clock
		},
		Getenv: func(k string) string { return f.vars[k] },
		Remove: os.Remove,
		ReadSecret: func() (string, error) {
			if len(f.secrets) == 0 {
				return "", errInput
			}
			v := f.secrets[0]
			f.secrets = f.secrets[1:]
			return v, nil
		},
		Getuid:     func() int { return f.uid },
		Executable: func() (string, error) { return exe, nil },
		ReadFile: func(p string) ([]byte, error) {
			if p == pingRange {
				return []byte(f.ping), nil
			}
			if v, ok := f.proc[p]; ok {
				return []byte(v), nil
			}
			return os.ReadFile(p) //nolint:gosec // test paths
		},
		ReadDir: os.ReadDir,
		Lstat:   os.Lstat,
		Stat: func(p string) (fs.FileInfo, error) {
			if p == "/run/systemd/system" {
				if f.systemd {
					return os.Stat(os.TempDir())
				}
				return nil, fs.ErrNotExist
			}
			return os.Stat(p)
		},
		FreeBytes: func(string) (uint64, error) { return 10 << 30, nil },
		LookPath: func(n string) (string, error) {
			if f.bins[n] {
				return "/usr/bin/" + n, nil
			}
			return "", fs.ErrNotExist
		},
		Interfaces: func() ([]NetIface, error) { return f.ifaces, nil },
		LookupUser: func(n string) (Account, error) {
			if a, ok := f.users[n]; ok {
				return a, nil
			}
			return Account{}, fs.ErrNotExist
		},
		StatSocket: func(p string) (SocketInfo, error) {
			if s, ok := f.sockets[p]; ok {
				return s, nil
			}
			return SocketInfo{}, fs.ErrNotExist
		},
		CanListen: func(string) error { return f.listen },
		Health:    func(context.Context, string) error { return f.health },
		Run:       f.run,
		WriteFile: func(p string, d []byte, m fs.FileMode, _ *Account) error { return writeFileAtomic(p, d, m, nil) },
		MkdirAll: func(p string, m fs.FileMode, _ *Account) error {
			return os.MkdirAll(p, m) //nolint:gosec // test paths
		},
		CopyExecutable: func(src, dst string) error {
			f.copied = append(f.copied, [2]string{src, dst})
			if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
				return err
			}
			return os.WriteFile(dst, []byte("binary"), 0o600)
		},
	}
}

func (f *fakeSys) run(_ context.Context, c Command) ([]byte, error) {
	f.cmds = append(f.cmds, c)
	switch c.Name {
	case "useradd":
		f.addUser(c.Args[len(c.Args)-1])
	case "journalctl":
		return []byte(f.logLines), nil
	case "systemctl":
		switch c.Args[0] {
		case "show":
			return []byte("ActiveState=" + f.active + "\nSubState=running\nMainPID=4242\nNRestarts=0\nActiveEnterTimestamp=Sat 2026-10-24 11:00:00 UTC\nUnitFileState=enabled\n"), nil
		case "is-active":
			return []byte(f.active + "\n"), nil
		case "is-enabled":
			return []byte("enabled\n"), nil
		case "enable", "restart":
			f.active = "active"
		}
	}
	if len(c.Args) >= 2 && c.Args[0] == "user" {
		switch c.Args[1] {
		case "list":
			return []byte(f.userList), nil
		case "add":
			f.userList = "USERNAME  ROLE  STATUS  LAST LOGIN\n" + c.Args[2] + "  admin  enabled  never\n"
		}
	}
	return nil, nil
}

// ran lists the commands that were run, each as one line.
func (f *fakeSys) ran() string {
	var b strings.Builder
	for _, c := range f.cmds {
		b.WriteString(c.Name + " " + strings.Join(c.Args, " ") + "\n")
	}
	return b.String()
}

func newReader(s string) *bufio.Reader { return bufio.NewReader(strings.NewReader(s)) }
