package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

func TestInsecureListen(t *testing.T) {
	tests := []struct {
		name string
		edit func(*config.Config)
		want bool
	}{
		{"loopback v4", func(c *config.Config) { c.Listen = "127.0.0.1:8080" }, false},
		{"loopback v6", func(c *config.Config) { c.Listen = "[::1]:8080" }, false},
		{"localhost", func(c *config.Config) { c.Listen = "localhost:8080" }, false},
		{"all interfaces", func(c *config.Config) { c.Listen = ":8080" }, true},
		{"lan address", func(c *config.Config) { c.Listen = "192.168.1.5:8080" }, true},
		{"unspecified v4", func(c *config.Config) { c.Listen = "0.0.0.0:8080" }, true},
		{"lan address with TLS", func(c *config.Config) {
			c.Listen = "0.0.0.0:8443"
			c.TLS = config.TLSConfig{CertFile: "c", KeyFile: "k"}
		}, false},
		{"lan address behind a proxy", func(c *config.Config) {
			c.Listen = "192.168.1.5:8080"
			c.TrustedProxies = []string{"192.168.1.1/32"}
		}, false},
		{"malformed address", func(c *config.Config) { c.Listen = "nonsense" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var c config.Config
			tt.edit(&c)
			if got := insecureListen(c); got != tt.want {
				t.Errorf("insecureListen(%q) = %v, want %v", c.Listen, got, tt.want)
			}
		})
	}
}

func TestNewServerLimits(t *testing.T) {
	srv := newServer(config.Config{Listen: "127.0.0.1:1"}, nil)
	if srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 30*time.Second ||
		srv.WriteTimeout != 60*time.Second || srv.IdleTimeout != 120*time.Second {
		t.Errorf("timeouts = %v %v %v %v", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.WriteTimeout, srv.IdleTimeout)
	}
	if srv.MaxHeaderBytes != 16<<10 {
		t.Errorf("MaxHeaderBytes = %d, want 16 KiB", srv.MaxHeaderBytes)
	}
	if srv.TLSConfig == nil || srv.TLSConfig.MinVersion < 0x0303 { // 0x0303 is TLS 1.2
		t.Errorf("TLS minimum version = %v, want TLS 1.2 or higher", srv.TLSConfig)
	}
}

func TestLoggerMasksSensitiveKeys(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf)
	l.Info("event",
		"password", "pw-value",
		"api_token", "tok-value",
		"Cookie", "cookie-value",
		"Authorization", "auth-value",
		"snmp_community", "comm-value",
		"clientSecret", "secret-value",
		"addr", "127.0.0.1:8080",
	)
	out := buf.String()
	for _, leaked := range []string{"pw-value", "tok-value", "cookie-value", "auth-value", "comm-value", "secret-value"} {
		if strings.Contains(out, leaked) {
			t.Errorf("log leaked %q: %s", leaked, out)
		}
	}
	if !strings.Contains(out, "127.0.0.1:8080") {
		t.Errorf("harmless attribute was masked: %s", out)
	}
}

func TestLoggerMasksSensitiveKeysInGroups(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf).Info("event", slog.Group("req", slog.String("authorization", "Bearer abc")))
	if strings.Contains(buf.String(), "Bearer abc") {
		t.Errorf("grouped attribute leaked: %s", buf.String())
	}
}

func TestRunExitCodes(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"no command", nil, 2, "usage:"},
		{"unknown command", []string{"frobnicate"}, 2, "unknown command"},
		{"help", []string{"help"}, 0, "usage:"},
		{"serve help", []string{"serve", "-h"}, 0, "-config"},
		{"serve with a bad flag", []string{"serve", "--nope"}, 1, "flag provided but not defined"},
		{"serve with a missing config", []string{"serve", "--config", "/nonexistent/netscribe.yaml"}, 1, "read config"},
		{"scan with a missing config", []string{"scan", "--config", "/nonexistent/netscribe.yaml"}, 1, "read config"},
		{"devices with a missing config", []string{"devices", "--config", "/nonexistent/netscribe.yaml"}, 1, "read config"},
		{"scan help", []string{"scan", "-h"}, 0, "-config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if got := run(tt.args, streams{in: strings.NewReader(""), out: io.Discard, err: &stderr}); got != tt.code {
				t.Errorf("exit code = %d, want %d (stderr: %s)", got, tt.code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.stderr)
			}
		})
	}
}

