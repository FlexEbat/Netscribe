package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewerComparesReleases(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.2.0", "v0.3.0", true},
		{"0.2.0", "v0.2.1", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.3.0", "v0.2.9", false},
		{"v1.0.0-rc.1", "v1.0.0", true},
		{"v1.0.0", "v1.0.0-rc.1", false},
		{"v0.9.0", "v0.10.0", true}, // numbers, not text
		{"dev", "v9.9.9", false},    // a build from a checkout is never outdated
		{"", "v9.9.9", false},
		{"v0.2.0", "latest", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestParseReleaseRefusesWhatIsNotARelease(t *testing.T) {
	good := `{"tag_name":"v0.3.0","html_url":"https://github.com/FlexEbat/Netscribe/releases/tag/v0.3.0","body":"- a fix","published_at":"2026-10-01T10:00:00Z"}`
	rel, err := ParseRelease([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.3.0" || rel.Version != "0.3.0" || rel.Notes != "- a fix" {
		t.Fatalf("release = %+v", rel)
	}
	for name, body := range map[string]string{
		"draft":      `{"tag_name":"v0.3.0","draft":true}`,
		"prerelease": `{"tag_name":"v0.3.0","prerelease":true}`,
		"bad tag":    `{"tag_name":"../../etc"}`,
		"not json":   `<html>`,
	} {
		if _, err := ParseRelease([]byte(body)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestParseReleaseReplacesALinkOutsideTheRepository(t *testing.T) {
	rel, err := ParseRelease([]byte(`{"tag_name":"v0.3.0","html_url":"https://evil.example/x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := ReleasesURL + "/tag/v0.3.0"; rel.URL != want {
		t.Fatalf("url = %q, want %q", rel.URL, want)
	}
}

func TestParseReleaseCutsLongNotes(t *testing.T) {
	body := `{"tag_name":"v0.3.0","body":"` + strings.Repeat("a", 3*maxNotes) + `"}`
	rel, err := ParseRelease([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(rel.Notes) > maxNotes+8 {
		t.Fatalf("notes are %d bytes", len(rel.Notes))
	}
}

func TestCheckerReportsANewerRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("GitHub needs a User-Agent")
		}
		_, _ = fmt.Fprint(w, `{"tag_name":"v0.3.0","body":"notes"}`)
	}))
	defer srv.Close()
	c := &Checker{URL: srv.URL, Now: func() time.Time { return time.Unix(100, 0) }}
	if st := c.Status("v0.2.0", true); st.Available || st.Latest != nil {
		t.Fatalf("before a check: %+v", st)
	}
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := c.Status("v0.2.0", true)
	if !st.Available || st.Latest == nil || st.Latest.Tag != "v0.3.0" {
		t.Fatalf("status = %+v", st)
	}
	if !st.CheckedAt.Equal(time.Unix(100, 0)) {
		t.Fatalf("checkedAt = %v", st.CheckedAt)
	}
	if got := c.Status("v0.3.0", true); got.Available {
		t.Fatal("the current release is not an update")
	}
}

func TestCheckerKeepsTheLastAnswerWhenACheckFails(t *testing.T) {
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail {
			http.Error(w, "rate limit", http.StatusForbidden)
			return
		}
		_, _ = fmt.Fprint(w, `{"tag_name":"v0.3.0"}`)
	}))
	defer srv.Close()
	c := &Checker{URL: srv.URL}
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	st := c.Status("v0.2.0", true)
	if !st.Available || st.Error == "" {
		t.Fatalf("status = %+v: the old answer stays and the error shows", st)
	}
}

func archiveOf(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{{"netscribe_0.3.0_linux_amd64/LICENSE", []byte("license")}, {name, body}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(f.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func releaseServer(t *testing.T, archive []byte, sum string) *httptest.Server {
	t.Helper()
	asset := Asset("v0.3.0", "amd64")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v0.3.0/checksums.txt":
			_, _ = fmt.Fprintf(w, "%s  %s\n%s  other.tar.gz\n", sum, asset, strings.Repeat("0", 64))
		case "/v0.3.0/" + asset:
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}

func sumOf(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestInstallReplacesTheProgram(t *testing.T) {
	archive := archiveOf(t, "netscribe_0.3.0_linux_amd64/netscribe", []byte("new program"))
	srv := releaseServer(t, archive, sumOf(archive))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "netscribe")
	if err := os.WriteFile(dst, []byte("old program"), 0o755); err != nil { //nolint:gosec // a test program
		t.Fatal(err)
	}
	if err := (Installer{BaseURL: srv.URL, GOARCH: "amd64"}).Install(context.Background(), "v0.3.0", dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst) //nolint:gosec // a test path
	if err != nil || string(got) != "new program" {
		t.Fatalf("program = %q, %v", got, err)
	}
	if info, _ := os.Stat(dst); info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v", info.Mode())
	}
	left, _ := os.ReadDir(filepath.Dir(dst))
	if len(left) != 1 {
		t.Fatalf("temporary files stay behind: %v", left)
	}
}

func TestInstallRefusesAWrongChecksum(t *testing.T) {
	archive := archiveOf(t, "netscribe", []byte("tampered"))
	srv := releaseServer(t, archive, strings.Repeat("a", 64))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "netscribe")
	if err := os.WriteFile(dst, []byte("old program"), 0o755); err != nil { //nolint:gosec // a test program
		t.Fatal(err)
	}
	err := (Installer{BaseURL: srv.URL, GOARCH: "amd64"}).Install(context.Background(), "v0.3.0", dst)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum error", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "old program" { //nolint:gosec // a test path
		t.Fatalf("the program changed to %q", got)
	}
	left, _ := os.ReadDir(filepath.Dir(dst))
	if len(left) != 1 {
		t.Fatalf("temporary files stay behind: %v", left)
	}
}

func TestInstallRefusesABadTagAndArchitecture(t *testing.T) {
	in := Installer{BaseURL: "http://127.0.0.1:1", GOARCH: "amd64"}
	for _, tag := range []string{"", "latest", "0.3.0", "v0.3.0/../x", "v0.3.0?x"} {
		if err := in.Install(context.Background(), tag, filepath.Join(t.TempDir(), "x")); err == nil {
			t.Errorf("tag %q: want an error", tag)
		}
	}
	in.GOARCH = "386"
	if err := in.Install(context.Background(), "v0.3.0", filepath.Join(t.TempDir(), "x")); err == nil {
		t.Error("386: want an error")
	}
}

func TestInstallRefusesAnArchiveWithoutTheProgram(t *testing.T) {
	archive := archiveOf(t, "netscribe_0.3.0_linux_amd64/README.md", []byte("text"))
	srv := releaseServer(t, archive, sumOf(archive))
	defer srv.Close()
	dst := filepath.Join(t.TempDir(), "netscribe")
	err := (Installer{BaseURL: srv.URL, GOARCH: "amd64"}).Install(context.Background(), "v0.3.0", dst)
	if err == nil || !strings.Contains(err.Error(), "no netscribe program") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(dst); statErr == nil {
		t.Fatal("a program was written")
	}
}
