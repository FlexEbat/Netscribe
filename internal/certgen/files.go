package certgen

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// CertName and KeyName are the file names inside the certificate directory.
	CertName = "tls.crt"
	KeyName  = "tls.key"

	dirMode  = 0o750
	certMode = 0o644
	keyMode  = 0o600

	// recheckEvery bounds how often the reloader looks at the files, so a busy server does not
	// call stat on every handshake.
	recheckEvery = 30 * time.Second
)

// Owner is the account that must own the files, so the service user can read the key when
// root made it. A nil Owner leaves the files with the user who runs the command.
type Owner struct{ UID, GID int }

// ErrExists is returned when a file is there and force is off.
var ErrExists = errors.New("certificate files exist already; use --force to replace them")

// WriteFiles writes tls.crt (0644) and tls.key (0600) into dir. It creates dir with 0750 and
// refuses a directory other users can write to. Without force it refuses to replace a file;
// with force it first renames the old files to tls.crt.previous and tls.key.previous. Each
// file goes through a temporary file and a rename, so a reader never sees half a file. A
// non-nil owner gets the new directory and both files.
func WriteFiles(dir string, certPEM, keyPEM []byte, force bool, owner *Owner) error {
	_, statErr := os.Stat(dir)
	madeDir := errors.Is(statErr, fs.ErrNotExist)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if madeDir && owner != nil {
		if err := os.Chown(dir, owner.UID, owner.GID); err != nil {
			return fmt.Errorf("give %s to its owner: %w", dir, err)
		}
	}
	st, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if st.Mode().Perm()&0o002 != 0 {
		return fmt.Errorf("%s can be written by every user; change its mode first", dir)
	}
	certPath, keyPath := filepath.Join(dir, CertName), filepath.Join(dir, KeyName)
	var existing []string
	for _, p := range []string{certPath, keyPath} {
		if _, err := os.Lstat(p); err == nil {
			existing = append(existing, p)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if len(existing) > 0 && !force {
		return ErrExists
	}
	for _, p := range existing {
		if err := os.Rename(p, p+".previous"); err != nil {
			return fmt.Errorf("keep the old file: %w", err)
		}
	}
	// The key goes first: a certificate without its key would stop the service at the next start.
	if err := writeAtomic(keyPath, keyPEM, keyMode, owner); err != nil {
		return err
	}
	return writeAtomic(certPath, certPEM, certMode, owner)
}

func writeAtomic(path string, data []byte, mode os.FileMode, owner *Owner) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) } // best effort: the file is a leftover at worst
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close() // the chmod error is the one that matters
		cleanup()
		return err
	}
	if owner != nil {
		if err := tmp.Chown(owner.UID, owner.GID); err != nil {
			_ = tmp.Close() // the chown error is the one that matters
			cleanup()
			return err
		}
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // the write error is the one that matters
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close() // the sync error is the one that matters
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return err
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Mode().Perm() != mode {
		return fmt.Errorf("%s has mode %o after writing, want %o", path, st.Mode().Perm(), mode)
	}
	return nil
}

// Reloader serves the certificate of two files and reads them again when their modification
// time changes, so a renewal needs no restart. A failed read keeps the certificate in use.
type Reloader struct {
	certFile, keyFile string
	now               func() time.Time

	mu      sync.Mutex
	cert    *tls.Certificate
	modCert time.Time
	modKey  time.Time
	checked time.Time
}

// NewReloader loads the pair once and fails when it cannot.
func NewReloader(certFile, keyFile string) (*Reloader, error) {
	r := &Reloader{certFile: certFile, keyFile: keyFile, now: time.Now}
	if err := r.load(); err != nil {
		return nil, err
	}
	r.checked = r.now()
	return r, nil
}

func (r *Reloader) load() error {
	cs, err := os.Stat(r.certFile)
	if err != nil {
		return err
	}
	ks, err := os.Stat(r.keyFile)
	if err != nil {
		return err
	}
	pair, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		return err
	}
	r.cert, r.modCert, r.modKey = &pair, cs.ModTime(), ks.ModTime()
	return nil
}

// GetCertificate is the tls.Config callback.
func (r *Reloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now := r.now(); now.Sub(r.checked) >= recheckEvery {
		r.checked = now
		if r.changed() {
			// A failed read keeps the certificate in use. The next check retries.
			if err := r.load(); err != nil {
				return r.cert, nil
			}
		}
	}
	return r.cert, nil
}

func (r *Reloader) changed() bool {
	cs, err := os.Stat(r.certFile)
	if err != nil {
		return false
	}
	ks, err := os.Stat(r.keyFile)
	if err != nil {
		return false
	}
	return !cs.ModTime().Equal(r.modCert) || !ks.ModTime().Equal(r.modKey)
}
