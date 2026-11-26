package certgen

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makePair(t *testing.T, name string) (cert, key []byte) {
	t.Helper()
	c, k, err := Generate(Options{Names: []string{name}, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return c, k
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Mode().Perm()
}

func TestWriteFilesModesAndNoOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	c, k := makePair(t, "a.lan")
	if err := WriteFiles(dir, c, k, false, nil); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, filepath.Join(dir, CertName)); m != 0o644 {
		t.Errorf("cert mode = %o", m)
	}
	if m := mode(t, filepath.Join(dir, KeyName)); m != 0o600 {
		t.Errorf("key mode = %o", m)
	}
	if m := mode(t, dir); m != 0o750 {
		t.Errorf("dir mode = %o", m)
	}
	c2, k2 := makePair(t, "b.lan")
	if err := WriteFiles(dir, c2, k2, false, nil); !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, CertName)) //nolint:gosec // a path inside the test directory
	if string(got) != string(c) {
		t.Error("a refused write changed the certificate")
	}
}

func TestWriteFilesForceKeepsPrevious(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil { //nolint:gosec // the directory of the service needs the group bit
		t.Fatal(err)
	}
	c, k := makePair(t, "a.lan")
	c2, k2 := makePair(t, "b.lan")
	if err := WriteFiles(dir, c, k, false, nil); err != nil {
		t.Fatal(err)
	}
	if err := WriteFiles(dir, c2, k2, true, nil); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]byte{
		filepath.Join(dir, CertName):             c2,
		filepath.Join(dir, KeyName):              k2,
		filepath.Join(dir, CertName+".previous"): c,
		filepath.Join(dir, KeyName+".previous"):  k,
	} {
		got, err := os.ReadFile(path) //nolint:gosec // a path inside the test directory
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s holds the wrong content", filepath.Base(path))
		}
	}
	if m := mode(t, filepath.Join(dir, KeyName+".previous")); m != 0o600 {
		t.Errorf("previous key mode = %o, want 600", m)
	}
}

func TestWriteFilesRefusesWorldWritableDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o777); err != nil { //nolint:gosec // the test makes the directory world-writable on purpose
		t.Fatal(err)
	}
	c, k := makePair(t, "a.lan")
	if err := WriteFiles(dir, c, k, false, nil); err == nil {
		t.Fatal("a world-writable directory must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, KeyName)); err == nil {
		t.Error("a key was written into the refused directory")
	}
}

func TestWriteFilesLeavesNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil { //nolint:gosec // the directory of the service needs the group bit
		t.Fatal(err)
	}
	c, k := makePair(t, "a.lan")
	if err := WriteFiles(dir, c, k, false, nil); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("directory holds %d entries, want 2", len(entries))
	}
}

// handshakeCN opens a TLS connection to ln and returns the common name the server showed.
func handshakeCN(t *testing.T, addr string) string {
	t.Helper()
	conn, err := tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec // the test reads the certificate, not trust
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }() // the handshake result is what the test reads
	return conn.ConnectionState().PeerCertificates[0].Subject.CommonName
}

func TestReloaderServesTheRenewedCertificate(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil { //nolint:gosec // a path inside the test directory
		t.Fatal(err)
	}
	c, k := makePair(t, "first.lan")
	if err := WriteFiles(dir, c, k, false, nil); err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(dir, CertName), filepath.Join(dir, KeyName)
	r, err := NewReloader(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	r.now = func() time.Time { return clock }

	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{GetCertificate: r.GetCertificate, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }() // the listener only serves this test
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }() // the client reads the handshake result
				_ = c.(*tls.Conn).Handshake()    // the client reads the result
			}(conn)
		}
	}()

	if cn := handshakeCN(t, ln.Addr().String()); cn != "first.lan" {
		t.Fatalf("first handshake CN = %q", cn)
	}

	c2, k2 := makePair(t, "second.lan")
	if err := WriteFiles(dir, c2, k2, true, nil); err != nil {
		t.Fatal(err)
	}
	// A new modification time even on a file system with coarse timestamps.
	future := time.Now().Add(time.Hour)
	for _, p := range []string{certFile, keyFile} {
		if err := os.Chtimes(p, future, future); err != nil {
			t.Fatal(err)
		}
	}

	// Inside the check interval the old certificate stays.
	clock = clock.Add(recheckEvery - time.Second)
	if cn := handshakeCN(t, ln.Addr().String()); cn != "first.lan" {
		t.Errorf("handshake inside the interval CN = %q, want first.lan", cn)
	}
	clock = clock.Add(2 * time.Second)
	if cn := handshakeCN(t, ln.Addr().String()); cn != "second.lan" {
		t.Errorf("handshake after the interval CN = %q, want second.lan", cn)
	}
}

func TestReloaderKeepsTheCertificateOnAFailedRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o750); err != nil { //nolint:gosec // a path inside the test directory
		t.Fatal(err)
	}
	c, k := makePair(t, "keep.lan")
	if err := WriteFiles(dir, c, k, false, nil); err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(dir, CertName), filepath.Join(dir, KeyName)
	r, err := NewReloader(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	r.now = func() time.Time { return clock }

	if err := os.WriteFile(certFile, []byte("broken"), 0o644); err != nil { //nolint:gosec // the certificate file is public by design
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(certFile, future, future); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(recheckEvery)
	got, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(got.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Subject.CommonName != "keep.lan" {
		t.Errorf("CN = %q, the old certificate must stay", parsed.Subject.CommonName)
	}
}

func TestNewReloaderFailsOnABadPair(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewReloader(filepath.Join(dir, "no.crt"), filepath.Join(dir, "no.key")); err == nil {
		t.Error("missing files must fail at start")
	}
}

func TestWriteFilesGivesTheFilesToTheOwner(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	c, k := makePair(t, "a.lan")
	// Chown to the own account always works, so the test checks the path without root.
	owner := &Owner{UID: os.Getuid(), GID: os.Getgid()}
	if err := WriteFiles(dir, c, k, false, owner); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, filepath.Join(dir, KeyName)); m != 0o600 {
		t.Errorf("key mode = %o", m)
	}
}
