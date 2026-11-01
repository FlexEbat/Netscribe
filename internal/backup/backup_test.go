package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var at = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func writeSrc(t *testing.T, dir, name, body string) File {
	t.Helper()
	p := filepath.Join(dir, "src-"+name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return File{Name: name, Path: p}
}

func goodArchive(t *testing.T) []byte {
	t.Helper()
	src := t.TempDir()
	var buf bytes.Buffer
	err := Write(&buf, Manifest{Version: "v1", Schema: 14, CreatedAt: at},
		[]File{writeSrc(t, src, NameDB, "db-bytes"), writeSrc(t, src, NameConfig, "listen: x")})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func entries(t *testing.T, ents []tar.Header, bodies map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, h := range ents {
		h := h
		body := bodies[h.Name]
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "stage")
	m, err := Stage(bytes.NewReader(goodArchive(t)), dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "v1" || m.Schema != 14 {
		t.Fatalf("manifest %+v", m)
	}
	for name, want := range map[string]string{NameDB: "db-bytes", NameConfig: "listen: x"} {
		got, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // a path inside the test directory
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v", name, got, err)
		}
		info, _ := os.Stat(filepath.Join(dir, name))
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", name, info.Mode().Perm())
		}
	}
	info, _ := os.Stat(dir)
	if info.Mode().Perm() != 0o700 {
		t.Errorf("stage mode %v", info.Mode().Perm())
	}
}

func TestWriteChecksItsInput(t *testing.T) {
	src := t.TempDir()
	db, cfg := writeSrc(t, src, NameDB, "x"), writeSrc(t, src, NameConfig, "y")
	for name, files := range map[string][]File{
		"no config":    {db},
		"no database":  {cfg},
		"odd name":     {db, cfg, {Name: "evil", Path: db.Path}},
		"manifest":     {db, cfg, {Name: NameManifest, Path: db.Path}},
		"listed twice": {db, cfg, db},
		"missing file": {db, {Name: NameConfig, Path: filepath.Join(src, "nope")}},
	} {
		if err := Write(&bytes.Buffer{}, Manifest{Schema: 1, CreatedAt: at}, files); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestStageRefusesBadArchives(t *testing.T) {
	man := `{"version":"v1","schema":3,"createdAt":"2026-10-31T12:00:00Z"}`
	reg := func(n string) tar.Header { return tar.Header{Name: n, Mode: 0o600, Typeflag: tar.TypeReg} }
	body := map[string]string{NameManifest: man, NameDB: "d", NameConfig: "c", "../x": "e", "/abs": "e"}
	cases := map[string][]byte{
		"not gzip":    []byte("plain text"),
		"traversal":   entries(t, []tar.Header{reg(NameManifest), reg(NameDB), reg(NameConfig), reg("../x")}, body),
		"absolute":    entries(t, []tar.Header{reg(NameManifest), reg(NameDB), reg(NameConfig), reg("/abs")}, body),
		"subdir":      entries(t, []tar.Header{reg(NameManifest), reg(NameDB), reg(NameConfig), reg("a/" + NameKeyForTest)}, body),
		"duplicate":   entries(t, []tar.Header{reg(NameManifest), reg(NameDB), reg(NameDB), reg(NameConfig)}, body),
		"no db":       entries(t, []tar.Header{reg(NameManifest), reg(NameConfig)}, body),
		"no config":   entries(t, []tar.Header{reg(NameManifest), reg(NameDB)}, body),
		"no manifest": entries(t, []tar.Header{reg(NameDB), reg(NameConfig)}, body),
		"symlink": entries(t, []tar.Header{reg(NameManifest), reg(NameDB), reg(NameConfig),
			{Name: NameTOTPKey, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}}, body),
		"directory": entries(t, []tar.Header{reg(NameManifest), reg(NameDB), reg(NameConfig),
			{Name: NameTOTPKey, Typeflag: tar.TypeDir, Mode: 0o700}}, body),
		"bad manifest": entries(t, []tar.Header{reg(NameManifest), reg(NameDB), reg(NameConfig)},
			map[string]string{NameManifest: `{"schema":0}`, NameDB: "d", NameConfig: "c"}),
		"unknown field": entries(t, []tar.Header{reg(NameManifest), reg(NameDB), reg(NameConfig)},
			map[string]string{NameManifest: `{"schema":3,"x":1}`, NameDB: "d", NameConfig: "c"}),
	}
	for name, data := range cases {
		dir := filepath.Join(t.TempDir(), "stage")
		_, err := Stage(bytes.NewReader(data), dir)
		var ae *ArchiveError
		if !errors.As(err, &ae) {
			t.Errorf("%s: err = %v, want an ArchiveError", name, err)
		}
		if _, err := os.Stat(dir); err == nil {
			t.Errorf("%s: the stage was left behind", name)
		}
	}
}

// NameKeyForTest is a plain file name used to build a nested path in a test.
const NameKeyForTest = "x"

func TestStageRefusesAnOversizedEntry(t *testing.T) {
	big := strings.Repeat("a", MaxConfig+1)
	data := entries(t, []tar.Header{
		{Name: NameManifest, Mode: 0o600, Typeflag: tar.TypeReg},
		{Name: NameDB, Mode: 0o600, Typeflag: tar.TypeReg},
		{Name: NameConfig, Mode: 0o600, Typeflag: tar.TypeReg},
	}, map[string]string{NameManifest: `{"schema":1}`, NameDB: "d", NameConfig: big})
	dir := filepath.Join(t.TempDir(), "stage")
	if _, err := Stage(bytes.NewReader(data), dir); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestStagePassesOnAnErrorOfTheReader(t *testing.T) {
	good := goodArchive(t)
	r := io.MultiReader(bytes.NewReader(good[:len(good)/2]), failingReader{})
	_, err := Stage(r, filepath.Join(t.TempDir(), "stage"))
	var ae *ArchiveError
	if err == nil || errors.As(err, &ae) {
		t.Fatalf("err = %v: a broken connection is not a bad archive", err)
	}
}

func TestStageTreatsATruncatedArchiveAsDamaged(t *testing.T) {
	good := goodArchive(t)
	_, err := Stage(bytes.NewReader(good[:len(good)-12]), filepath.Join(t.TempDir(), "stage"))
	var ae *ArchiveError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v", err)
	}
}

func TestStageRefusesAnExistingDirectory(t *testing.T) {
	dir := t.TempDir() // exists
	if _, err := Stage(bytes.NewReader(goodArchive(t)), dir); err == nil {
		t.Fatal("no error")
	}
}

func TestClipNameHidesControlCharacters(t *testing.T) {
	got := clipName("a\nb\x00" + strings.Repeat("z", 100))
	if strings.ContainsAny(got, "\n\x00") || len([]rune(got)) > 41 {
		t.Fatalf("%q", got)
	}
}
