package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/backup"
	"github.com/FlexEbat/Netscribe/internal/config"
)

// stagedInstall is an install with a database and a stage that holds a backup of it.
func stagedInstall(t *testing.T) (*fakeSys, Options, Env, string) {
	t.Helper()
	f, o, env := helperSetup(t)
	cfg, err := config.Load(o.ConfigPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.Database, []byte("old database"), 0o600); err != nil {
		t.Fatal(err)
	}
	stage := StageDir(o)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, backup.NameDB), []byte("new database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, backup.NameConfig), []byte(readFile(t, o.ConfigPath)), 0o600); err != nil {
		t.Fatal(err)
	}
	return f, o, env, cfg.Database
}

func TestHelperAcceptsTheRestoreRequest(t *testing.T) {
	_, o, _, _ := stagedInstall(t)
	req := leaveRequest(t, o, "restore\n")
	verb, err := ReadRequest(req)
	if err != nil || verb != HelperRestore {
		t.Fatalf("verb %q, err %v", verb, err)
	}
}

func TestHelperRestorePutsTheStagedFilesInPlace(t *testing.T) {
	f, o, env, db := stagedInstall(t)
	req := leaveRequest(t, o, "restore")
	if err := Helper(context.Background(), env, o, req); err != nil {
		t.Fatal(err)
	}
	ran := f.ran()
	stage := StageDir(o)
	// the order is what protects the data: take over, stop, keep the old, copy, start, clean up
	order := []string{
		"chown -R -h root:root " + stage,
		"systemctl stop netscribe",
		"cp -p -- " + db + " " + db + beforeRestore,
		"cp -p -- " + o.ConfigPath + " " + o.ConfigPath + beforeRestore,
		"rm -f -- " + db + "-wal " + db + "-shm",
		"cp --no-preserve=mode,ownership -- " + filepath.Join(stage, backup.NameDB) + " " + db,
		"chmod 0600 " + db,
		"cp --no-preserve=mode,ownership -- " + filepath.Join(stage, backup.NameConfig) + " " + o.ConfigPath,
		"systemctl start netscribe",
		"rm -rf -- " + stage,
	}
	at := 0
	for _, want := range order {
		i := strings.Index(ran[at:], want)
		if i < 0 {
			t.Fatalf("%q is missing or out of order in:\n%s", want, ran)
		}
		at += i + len(want)
	}
	if st := statusOf(t, o); st.State != "done" || st.Action != "restore" {
		t.Errorf("status = %+v", st)
	}
	if exists(req) {
		t.Error("the request stays")
	}
}

func TestHelperRestoreRefusesABadStageBeforeItStopsAnything(t *testing.T) {
	cases := map[string]func(t *testing.T, o Options, db string){
		"no database": func(t *testing.T, o Options, _ string) {
			if err := os.Remove(filepath.Join(StageDir(o), backup.NameDB)); err != nil {
				t.Fatal(err)
			}
		},
		"a link instead of the config": func(t *testing.T, o Options, _ string) {
			p := filepath.Join(StageDir(o), backup.NameConfig)
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(o.ConfigPath, p); err != nil {
				t.Fatal(err)
			}
		},
		"a config that is not valid": func(t *testing.T, o Options, _ string) {
			if err := os.WriteFile(filepath.Join(StageDir(o), backup.NameConfig), []byte("listen: [not valid"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"another database path": func(t *testing.T, o Options, db string) {
			p := filepath.Join(StageDir(o), backup.NameConfig)
			text := strings.ReplaceAll(readFile(t, p), db, db+"-other")
			if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			f, o, env, db := stagedInstall(t)
			spoil(t, o, db)
			err := Helper(context.Background(), env, o, leaveRequest(t, o, "restore"))
			if err == nil {
				t.Fatal("no error")
			}
			if strings.Contains(f.ran(), "systemctl stop") {
				t.Errorf("the service was stopped for a stage that cannot be used:\n%s", f.ran())
			}
			if got := readFile(t, db); got != "old database" {
				t.Errorf("the database changed to %q", got)
			}
			if st := statusOf(t, o); st.State != "failed" {
				t.Errorf("status = %+v", st)
			}
		})
	}
}

func TestHelperRestorePutsTheOldFilesBackWhenThePanelDoesNotAnswer(t *testing.T) {
	f, o, env, db := stagedInstall(t)
	f.health = errors.New("connection refused")
	err := Helper(context.Background(), env, o, leaveRequest(t, o, "restore"))
	if err == nil || !strings.Contains(err.Error(), "old data was put back") {
		t.Fatalf("err = %v", err)
	}
	ran := f.ran()
	for _, want := range []string{
		"cp -p -- " + db + beforeRestore + " " + db,
		"cp -p -- " + o.ConfigPath + beforeRestore + " " + o.ConfigPath,
	} {
		if !strings.Contains(ran, want) {
			t.Errorf("%q is missing:\n%s", want, ran)
		}
	}
	if strings.Contains(ran, "rm -rf") {
		t.Error("the stage was removed after a failed restore")
	}
	if st := statusOf(t, o); st.State != "failed" {
		t.Errorf("status = %+v", st)
	}
}
