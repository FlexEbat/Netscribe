// Package backup writes and reads the archive that holds a whole Netscribe install: the
// database, the config and a manifest. It uses only the standard library and knows nothing
// about SQLite or the config format, so the checks that need them are done by the caller.
package backup

import (
	"archive/tar"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// The only names an archive may hold. Nothing else is ever read from one or written to one.
const (
	NameDB       = "netscribe.db"
	NameConfig   = "netscribe.yaml"
	NameManifest = "manifest.json"
	NameTOTPKey  = "totp.key"
)

// Limits of one entry, in bytes.
const (
	MaxDB       = 1 << 30
	MaxConfig   = 1 << 20
	MaxManifest = 4 << 10
	MaxKey      = 1 << 10
)

// limits maps every allowed name to the largest size of its entry.
var limits = map[string]int64{
	NameDB:       MaxDB,
	NameConfig:   MaxConfig,
	NameManifest: MaxManifest,
	NameTOTPKey:  MaxKey,
}

// maxTotal bounds everything that is read after decompression, so a small archive cannot
// become an enormous stream.
const maxTotal = MaxDB + MaxConfig + MaxManifest + MaxKey + 64<<10

// Manifest describes an archive.
type Manifest struct {
	// Version is the Netscribe version that wrote the archive.
	Version string `json:"version"`
	// Schema is the number of the newest migration applied to the database in the archive.
	Schema    int       `json:"schema"`
	CreatedAt time.Time `json:"createdAt"`
}

// ArchiveError is a problem with what an archive holds, as opposed to a problem with the
// disk or the connection it was read from. Its text is written for the person who sent it.
type ArchiveError struct{ Msg string }

func (e *ArchiveError) Error() string { return e.Msg }

func bad(format string, a ...any) error { return &ArchiveError{Msg: fmt.Sprintf(format, a...)} }

// isContent reports whether a read error comes from a damaged archive and not from the
// reader underneath it.
func isContent(err error) bool {
	var corrupt flate.CorruptInputError
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, tar.ErrHeader) ||
		errors.Is(err, gzip.ErrChecksum) || errors.Is(err, gzip.ErrHeader) || errors.As(err, &corrupt)
}

// File is a file on disk that goes into an archive under Name.
type File struct {
	Name string
	Path string
}