func TestScanTargets(t *testing.T) {
	cfg := config.Config{Scan: config.ScanConfig{Targets: []string{"192.168.1.0/24"}}}

	got, err := scanTargets(nil, cfg)
	if err != nil || len(got) != 1 || got[0].String() != "192.168.1.0/24" {
		t.Fatalf("config targets: %v, %v", got, err)
	}
	got, err = scanTargets([]string{"10.1.0.0/24", "10.2.0.0/24"}, cfg)
	if err != nil || len(got) != 2 || got[0].String() != "10.1.0.0/24" {
		t.Fatalf("arguments must replace the config targets: %v, %v", got, err)
	}

	_, err = scanTargets([]string{"8.8.8.8/32"}, cfg)
	if err == nil || !strings.Contains(err.Error(), "target is outside private ranges") {
		t.Errorf("public target = %v, want the private-range error", err)
	}
	cfg.AllowPublicTargets = true
	if _, err := scanTargets([]string{"8.8.8.8/32"}, cfg); err != nil {
		t.Errorf("public target with allowPublicTargets = %v", err)
	}
	if _, err := scanTargets([]string{"10.0.0.0/8"}, cfg); err == nil || !strings.Contains(err.Error(), "wider than /16") {
		t.Errorf("wide target = %v", err)
	}
	if _, err := scanTargets([]string{"192.168.1.0/24", "oops"}, cfg); err == nil || !strings.Contains(err.Error(), "oops") {
		t.Errorf("one bad argument must fail the whole scan and name the argument: %v", err)
	}
	if _, err := scanTargets(nil, config.Config{}); err == nil || !strings.Contains(err.Error(), "no scan targets") {
		t.Errorf("no targets = %v", err)
	}
}

