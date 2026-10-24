package setup

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomicReplacesTheFileWithTheModeAndLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "netscribe.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil { //nolint:gosec // the old file is open on purpose
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("new"), 0o600, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 || readFile(t, path) != "new" {
		t.Errorf("mode %v, content %q, err %v", info.Mode().Perm(), readFile(t, path), err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("a temporary file stayed behind: %v", entries)
	}
	if err := writeFileAtomic(filepath.Join(dir, "missing", "x"), nil, 0o600, nil); err == nil {
		t.Error("a missing directory must be an error")
	}
}

func TestMkdirAllSetsTheMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b")
	if err := mkdirAll(path, 0o750, nil); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o750 {
		t.Errorf("mode of the new directory: %v, %v", info, err)
	}
}

func TestCopyExecutable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "bin", "netscribe")
	if err := copyExecutable(src, dst); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(dst); info.Mode().Perm() != 0o755 || readFile(t, dst) != "#!/bin/sh\n" {
		t.Errorf("the copy: %v %q", info.Mode().Perm(), readFile(t, dst))
	}
	if err := copyExecutable(filepath.Join(dir, "nope"), dst); err == nil {
		t.Error("a missing source must be an error")
	}
}

func TestCanListen(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback")
	}
	defer func() { _ = l.Close() }()
	if err := canListen(l.Addr().String()); err == nil {
		t.Error("a bound port must be reported as busy")
	}
	free := l.Addr().String()
	_ = l.Close()
	if err := canListen(free); err != nil {
		t.Errorf("a free port: %v", err)
	}
}

func TestHealth(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer ok.Close()
	if err := health(context.Background(), ok.URL); err != nil {
		t.Errorf("a healthy panel: %v", err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer bad.Close()
	if err := health(context.Background(), bad.URL); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("a failing panel: %v", err)
	}
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer tlsSrv.Close()
	if err := health(context.Background(), tlsSrv.URL); err != nil {
		t.Errorf("a panel with a self-signed certificate must count as up: %v", err)
	}
}

func TestRun(t *testing.T) {
	out, err := run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo $NS_TEST; cat"}, Env: []string{"NS_TEST=hello"}, Stdin: strings.NewReader("piped")})
	if err != nil || string(out) != "hello\npiped" {
		t.Errorf("output %q, err %v", out, err)
	}
	out, err = run(context.Background(), Command{Name: "sh", Args: []string{"-c", "echo oops; exit 3"}})
	if err == nil || !strings.Contains(string(out), "oops") {
		t.Errorf("a failing command keeps its output: %q, %v", out, err)
	}
}

func TestRealEnvReadsTheMachine(t *testing.T) {
	var out bytes.Buffer
	env := NewEnv(strings.NewReader(""), &out, false, false)
	if env.GOOS != runtime.GOOS || env.Getuid() != os.Getuid() {
		t.Error("the real environment must describe this machine")
	}
	if list, err := env.Interfaces(); err != nil || len(list) == 0 {
		t.Errorf("interfaces: %v, %v", list, err)
	}
	if free, err := env.FreeBytes(t.TempDir()); err != nil || free == 0 {
		t.Errorf("free bytes: %d, %v", free, err)
	}
	if _, err := env.LookupUser("no-such-user-netscribe"); err == nil {
		t.Error("an unknown user must be an error")
	}
	if _, err := env.StatSocket(filepath.Join(t.TempDir(), "none.sock")); err == nil {
		t.Error("a missing socket must be an error")
	}
}

func TestSplitList(t *testing.T) {
	got := splitList("10.0.0.0/24, 10.0.1.0/24  10.0.2.0/24,")
	if len(got) != 3 || got[2] != "10.0.2.0/24" {
		t.Errorf("%v", got)
	}
}

func TestCreateAccountWithAdduserOnAlpine(t *testing.T) {
	f := newFake(t)
	f.bins = map[string]bool{"adduser": true}
	s := newSession(f.env(""), f.opts())
	if err := s.createAccount(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ran := f.ran(); !strings.Contains(ran, "addgroup -S netscribe") || !strings.Contains(ran, "adduser -S -D -H") {
		t.Errorf("commands:\n%s", ran)
	}
	f.bins = map[string]bool{}
	if err := s.createAccount(context.Background()); err == nil {
		t.Error("no tool to create the account must be an error")
	}
}

func TestAdminRetriesAfterAWeakPassword(t *testing.T) {
	f := newFake(t)
	o := f.opts()
	f.addUser(o.ServiceUser)
	if err := os.MkdirAll(filepath.Dir(o.BinPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(o.BinPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.userList = "USERNAME  ROLE  STATUS  LAST LOGIN\n"
	env := f.env("alice\ny\n")
	attempts := 0
	run := env.Run
	env.Run = func(ctx context.Context, c Command) ([]byte, error) {
		if len(c.Args) > 1 && c.Args[1] == "add" {
			attempts++
			if attempts == 1 {
				return nil, os.ErrInvalid
			}
		}
		return run(ctx, c)
	}
	s := newSession(env, o)
	s.acc = &Account{Name: o.ServiceUser}
	if err := s.admin(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Errorf("attempts %d", attempts)
	}
}