// Write writes a gzip compressed tar archive: the manifest first, then every file. A name
// that is not allowed, a missing database or config, or a file over its limit is an error.
func Write(w io.Writer, m Manifest, files []File) error {
	seen := map[string]bool{}
	for _, f := range files {
		if _, ok := limits[f.Name]; !ok || f.Name == NameManifest {
			return fmt.Errorf("backup: %q cannot go into an archive", f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("backup: %q is listed twice", f.Name)
		}
		seen[f.Name] = true
	}
	if !seen[NameDB] || !seen[NameConfig] {
		return errors.New("backup: an archive needs the database and the config")
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("backup: write the manifest: %w", err)
	}
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := addBytes(tw, NameManifest, data, m.CreatedAt); err != nil {
		return err
	}
	for _, f := range files {
		if err := addFile(tw, f, m.CreatedAt); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("backup: finish the archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("backup: finish the archive: %w", err)
	}
	return nil
}

func addBytes(tw *tar.Writer, name string, data []byte, at time.Time) error {
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: at, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: write %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("backup: write %s: %w", name, err)
	}
	return nil
}

func addFile(tw *tar.Writer, f File, at time.Time) error {
	src, err := os.Open(f.Path) //nolint:gosec // the caller names the files of its own install
	if err != nil {
		return fmt.Errorf("backup: open %s: %w", f.Name, err)
	}
	defer func() { _ = src.Close() }() // read only
	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("backup: read %s: %w", f.Name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("backup: %s is not a regular file", f.Name)
	}
	if info.Size() > limits[f.Name] {
		return fmt.Errorf("backup: %s is larger than %d bytes", f.Name, limits[f.Name])
	}
	hdr := &tar.Header{Name: f.Name, Mode: 0o600, Size: info.Size(), ModTime: at, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("backup: write %s: %w", f.Name, err)
	}
	if n, err := io.Copy(tw, io.LimitReader(src, info.Size())); err != nil || n != info.Size() {
		return fmt.Errorf("backup: write %s: %w", f.Name, errors.Join(err, errShort(n, info.Size())))
	}
	return nil
}

func errShort(n, want int64) error {
	if n == want {
		return nil
	}
	return fmt.Errorf("wrote %d of %d bytes", n, want)
}

// Stage reads an archive and writes its files into dir, which it creates (mode 0700) and
// which must not exist. It accepts only regular entries with the allowed names, each once and
// inside its size limit, and it needs the manifest, the database and the config. On any error
// dir is removed. It does not look inside the files: the caller checks the database and the
// config before it trusts them.
func Stage(r io.Reader, dir string) (Manifest, error) {
	m, err := stage(r, dir)
	if err != nil {
		_ = os.RemoveAll(dir) // a half written stage must not be mistaken for a good one
		return Manifest{}, err
	}
	return m, nil
}

func stage(r io.Reader, dir string) (Manifest, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return Manifest{}, bad("the file is not a gzip archive")
	}
	defer func() { _ = gz.Close() }() // read only
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return Manifest{}, fmt.Errorf("prepare the stage: %w", err)
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return Manifest{}, fmt.Errorf("prepare the stage: %w", err)
	}
	limited := io.LimitReader(gz, maxTotal)
	tr := tar.NewReader(limited)
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if isContent(err) {
				return Manifest{}, bad("the archive is damaged")
			}
			return Manifest{}, err
		}
		limit, ok := limits[hdr.Name]
		if hdr.Typeflag != tar.TypeReg || !ok {
			return Manifest{}, bad("the archive holds an entry that is not allowed (%s)", clipName(hdr.Name))
		}
		if seen[hdr.Name] {
			return Manifest{}, bad("the archive holds %s twice", hdr.Name)
		}
		seen[hdr.Name] = true
		if hdr.Size < 0 || hdr.Size > limit {
			return Manifest{}, bad("%s in the archive is too large", hdr.Name)
		}
		// hdr.Name is one of the four allowed names: the lookup above refused anything else.
		if err := writeStaged(filepath.Join(dir, hdr.Name), tr, hdr.Size); err != nil { //nolint:gosec // not a path from the archive, see above
			return Manifest{}, err
		}
	}
	// The tar ends before the gzip trailer. Reading on makes gzip check its checksum, so a
	// cut off archive is found even when every entry arrived.
	if _, err := io.Copy(io.Discard, io.LimitReader(limited, 1<<20)); err != nil {
		if isContent(err) {
			return Manifest{}, bad("the archive is damaged")
		}
		return Manifest{}, err
	}
	for _, n := range []string{NameManifest, NameDB, NameConfig} {
		if !seen[n] {
			return Manifest{}, bad("the archive has no %s", n)
		}
	}
	return readManifest(filepath.Join(dir, NameManifest))
}

func writeStaged(path string, r io.Reader, size int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the name is one of four fixed names inside the stage
	if err != nil {
		return fmt.Errorf("stage %s: %w", filepath.Base(path), err)
	}
	n, err := io.Copy(f, io.LimitReader(r, size))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		if isContent(err) {
			return bad("the archive is damaged")
		}
		return fmt.Errorf("stage %s: %w", filepath.Base(path), err)
	}
	if n != size {
		return bad("the archive is damaged")
	}
	return nil
}

// ReadManifest reads and checks the manifest file inside a stage.
func ReadManifest(dir string) (Manifest, error) {
	return readManifest(filepath.Join(dir, NameManifest))
}

func readManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path) //nolint:gosec // inside the stage
	if err != nil {
		return Manifest{}, fmt.Errorf("read the manifest: %w", err)
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, bad("the manifest of the archive is not valid")
	}
	if m.Schema < 1 {
		return Manifest{}, bad("the manifest of the archive has no schema number")
	}
	return m, nil
}

func clipName(s string) string {
	const max = 40
	r := []rune(s)
	if len(r) > max {
		r = append(r[:max], '…')
	}
	out := make([]rune, 0, len(r))
	for _, c := range r {
		if c < 0x20 || c == 0x7f {
			c = '?'
		}
		out = append(out, c)
	}
	return string(out)
}
