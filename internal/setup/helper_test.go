package setup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
