package update

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxArchive  = 200 << 20 // the release archive
	maxBinary   = 300 << 20 // the program inside it
	maxChecksum = 1 << 20
	downloadFor = 10 * time.Minute
)

// Installer downloads a release and replaces the program with it.
type Installer struct {
	// Client downloads. Nil means a client with a timeout.
	Client *http.Client
	// BaseURL is where releases are downloaded from, without the tag. Empty means the
	// GitHub releases of this repository. Only tests change it.
	BaseURL string
	// GOARCH is the architecture of the host: amd64 or arm64.
	GOARCH string
}

// Asset is the file name of the release archive for a tag and an architecture.
func Asset(tag, goarch string) string {
	return fmt.Sprintf("netscribe_%s_linux_%s.tar.gz", strings.TrimPrefix(tag, "v"), goarch)
}

// Install downloads the release tag, checks its SHA-256 sum against checksums.txt of the
// same release, and replaces the program at dst. The replacement is a rename, so a running
// copy keeps working and a failed download leaves dst as it was.
func (in Installer) Install(ctx context.Context, tag, dst string) error {
	if _, err := ParseVersion(tag); err != nil {
		return err
	}
	if !strings.HasPrefix(tag, "v") || strings.ContainsAny(tag, "/\\ ?#") {
		return fmt.Errorf("%q is not a release tag", tag)
	}
	if in.GOARCH != "amd64" && in.GOARCH != "arm64" {
		return fmt.Errorf("there is no release for %s: the releases are linux amd64 and arm64", in.GOARCH)
	}
	ctx, cancel := context.WithTimeout(ctx, downloadFor)
	defer cancel()
	base := in.BaseURL
	if base == "" {
		base = "https://github.com/" + Repo + "/releases/download"
	}
	base = strings.TrimSuffix(base, "/") + "/" + tag + "/"
	asset := Asset(tag, in.GOARCH)

	sums, err := in.get(ctx, base+"checksums.txt", maxChecksum)
	if err != nil {
		return fmt.Errorf("download checksums.txt: %w", err)
	}
	want, err := checksumOf(sums, asset)
	if err != nil {
		return err
	}

	dir := filepath.Dir(dst)
	archive, err := os.CreateTemp(dir, ".netscribe-archive-*")
	if err != nil {
		return fmt.Errorf("create a download file next to %s: %w", dst, err)
	}
	defer func() { _ = os.Remove(archive.Name()) }() // a leftover download is of no use
	got, err := in.download(ctx, base+asset, archive)
	if cerr := archive.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	if got != want {
		return fmt.Errorf("%s does not match its checksum: nothing was installed", asset)
	}

	next, err := os.CreateTemp(dir, ".netscribe-new-*")
	if err != nil {
		return fmt.Errorf("create a file next to %s: %w", dst, err)
	}
	defer func() { _ = os.Remove(next.Name()) }() // after a successful rename the name is gone, and Remove only reports that
	if err := extractBinary(archive.Name(), next); err != nil {
		_ = next.Close() // the extraction error is the one to report
		return err
	}
	if err := next.Chmod(0o755); err != nil { //nolint:gosec // a program must be executable
		_ = next.Close() // the chmod error is the one to report
		return err
	}
	if err := next.Close(); err != nil {
		return err
	}
	if err := os.Rename(next.Name(), dst); err != nil {
		return fmt.Errorf("replace %s: %w", dst, err)
	}
	return nil
}

func (in Installer) client() *http.Client {
	if in.Client != nil {
		return in.Client
	}
	return &http.Client{Timeout: downloadFor}
}

func (in Installer) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	var b strings.Builder
	if _, err := in.copyTo(ctx, url, &b, limit); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// download writes the body to w and returns its SHA-256 sum in hex.
func (in Installer) download(ctx context.Context, url string, w io.Writer) (string, error) {
	h := sha256.New()
	if _, err := in.copyTo(ctx, url, io.MultiWriter(w, h), maxArchive); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (in Installer) copyTo(ctx context.Context, url string, w io.Writer, limit int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "netscribe-update")
	resp, err := in.client().Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }() // the body is read already
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("the server answered %d", resp.StatusCode)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, fmt.Errorf("the file is larger than %d MiB", limit>>20)
	}
	return n, nil
}

// checksumOf finds the sum of name in the text of checksums.txt ("<sum>  <name>").
func checksumOf(sums []byte, name string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if len(f[0]) != sha256.Size*2 {
				return "", fmt.Errorf("checksums.txt has a bad sum for %s", name)
			}
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

// extractBinary copies the file named netscribe out of the archive into w.
func extractBinary(archive string, w io.Writer) error {
	f, err := os.Open(archive) //nolint:gosec // the path is a temporary file this package created
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // read only
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("the archive is not gzip: %w", err)
	}
	defer func() { _ = gz.Close() }() // read only
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the archive has no netscribe program")
		}
		if err != nil {
			return fmt.Errorf("read the archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || path.Base(h.Name) != "netscribe" {
			continue
		}
		n, err := io.Copy(w, io.LimitReader(tr, maxBinary+1))
		if err != nil {
			return err
		}
		if n > maxBinary {
			return fmt.Errorf("the program is larger than %d MiB", maxBinary>>20)
		}
		if n == 0 {
			return errors.New("the program in the archive is empty")
		}
		return nil
	}
}
