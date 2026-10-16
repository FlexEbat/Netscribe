package main

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/render"
	"github.com/FlexEbat/Netscribe/internal/store"
)

var generatedLine = regexp.MustCompile(`(?m)^Generated: .*$`)

func seedHost(t *testing.T, c *cli) {
	t.Helper()
	st, err := store.Open(c.db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	host := model.DeviceInput{Key: model.DeviceKey{IP: "192.168.1.5"}, Hostname: "docker-host", Source: "local"}
	if err := st.ReplaceContainers(context.Background(), host, []model.ContainerInput{
		{Device: host.Key, ID: "abcd", Name: "web", Image: "nginx:1.27", State: "running"},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestExportWritesTheFileTheAPIServes(t *testing.T) {
	c := newCLI(t)
	seedHost(t, c)

	for format, ext := range map[string]string{"md": "md", "drawio": "drawio"} {
		out := filepath.Join(t.TempDir(), "doc."+ext)
		if code, _, stderr := c.run("", "export", "--format", format, "--out", out); code != 0 {
			t.Fatalf("%s: exit %d: %s", format, code, stderr)
		}
		got, err := os.ReadFile(out) //nolint:gosec // a path inside the test directory
		if err != nil {
			t.Fatal(err)
		}
		st, err := store.Open(c.db)
		if err != nil {
			t.Fatal(err)
		}
		want, err := render.Export(context.Background(), st, format, time.Now())
		_ = st.Close()
		if err != nil {
			t.Fatal(err)
		}
		if generatedLine.ReplaceAllString(string(got), "") != generatedLine.ReplaceAllString(string(want.Body), "") {
			t.Errorf("%s: the CLI file differs from what the API serves", format)
		}
		if !strings.Contains(string(got), "docker-host") {
			t.Errorf("%s: the host is missing:\n%s", format, got)
		}
		if info, err := os.Stat(out); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s: file mode = %v, %v, want owner only", format, info, err)
		}
	}
}

func TestExportWritesToStandardOutputByDefault(t *testing.T) {
	c := newCLI(t)
	seedHost(t, c)
	code, stdout, stderr := c.run("", "export")
	if code != 0 || !strings.HasPrefix(stdout, "# Netscribe network documentation") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestExportOfAnEmptyDatabaseWorks(t *testing.T) {
	c := newCLI(t)
	code, stdout, stderr := c.run("", "export", "--out", "-")
	if code != 0 || !strings.Contains(strings.ToLower(stdout), "no devices") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestExportRefusals(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{
		{"export", "--format", "pdf"},
		{"export", "--format", "html"},
		{"export", "stray"},
	} {
		if code, _, stderr := c.run("", args...); code != 1 || stderr == "" {
			t.Errorf("%v: exit %d, stderr %q", args, code, stderr)
		}
	}
}
