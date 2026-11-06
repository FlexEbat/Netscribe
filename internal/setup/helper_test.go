package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/update"
)

func helperSetup(t *testing.T) (*fakeSys, Options, Env) {
	t.Helper()
	f, o := installed(t)
	o.Version = "v0.2.0"
	o.HelperStatus = filepath.Join(t.TempDir(), "status.json")
	env := f.env("")
	env.Interactive = false
	return f, o, env
}

func leaveRequest(t *testing.T, o Options, text string) string {
	t.Helper()
	p := RequestPath(o)
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func statusOf(t *testing.T, o Options) HelperStatus {
	t.Helper()
	var st HelperStatus
	if err := json.Unmarshal([]byte(readFile(t, o.HelperStatus)), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestHelperRestartsTheServiceAndRemovesTheRequest(t *testing.T) {
	f, o, env := helperSetup(t)
	req := leaveRequest(t, o, "restart\n")
	if err := Helper(context.Background(), env, o, req); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.ran(), "systemctl restart netscribe") {
		t.Errorf("the service was not restarted:\n%s", f.ran())
	}
	if exists(req) {
		t.Error("the request stays, so systemd would start the helper again")
	}
	if st := statusOf(t, o); st.State != "done" || st.Action != "restart" {
		t.Errorf("status = %+v", st)
	}
}

func TestHelperUpdateInstallsOnlyTheNewestRelease(t *testing.T) {
	f, o, env := helperSetup(t)
	f.active = "active"
	var installed []string
	env.LatestRelease = func(context.Context) (update.Release, error) {
		return update.Release{Tag: "v0.3.0", Version: "0.3.0"}, nil
	}
	env.InstallRelease = func(_ context.Context, tag, dst string) error {
		installed = append(installed, tag+" -> "+dst)
		return nil
	}
	req := leaveRequest(t, o, "update")
	if err := Helper(context.Background(), env, o, req); err != nil {
		t.Fatal(err)
	}
	if len(installed) != 1 || !strings.HasPrefix(installed[0], "v0.3.0 -> ") {
		t.Fatalf("installed = %v", installed)
	}
	ran := f.ran()
	for _, want := range []string{"update --units", "systemctl restart netscribe"} {
		if !strings.Contains(ran, want) {
			t.Errorf("the update did not run %q:\n%s", want, ran)
		}
	}
	if st := statusOf(t, o); st.State != "done" {
		t.Errorf("status = %+v", st)
	}
}

func TestHelperUpdateDoesNothingWhenAlreadyCurrent(t *testing.T) {
	_, o, env := helperSetup(t)
	env.LatestRelease = func(context.Context) (update.Release, error) { return update.Release{Tag: "v0.2.0"}, nil }
	env.InstallRelease = func(context.Context, string, string) error {
		t.Error("an install ran although nothing is newer")
		return nil
	}
	if err := Helper(context.Background(), env, o, leaveRequest(t, o, "update")); err != nil {
		t.Fatal(err)
	}
}

func TestHelperReportsAFailedInstall(t *testing.T) {
	_, o, env := helperSetup(t)
	env.LatestRelease = func(context.Context) (update.Release, error) { return update.Release{Tag: "v0.3.0"}, nil }
	env.InstallRelease = func(context.Context, string, string) error { return errors.New("checksum does not match") }
	if err := Helper(context.Background(), env, o, leaveRequest(t, o, "update")); err == nil {
		t.Fatal("want the error")
	}
	if st := statusOf(t, o); st.State != "failed" || !strings.Contains(st.Message, "checksum") {
		t.Errorf("status = %+v", st)
	}
}

func TestHelperIgnoresAnythingButTheTwoWords(t *testing.T) {
	f, o, env := helperSetup(t)
	for _, body := range []string{"rm -rf /", "update; reboot", "v9.9.9", "", strings.Repeat("a", 500)} {
		req := leaveRequest(t, o, body)
		if err := Helper(context.Background(), env, o, req); err == nil {
			t.Errorf("request %q was accepted", body)
		}
		if exists(req) {
			t.Errorf("request %q stays", body)
		}
	}
	if len(f.cmds) != 0 {
		t.Errorf("a command ran for a bad request:\n%s", f.ran())
	}
}

func TestHelperDoesNotFollowALinkAndRemovesIt(t *testing.T) {
	f, o, env := helperSetup(t)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("restart"), 0o600); err != nil {
		t.Fatal(err)
	}
	req := RequestPath(o)
	if err := os.Symlink(target, req); err != nil {
		t.Fatal(err)
	}
	if err := Helper(context.Background(), env, o, req); err == nil {
		t.Fatal("a link was accepted as a request")
	}
	if len(f.cmds) != 0 {
		t.Errorf("a command ran:\n%s", f.ran())
	}
	if _, err := os.Lstat(req); err == nil {
		t.Error("the link stays")
	}
	if !exists(target) {
		t.Error("the target of the link was removed")
	}
}

func TestHelperUnitsAreRenderedForTheInstall(t *testing.T) {
	o := DefaultOptions()
	o.BinPath, o.StateDir, o.UnitDir = "/opt/ns/bin/netscribe", "/srv/ns/data", "/etc/systemd/system"
	svc, err := RenderHelperService(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ExecStart=/opt/ns/bin/netscribe helper --request /srv/ns/data/helper.request",
		"ReadWritePaths=/opt/ns/bin /etc/systemd/system /srv/ns/data /etc/netscribe",
	} {
		if !strings.Contains(svc, want) {
			t.Errorf("service lacks %q:\n%s", want, svc)
		}
	}
	path, err := RenderHelperPath(o)
	if err != nil || !strings.Contains(path, "PathExists=/srv/ns/data/helper.request") {
		t.Errorf("path unit: %v\n%s", err, path)
	}
	drop := RenderPanelDropIn(o)
	if !strings.Contains(drop, "ReadWritePaths=/etc/netscribe") {
		t.Errorf("drop-in:\n%s", drop)
	}
}

func TestEmbeddedHelperUnitsEqualTheShippedOnes(t *testing.T) {
	for name, embedded := range map[string]string{
		"netscribe-helper.service": helperServiceTemplate,
		"netscribe-helper.path":    helperPathTemplate,
	} {
		shipped, err := os.ReadFile("../../deploy/" + name) //nolint:gosec // a fixed test path
		if err != nil {
			t.Fatal(err)
		}
		if string(shipped) != embedded {
			t.Errorf("internal/setup/%s differs from deploy/%s: copy the second over the first", name, name)
		}
	}
}

func TestInstallPanelUnitsWritesThemOnceAndEnablesThePathUnit(t *testing.T) {
	f, o := installed(t)
	if err := RefreshUnits(context.Background(), f.env(""), o); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"netscribe-helper.service", "netscribe-helper.path", "netscribe.service.d/20-panel.conf"} {
		if !exists(filepath.Join(o.UnitDir, p)) {
			t.Errorf("%s was not written", p)
		}
	}
	if !strings.Contains(f.ran(), "systemctl enable --now netscribe-helper.path") {
		t.Errorf("the path unit was not started:\n%s", f.ran())
	}
	before := len(f.cmds)
	if err := RefreshUnits(context.Background(), f.env(""), o); err != nil {
		t.Fatal(err)
	}
	if len(f.cmds) != before {
		t.Errorf("an unchanged install ran commands again:\n%s", f.ran())
	}
}