func TestPrintable(t *testing.T) {
	for in, want := range map[string]string{
		"nas.lan":              "nas.lan",
		"Принтер":              "Принтер",
		"evil\x1b[2Jhost":      "evil?[2Jhost",
		"line\nbreak\ttab\x00": "line?break?tab?",
	} {
		if got := printable(in); got != want {
			t.Errorf("printable(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrintDevices(t *testing.T) {
	var buf bytes.Buffer
	err := printDevices(&buf, []model.Device{
		{IP: "192.168.1.1", MAC: "aa:bb:cc:00:00:01", Hostname: "router.lan", Online: true},
		{IP: "192.168.1.20", MAC: "aa:bb:cc:00:00:20", Hostname: "x\x1b[31m", Online: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("output:\n%s", buf.String())
	}
	for i, want := range [][]string{
		{"IP", "MAC", "HOSTNAME", "ONLINE"},
		{"192.168.1.1", "aa:bb:cc:00:00:01", "router.lan", "yes"},
		{"192.168.1.20", "aa:bb:cc:00:00:20", "x?[31m", "no"},
	} {
		got := strings.Fields(lines[i])
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("line %d = %v, want %v", i, got, want)
		}
	}
	if strings.Contains(buf.String(), "\x1b") {
		t.Error("an escape sequence reached the terminal output")
	}
}

func TestPrintDevicesEmptyHasHeaderOnly(t *testing.T) {
	var buf bytes.Buffer
	if err := printDevices(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Count(buf.String(), "\n") != 1 || !strings.HasPrefix(buf.String(), "IP") {
		t.Errorf("output = %q", buf.String())
	}
}

// cli runs the program against a fresh config and database in a temporary directory.
type cli struct {
	t   *testing.T
	cfg string
	db  string
}

func newCLI(t *testing.T) *cli {
	t.Helper()
	dir := t.TempDir()
	c := &cli{t: t, cfg: filepath.Join(dir, "netscribe.yaml"), db: filepath.Join(dir, "nb.db")}
	body := "database: " + c.db + "\nscan:\n  targets: [192.168.1.0/24]\nauth:\n  argon2:\n    memoryKiB: 19456\n    iterations: 2\n    parallelism: 1\n"
	if err := os.WriteFile(c.cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return c
}

// run executes one command line. stdin is the text piped to the process.
func (c *cli) run(stdin string, args ...string) (code int, stdout, stderr string) {
	c.t.Helper()
	var out, errOut bytes.Buffer
	args = append(args, "--config", c.cfg)
	code = run(args, streams{in: strings.NewReader(stdin), out: &out, err: &errOut, prompt: func(string) (string, error) {
		return "", errors.New("no terminal in tests")
	}})
	return code, out.String(), errOut.String()
}

func (c *cli) users() []model.User {
	c.t.Helper()
	st, err := store.Open(c.db)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	list, err := st.ListUsers(context.Background())
	if err != nil {
		c.t.Fatal(err)
	}
	return list
}

const cliPassword = "correct horse battery"

func TestUserAddCreatesAnAccountWithAnArgon2idHash(t *testing.T) {
	c := newCLI(t)
	code, out, errOut := c.run(cliPassword+"\n", "user", "add", "alice", "--role", "admin", "--password-stdin")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(out, "alice") || !strings.Contains(out, "admin") {
		t.Errorf("output = %q", out)
	}
	if users := c.users(); len(users) != 1 || users[0].Username != "alice" || users[0].Role != model.RoleAdmin {
		t.Fatalf("users = %+v", users)
	}

	st, _ := store.Open(c.db)
	defer func() { _ = st.Close() }()
	rec, err := st.GetUserByName(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rec.PasswordHash, "$argon2id$v=19$m=") || strings.Contains(rec.PasswordHash, cliPassword) {
		t.Errorf("stored hash = %q", rec.PasswordHash)
	}
	if ok, err := auth.Verify(cliPassword, rec.PasswordHash); err != nil || !ok {
		t.Errorf("the hash does not verify the password: %v %v", ok, err)
	}
	if strings.Contains(out+errOut, cliPassword) {
		t.Error("the password was echoed")
	}
}

func TestUserAddFlagsMayFollowTheName(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{
		{"user", "add", "alice", "--role", "viewer", "--password-stdin"},
		{"user", "add", "--role", "viewer", "--password-stdin", "bob"},
	} {
		if code, _, errOut := c.run(cliPassword+"\n", args...); code != 0 {
			t.Errorf("%v: exit %d: %s", args, code, errOut)
		}
	}
	if len(c.users()) != 2 {
		t.Errorf("%d users", len(c.users()))
	}
}

func TestThePasswordIsNeverAnArgument(t *testing.T) {
	c := newCLI(t)
	for _, flagName := range []string{"--password", "-p", "--pass", "--passwd"} {
		code, _, errOut := c.run("", "user", "add", "bob", "--role", "viewer", flagName, "hunter2hunter2")
		if code == 0 {
			t.Errorf("%s was accepted", flagName)
		}
		if !strings.Contains(errOut, "flag provided but not defined") {
			t.Errorf("%s: stderr = %q", flagName, errOut)
		}
	}
	if len(c.users()) != 0 {
		t.Error("an account was created with a password from the command line")
	}
}

func TestUserAddRefusals(t *testing.T) {
	c := newCLI(t)
	tests := []struct {
		name  string
		stdin string
		args  []string
		want  string
	}{
		{"no role", cliPassword + "\n", []string{"user", "add", "alice", "--password-stdin"}, "--role is required"},
		{"unknown role", cliPassword + "\n", []string{"user", "add", "alice", "--role", "root", "--password-stdin"}, "--role is required"},
		{"bad name", cliPassword + "\n", []string{"user", "add", "a", "--role", "admin", "--password-stdin"}, "username is invalid"},
		{"short password", "short\n", []string{"user", "add", "alice", "--role", "admin", "--password-stdin"}, "password is shorter than the minimum"},
		{"password equals the name", "administrator\n", []string{"user", "add", "administrator", "--role", "admin", "--password-stdin"}, "password equals username"},
		{"empty stdin", "", []string{"user", "add", "alice", "--role", "admin", "--password-stdin"}, "no password on standard input"},
		{"empty line", "\n", []string{"user", "add", "alice", "--role", "admin", "--password-stdin"}, "the password is empty"},
		{"no name", cliPassword + "\n", []string{"user", "add", "--role", "admin", "--password-stdin"}, "exactly one user name"},
		{"two names", cliPassword + "\n", []string{"user", "add", "a1", "a2", "--role", "admin", "--password-stdin"}, "exactly one user name"},
		{"no terminal", "", []string{"user", "add", "alice", "--role", "admin"}, "no terminal in tests"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := c.run(tt.stdin, tt.args...)
			if code != 1 || !strings.Contains(errOut, tt.want) {
				t.Errorf("exit %d, stderr %q; want exit 1 with %q", code, errOut, tt.want)
			}
		})
	}
	if len(c.users()) != 0 {
		t.Error("a refused command created an account")
	}
}

func TestUserAddDuplicateAndCaseInsensitivity(t *testing.T) {
	c := newCLI(t)
	c.run(cliPassword+"\n", "user", "add", "alice", "--role", "admin", "--password-stdin")
	code, _, errOut := c.run(cliPassword+"\n", "user", "add", "ALICE", "--role", "viewer", "--password-stdin")
	if code != 1 || !strings.Contains(errOut, "already exists") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if len(c.users()) != 1 {
		t.Error("a duplicate was created")
	}
}

func TestUserAddOnTheTerminalAsksTwice(t *testing.T) {
	c := newCLI(t)
	var labels []string
	answers := []string{cliPassword, cliPassword}
	var out, errOut bytes.Buffer
	code := run([]string{"user", "add", "alice", "--role", "admin", "--config", c.cfg}, streams{
		in: strings.NewReader(""), out: &out, err: &errOut,
		prompt: func(label string) (string, error) {
			labels = append(labels, label)
			a := answers[0]
			answers = answers[1:]
			return a, nil
		},
	})
	if code != 0 || len(labels) != 2 {
		t.Fatalf("exit %d, prompts %v, stderr %q", code, labels, errOut.String())
	}

	mismatch := []string{"first password value", "different second one"}
	code = run([]string{"user", "add", "bob", "--role", "admin", "--config", c.cfg}, streams{
		in: strings.NewReader(""), out: &out, err: &errOut,
		prompt: func(string) (string, error) {
			a := mismatch[0]
			mismatch = mismatch[1:]
			return a, nil
		},
	})
	if code != 1 || !strings.Contains(errOut.String(), "do not match") {
		t.Errorf("mismatch: exit %d, stderr %q", code, errOut.String())
	}
	if len(c.users()) != 1 {
		t.Error("an account was created although the passwords differed")
	}
}

func TestUserListAndDisable(t *testing.T) {
	c := newCLI(t)
	c.run(cliPassword+"\n", "user", "add", "root", "--role", "admin", "--password-stdin")
	c.run(cliPassword+"\n", "user", "add", "vera", "--role", "viewer", "--password-stdin")

	code, out, errOut := c.run("", "user", "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "USERNAME") {
		t.Fatalf("output:\n%s", out)
	}
	if f := strings.Fields(lines[1]); f[0] != "root" || f[1] != "admin" || f[2] != "enabled" {
		t.Errorf("line 1 = %v", f)
	}
	if strings.Contains(out, "argon2") {
		t.Error("the list shows a hash")
	}

	if code, out, errOut := c.run("", "user", "disable", "vera"); code != 0 || !strings.Contains(out, "disabled") {
		t.Fatalf("disable: exit %d %q %q", code, out, errOut)
	}
	_, out, _ = c.run("", "user", "list")
	if !strings.Contains(out, "disabled") {
		t.Errorf("the list does not show the disabled account:\n%s", out)
	}
	if code, _, errOut := c.run("", "user", "disable", "ghost"); code != 1 || !strings.Contains(errOut, `no user "ghost"`) {
		t.Errorf("unknown user: exit %d %q", code, errOut)
	}
	if code, _, errOut := c.run("", "user", "list", "extra"); code != 1 {
		t.Errorf("list with an argument: exit %d %q", code, errOut)
	}
}

func TestTheLastAdministratorCannotBeDisabled(t *testing.T) {
	c := newCLI(t)
	c.run(cliPassword+"\n", "user", "add", "root", "--role", "admin", "--password-stdin")
	code, _, errOut := c.run("", "user", "disable", "root")
	if code != 1 || !strings.Contains(errOut, "cannot remove the last administrator") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if c.users()[0].Disabled {
		t.Error("the only administrator was disabled")
	}
}

func TestUserPasswdReplacesThePassword(t *testing.T) {
	c := newCLI(t)
	c.run(cliPassword+"\n", "user", "add", "alice", "--role", "admin", "--password-stdin")
	const next = "a brand new passphrase"
	if code, _, errOut := c.run(next+"\n", "user", "passwd", "alice", "--password-stdin"); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	st, _ := store.Open(c.db)
	defer func() { _ = st.Close() }()
	rec, _ := st.GetUserByName(context.Background(), "alice")
	if ok, _ := auth.Verify(next, rec.PasswordHash); !ok {
		t.Error("the new password does not verify")
	}
	if ok, _ := auth.Verify(cliPassword, rec.PasswordHash); ok {
		t.Error("the old password still verifies")
	}
	if code, _, errOut := c.run("short\n", "user", "passwd", "alice", "--password-stdin"); code != 1 || !strings.Contains(errOut, "shorter than the minimum") {
		t.Errorf("short password: exit %d %q", code, errOut)
	}
	if code, _, errOut := c.run(next+"\n", "user", "passwd", "ghost", "--password-stdin"); code != 1 || !strings.Contains(errOut, `no user "ghost"`) {
		t.Errorf("unknown user: exit %d %q", code, errOut)
	}
}

func TestUserCommandsAreAudited(t *testing.T) {
	c := newCLI(t)
	c.run(cliPassword+"\n", "user", "add", "root", "--role", "admin", "--password-stdin")
	c.run(cliPassword+"\n", "user", "add", "vera", "--role", "viewer", "--password-stdin")
	c.run("", "user", "disable", "vera")
	c.run("a brand new passphrase\n", "user", "passwd", "vera", "--password-stdin")

	st, _ := store.Open(c.db)
	defer func() { _ = st.Close() }()
	entries, err := st.ListAudit(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for i := len(entries) - 1; i >= 0; i-- {
		actions = append(actions, entries[i].Action+":"+entries[i].EntityID)
		if strings.Contains(entries[i].Detail, "passphrase") || strings.Contains(entries[i].Detail, cliPassword) {
			t.Errorf("a password reached the audit log: %+v", entries[i])
		}
	}
	want := "user_create:root user_create:vera user_update:vera user_password_reset:vera"
	if strings.Join(actions, " ") != want {
		t.Errorf("audit = %v, want %s", actions, want)
	}
}

func TestUserUnknownSubcommand(t *testing.T) {
	c := newCLI(t)
	if code, _, errOut := c.run("", "user", "delete", "alice"); code != 1 || !strings.Contains(errOut, "unknown user command") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	var errOut bytes.Buffer
	if code := run([]string{"user"}, streams{in: strings.NewReader(""), out: io.Discard, err: &errOut}); code != 0 || !strings.Contains(errOut.String(), "usage: netscribe user") {
		t.Errorf("user without arguments: exit %d %q", code, errOut.String())
	}
}

func TestReadPasswordFromStdin(t *testing.T) {
	for name, tt := range map[string]struct {
		in   string
		want string
		err  string
	}{
		"line":                {"secret value 123\n", "secret value 123", ""},
		"CRLF":                {"secret value 123\r\n", "secret value 123", ""},
		"no trailing newline": {"secret value 123", "secret value 123", ""},
		"spaces are kept":     {"  padded secret  \n", "  padded secret  ", ""},
		"only the first line": {"first line here\nsecond line\n", "first line here", ""},
		"unicode":             {"пароль-юникод-🔐\n", "пароль-юникод-🔐", ""},
		"empty input":         {"", "", "no password on standard input"},
		"empty line":          {"\n", "", "the password is empty"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := readPassword(streams{in: strings.NewReader(tt.in)}, true)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Errorf("error = %v, want %q", err, tt.err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("readPassword = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestScanTargetsMayPrecedeFlags(t *testing.T) {
	c := newCLI(t)
	// A public target is refused before any network traffic, which proves the argument was parsed
	// although the flag follows it.
	var errOut bytes.Buffer
	code := run([]string{"scan", "8.8.8.8/32", "--config", c.cfg}, streams{in: strings.NewReader(""), out: io.Discard, err: &errOut})
	if code != 1 || !strings.Contains(errOut.String(), "target is outside private ranges") {
		t.Errorf("exit %d, stderr %q", code, errOut.String())
	}
}

type fakePruner struct {
	mu       sync.Mutex
	sessions int
	audits   int
	keep     int
}

func (p *fakePruner) PruneSessions(context.Context, time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessions++
	return nil
}

func (p *fakePruner) PruneAudit(_ context.Context, keepDays int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.audits++
	p.keep = keepDays
	return nil
}

func TestMaintainPrunesAtStartAndOnEveryTick(t *testing.T) {
	p := &fakePruner{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		maintain(ctx, p, 365, 20*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	time.Sleep(110 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("maintain did not stop on cancellation")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions < 3 || p.audits < 3 {
		t.Errorf("pruned %d and %d times, want at least 3 each (start plus ticks)", p.sessions, p.audits)
	}
	if p.keep != 365 {
		t.Errorf("keepDays = %d", p.keep)
	}
}

func TestParseProxies(t *testing.T) {
	got, err := parseProxies([]string{"10.0.0.0/8", "fd00::/8"})
	if err != nil || len(got) != 2 || got[0].String() != "10.0.0.0/8" {
		t.Errorf("parseProxies = %v, %v", got, err)
	}
	if _, err := parseProxies([]string{"10.0.0.1"}); err == nil {
		t.Error("a bare address was accepted as a CIDR")
	}
	if got, err := parseProxies(nil); err != nil || len(got) != 0 {
		t.Errorf("parseProxies(nil) = %v, %v", got, err)
	}
}
