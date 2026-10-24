// Package setup implements "netscribe setup" and "netscribe doctor": the guided install of
// the service and the checks that tell whether an install is healthy.
//
// Every effect on the machine goes through Env, so the tests run the whole flow against a
// fake system and no test touches /etc or starts a process.
package setup

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Level ranks a finding. A failure means the install does not work, a warning means it
// works but something is wrong or missing.
type Level int

const (
	LevelOK Level = iota
	LevelInfo
	LevelWarn
	LevelFail
)

// Finding is the result of one check.
type Finding struct {
	Area   string
	Level  Level
	Title  string
	Detail string // why, in one line
	Fix    string // what to do, shown for warnings and failures
}

// Account is a local user.
type Account struct {
	Name   string
	UID    uint32
	GID    uint32
	Groups []uint32 // the numeric group IDs the account belongs to
}

// NetIface is a network interface with its IPv4 and IPv6 addresses.
type NetIface struct {
	Name     string
	Up       bool
	Loopback bool
	Addrs    []netip.Prefix
}

// SocketInfo describes a Unix socket such as the Docker socket.
type SocketInfo struct {
	Mode  fs.FileMode
	GID   uint32
	Group string
}

// Command is a process to run.
type Command struct {
	Name string
	Args []string
	Env  []string // extra variables, added to the environment of this process
	As   *Account // run as this account, nil means the current one
	// Interactive connects the process to the terminal of this one, so that it can ask
	// for a password.
	Interactive bool
	Stdin       io.Reader // standard input of a process that is not interactive
}

// Env is the machine the setup runs on. NewEnv returns the real one.
type Env struct {
	In          *bufio.Reader
	Out         io.Writer
	Color       bool
	Interactive bool // standard input is a terminal

	GOOS, GOARCH string

	Getuid     func() int
	Executable func() (string, error)
	ReadFile   func(path string) ([]byte, error)
	ReadDir    func(path string) ([]fs.DirEntry, error)
	Stat       func(path string) (fs.FileInfo, error)
	FreeBytes  func(path string) (uint64, error)
	LookPath   func(name string) (string, error)
	Interfaces func() ([]NetIface, error)
	LookupUser func(name string) (Account, error)
	StatSocket func(path string) (SocketInfo, error)
	// CanListen reports whether the address can be bound now.
	CanListen func(addr string) error
	// Health asks the panel at url whether it is up.
	Health func(ctx context.Context, url string) error
	// Run runs a command and returns what it printed. An interactive command returns nothing.
	Run func(ctx context.Context, c Command) ([]byte, error)
	// WriteFile replaces a file atomically. A nil owner leaves the file to the current user.
	WriteFile func(path string, data []byte, mode fs.FileMode, owner *Account) error
	MkdirAll  func(path string, mode fs.FileMode, owner *Account) error
	// CopyExecutable copies a program to its place, replacing a running copy safely.
	CopyExecutable func(src, dst string) error
}

// NewEnv returns the real machine. in and out are the terminal of the caller.
func NewEnv(in io.Reader, out io.Writer, interactive, color bool) Env {
	return Env{
		In:             bufio.NewReader(in),
		Out:            out,
		Color:          color,
		Interactive:    interactive,
		GOOS:           runtime.GOOS,
		GOARCH:         runtime.GOARCH,
		Getuid:         os.Getuid,
		Executable:     os.Executable,
		ReadFile:       os.ReadFile,
		ReadDir:        os.ReadDir,
		Stat:           os.Stat,
		FreeBytes:      freeBytes,
		LookPath:       exec.LookPath,
		Interfaces:     interfaces,
		LookupUser:     lookupUser,
		StatSocket:     statSocket,
		CanListen:      canListen,
		Health:         health,
		Run:            run,
		WriteFile:      writeFileAtomic,
		MkdirAll:       mkdirAll,
		CopyExecutable: copyExecutable,
	}
}

func interfaces() ([]NetIface, error) {
	list, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]NetIface, 0, len(list))
	for _, ifc := range list {
		n := NetIface{Name: ifc.Name, Up: ifc.Flags&net.FlagUp != 0, Loopback: ifc.Flags&net.FlagLoopback != 0}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue // an interface that vanished while we looked
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP)
			if !ok {
				continue
			}
			ones, _ := ipn.Mask.Size()
			n.Addrs = append(n.Addrs, netip.PrefixFrom(ip.Unmap(), ones))
		}
		out = append(out, n)
	}
	return out, nil
}

func lookupUser(name string) (Account, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return Account{}, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return Account{}, err
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return Account{}, err
	}
	a := Account{Name: name, UID: uint32(uid), GID: uint32(gid)}
	ids, err := u.GroupIds()
	if err != nil {
		return a, nil // the groups are a hint for the Docker check, the account is real
	}
	for _, id := range ids {
		if n, err := strconv.ParseUint(id, 10, 32); err == nil {
			a.Groups = append(a.Groups, uint32(n))
		}
	}
	return a, nil
}

func statSocket(path string) (SocketInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return SocketInfo{}, err
	}
	s := SocketInfo{Mode: info.Mode()}
	if _, gid, ok := ownerOf(info); ok {
		s.GID = gid
		if g, err := user.LookupGroupId(strconv.FormatUint(uint64(gid), 10)); err == nil {
			s.Group = g.Name
		}
	}
	return s, nil
}

func canListen(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return l.Close()
}

func health(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// The panel may use a certificate that the host does not know. This request only asks
	// the local server whether it answers, and sends no credentials.
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}, //nolint:gosec // see above
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req) //nolint:gosec // the URL is built from the listen address of the config
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // a health probe: nothing to recover from a failed close
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func run(ctx context.Context, c Command) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.Name, c.Args...) //nolint:gosec // the setup runs fixed programs with arguments it built
	cmd.Env = append(os.Environ(), c.Env...)
	if c.As != nil {
		cmd.SysProcAttr = credential(*c.As)
	}
	if c.Interactive {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return nil, cmd.Run()
	}
	cmd.Stdin = c.Stdin
	out, err := cmd.CombinedOutput()
	return out, err
}

// writeFileAtomic writes a temporary file next to the target and renames it, so a reader
// such as the config watcher never sees half a file.
func writeFileAtomic(path string, data []byte, mode fs.FileMode, owner *Account) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func() { _ = os.Remove(tmp) } // the temporary file is garbage when an earlier step failed
	if _, err := f.Write(data); err != nil {
		_ = f.Close() // the write error is the one to report
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		cleanup()
		return err
	}
	if owner != nil {
		if err := os.Chown(tmp, int(owner.UID), int(owner.GID)); err != nil {
			cleanup()
			return err
		}
	}
	if err := os.Rename(tmp, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func mkdirAll(path string, mode fs.FileMode, owner *Account) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	if owner != nil {
		return os.Chown(path, int(owner.UID), int(owner.GID))
	}
	return nil
}

// copyExecutable replaces dst with a copy of src. The rename keeps a running copy of dst
// intact: the old file stays open until its processes exit.
func copyExecutable(src, dst string) error {
	data, err := os.ReadFile(src) //nolint:gosec // src is the path of this very program
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil { //nolint:gosec // a binary directory must be readable by everyone
		return err
	}
	return writeFileAtomic(dst, data, 0o755, nil) //nolint:gosec // a program must be executable
}