func TestUpdateFlowAsksBeforeItInstalls(t *testing.T) {
	f, o := installed(t)
	o.Version = "v0.2.0"
	env := f.env(menu("n"))
	env.LatestRelease = func(context.Context) (update.Release, error) {
		return update.Release{Tag: "v0.3.0", URL: "https://github.com/FlexEbat/Netscribe/releases/tag/v0.3.0", Notes: "- faster scans\n- a fix"}, nil
	}
	env.InstallRelease = func(context.Context, string, string) error {
		t.Error("installed after a no")
		return nil
	}
	if err := Update(context.Background(), env, o, false); err != nil && !errors.Is(err, errDeclined) {
		t.Fatal(err)
	}
	out := f.out.String()
	for _, want := range []string{"v0.2.0", "v0.3.0", "faster scans", "releases/tag/v0.3.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q:\n%s", want, out)
		}
	}
}

func TestUpdateCheckOnlyNeverInstalls(t *testing.T) {
	f, o := installed(t)
	o.Version = "v0.2.0"
	env := f.env("")
	env.LatestRelease = func(context.Context) (update.Release, error) { return update.Release{Tag: "v0.3.0"}, nil }
	env.InstallRelease = func(context.Context, string, string) error {
		t.Error("installed with --check")
		return nil
	}
	if err := Update(context.Background(), env, o, true); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateSaysWhenNothingIsNewer(t *testing.T) {
	f, o := installed(t)
	o.Version = "v0.3.0"
	env := f.env("")
	env.LatestRelease = func(context.Context) (update.Release, error) { return update.Release{Tag: "v0.3.0"}, nil }
	if err := Update(context.Background(), env, o, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "up to date") {
		t.Errorf("output:\n%s", f.out.String())
	}
}

func TestUpdateOfADevBuildOnlyPointsAtTheRelease(t *testing.T) {
	f, o := installed(t)
	o.Version = "dev"
	env := f.env("")
	env.LatestRelease = func(context.Context) (update.Release, error) { return update.Release{Tag: "v0.3.0"}, nil }
	env.InstallRelease = func(context.Context, string, string) error {
		t.Error("a dev build was replaced")
		return nil
	}
	if err := Update(context.Background(), env, o, false); err != nil {
		t.Fatal(err)
	}
}

func updateEnv(t *testing.T) (*fakeSys, Options, Env, *[]string) {
	t.Helper()
	f, o, env := helperSetup(t)
	f.active = "active"
	o.RollbackWait = 300 * time.Millisecond
	env.LatestRelease = func(context.Context) (update.Release, error) {
		return update.Release{Tag: "v0.3.0", Version: "0.3.0"}, nil
	}
	var installed []string
	env.InstallRelease = func(_ context.Context, tag, dst string) error {
		installed = append(installed, tag)
		return os.WriteFile(dst, []byte("new "+tag), 0o755) //nolint:gosec // a test program
	}
	return f, o, env, &installed
}

func TestHelperUpdateKeepsThePreviousProgramBeforeReplacingIt(t *testing.T) {
	f, o, env, _ := updateEnv(t)
	prev := filepath.Join(filepath.Dir(o.BinPath), "netscribe.previous")
	var order []string
	inner := env.InstallRelease
	env.InstallRelease = func(ctx context.Context, tag, dst string) error {
		if _, err := os.Stat(prev); err != nil {
			t.Errorf("the previous program was not kept before the install: %v", err)
		}
		order = append(order, "install")
		return inner(ctx, tag, dst)
	}
	env.HealthVersion = func(context.Context, string) (string, error) { return "v0.3.0", nil }
	if err := Helper(context.Background(), env, o, leaveRequest(t, o, "update")); err != nil {
		t.Fatal(err)
	}
	if len(f.copied) == 0 || f.copied[0] != [2]string{o.BinPath, prev} {
		t.Errorf("copied = %v, want the old program copied to %s first", f.copied, prev)
	}
	if st := statusOf(t, o); st.State != "done" {
		t.Errorf("status = %+v, want done", st)
	}
}

func TestHelperRollsBackWhenTheNewProgramDoesNotAnswer(t *testing.T) {
	f, o, env, _ := updateEnv(t)
	env.HealthVersion = func(context.Context, string) (string, error) { return "", errors.New("connection refused") }

	err := Helper(context.Background(), env, o, leaveRequest(t, o, "update"))
	if err == nil {
		t.Fatal("a program that does not answer was left in place without an error")
	}
	st := statusOf(t, o)
	if st.State != "rolled back" || st.Action != "update" || !strings.Contains(st.Message, "v0.3.0") {
		t.Errorf("status = %+v, want rolled back with the version in the message", st)
	}
	prev := filepath.Join(filepath.Dir(o.BinPath), "netscribe.previous")
	if last := f.copied[len(f.copied)-1]; last != [2]string{prev, o.BinPath} {
		t.Errorf("last copy = %v, want the previous program put back at %s", last, o.BinPath)
	}
	restarts := strings.Count(f.ran(), "systemctl restart netscribe")
	if restarts != 2 {
		t.Errorf("the service was restarted %d times, want 2: after the update and after the rollback", restarts)
	}
}

func TestHelperRollsBackWhenTheWrongVersionAnswers(t *testing.T) {
	_, o, env, _ := updateEnv(t)
	env.HealthVersion = func(context.Context, string) (string, error) { return "v0.2.0", nil } // the old one
	if err := Helper(context.Background(), env, o, leaveRequest(t, o, "update")); err == nil {
		t.Fatal("want a rollback")
	}
	if st := statusOf(t, o); st.State != "rolled back" {
		t.Errorf("status = %+v, want rolled back", st)
	}
}

func TestHelperDoesNotRollBackWhenTheChecksumFails(t *testing.T) {
	f, o, env, _ := updateEnv(t)
	env.InstallRelease = func(context.Context, string, string) error {
		return errors.New("netscribe_0.3.0_linux_amd64.tar.gz does not match its checksum: nothing was installed")
	}
	env.HealthVersion = func(context.Context, string) (string, error) {
		t.Error("the panel was asked about a version that was never installed")
		return "", nil
	}
	if err := Helper(context.Background(), env, o, leaveRequest(t, o, "update")); err == nil {
		t.Fatal("want the error")
	}
	if st := statusOf(t, o); st.State != "failed" || !strings.Contains(st.Message, "checksum") {
		t.Errorf("status = %+v, want failed with the checksum in it", st)
	}
	if strings.Contains(f.ran(), "systemctl restart") {
		t.Error("the service was restarted for a release that was not installed")
	}
	if _, err := os.Stat(o.BinPath); err == nil {
		if b, _ := os.ReadFile(o.BinPath); strings.HasPrefix(string(b), "new") {
			t.Error("the program was replaced although the checksum failed")
		}
	}
}

func TestHelperUpdateWithoutARunningServiceHasNothingToWaitFor(t *testing.T) {
	f, o, env, installed := updateEnv(t)
	f.active = "inactive"
	env.HealthVersion = func(context.Context, string) (string, error) {
		t.Error("the panel was asked although the service was not running")
		return "", nil
	}
	if err := Helper(context.Background(), env, o, leaveRequest(t, o, "update")); err != nil {
		t.Fatal(err)
	}
	if len(*installed) != 1 {
		t.Errorf("installed = %v, want the one release", *installed)
	}
}
