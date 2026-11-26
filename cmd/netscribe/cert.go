package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/FlexEbat/Netscribe/internal/certgen"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/setup"
)

const certUsage = `usage: netscribe cert [--config FILE] <command>

  generate [--name NAME]... [--days N] [--dir DIR] [--force] [--set]
                make tls.crt and tls.key; --set also writes tls.certFile and
                tls.keyFile into the config
  show          print the names, the dates and the fingerprint of the configured certificate
  renew         make a new certificate with the same names and length; a running
                service picks it up without a restart
`

// nameFlag collects every --name.
type nameFlag []string

func (n *nameFlag) String() string     { return strings.Join(*n, ",") }
func (n *nameFlag) Set(v string) error { *n = append(*n, v); return nil }

// runCert makes, shows and renews the self-signed certificate of slice 47.
func runCert(args []string, e streams) error {
	fs := flag.NewFlagSet("cert", flag.ContinueOnError)
	fs.SetOutput(e.err)
	o := baseOptions()
	fs.StringVar(&o.ConfigPath, "config", o.ConfigPath, "path of the config file")
	loose := fs.Bool("allow-loose-permissions", false, "read the config although it is readable by others")
	var names nameFlag
	fs.Var(&names, "name", "host name or address the certificate is for; may be repeated (default: this host)")
	days := fs.Int("days", certgen.DefaultDays, fmt.Sprintf("days the certificate is valid, at most %d", certgen.MaxDays))
	dir := fs.String("dir", "", "directory of the two files (default: tls next to the config)")
	force := fs.Bool("force", false, "replace existing files, keeping the old ones as .previous")
	set := fs.Bool("set", false, "write tls.certFile and tls.keyFile into the config")
	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		_, _ = fmt.Fprint(e.err, certUsage)
		return errors.New("cert needs one command: generate, show or renew")
	}
	switch rest[0] {
	case "generate":
		return certGenerate(e, o, certOptions{names: names, days: *days, dir: *dir, force: *force, set: *set, loose: *loose})
	case "show":
		return certShow(e, o, *loose)
	case "renew":
		return certRenew(e, o, *loose)
	}
	_, _ = fmt.Fprint(e.err, certUsage)
	return fmt.Errorf("unknown command %q", rest[0])
}

type certOptions struct {
	names []string
	days  int
	dir   string
	force bool
	set   bool
	loose bool
}

func certGenerate(e streams, o setup.Options, c certOptions) error {
	listen := ""
	if cfg, err := config.Load(o.ConfigPath, true); err == nil {
		listen = cfg.Listen // without a config the names fall back to every address of the host
	}
	names := c.names
	if len(names) == 0 {
		names = certgen.DefaultNames(listen)
	}
	dir := c.dir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(o.ConfigPath), "tls")
	}
	certPEM, keyPEM, err := certgen.Generate(certgen.Options{Names: names, Days: c.days})
	if err != nil {
		return err
	}
	if err := certgen.WriteFiles(dir, certPEM, keyPEM, c.force, configOwner(o.ConfigPath)); err != nil {
		return err
	}
	info, err := certgen.Describe(certPEM)
	if err != nil {
		return err
	}
	certFile, keyFile := filepath.Join(dir, certgen.CertName), filepath.Join(dir, certgen.KeyName)
	printCertInfo(e, info)
	_, _ = fmt.Fprintf(e.out, "files:       %s, %s\n", certFile, keyFile)
	_, _ = fmt.Fprintln(e.out, "A browser warns about a self-signed certificate until you trust it. Compare the fingerprint with the one it shows.")
	if c.set {
		return setup.SetTLSFiles(context.Background(), setupEnv(e), o, certFile, keyFile)
	}
	_, _ = fmt.Fprintf(e.out, "\nAdd this to the config, then restart the service:\n\ntls:\n  certFile: %s\n  keyFile: %s\n", certFile, keyFile)
	return nil
}

func certShow(e streams, o setup.Options, loose bool) error {
	cfg, err := config.Load(o.ConfigPath, loose)
	if err != nil {
		return err
	}
	if cfg.TLS.CertFile == "" {
		return errors.New("the config sets no tls.certFile: run netscribe cert generate --set")
	}
	data, err := os.ReadFile(cfg.TLS.CertFile)
	if err != nil {
		return fmt.Errorf("read the certificate: %w", err)
	}
	info, err := certgen.Describe(data)
	if err != nil {
		return err
	}
	printCertInfo(e, info)
	return nil
}

func certRenew(e streams, o setup.Options, loose bool) error {
	cfg, err := config.Load(o.ConfigPath, loose)
	if err != nil {
		return err
	}
	if cfg.TLS.CertFile == "" {
		return errors.New("the config sets no tls.certFile: run netscribe cert generate --set")
	}
	data, err := os.ReadFile(cfg.TLS.CertFile)
	if err != nil {
		return fmt.Errorf("read the certificate: %w", err)
	}
	old, err := certgen.Describe(data)
	if err != nil {
		return err
	}
	if !old.SelfSigned {
		return errors.New("the configured certificate is not self-signed: renew it where you got it")
	}
	dir := filepath.Dir(cfg.TLS.CertFile)
	if filepath.Base(cfg.TLS.CertFile) != certgen.CertName || cfg.TLS.KeyFile != filepath.Join(dir, certgen.KeyName) {
		return errors.New("the configured files were not made by netscribe cert generate: make a new pair with --dir and --set")
	}
	days := int(math.Round(old.NotAfter.Sub(old.NotBefore).Hours() / 24))
	days = min(max(days, 1), certgen.MaxDays)
	// The common name stays the first name.
	names := []string{old.CommonName}
	names = append(names, old.Names...)
	certPEM, keyPEM, err := certgen.Generate(certgen.Options{Names: names, Days: days})
	if err != nil {
		return err
	}
	if err := certgen.WriteFiles(dir, certPEM, keyPEM, true, configOwner(o.ConfigPath)); err != nil {
		return err
	}
	info, err := certgen.Describe(certPEM)
	if err != nil {
		return err
	}
	printCertInfo(e, info)
	_, _ = fmt.Fprintln(e.out, "A running service serves the new certificate within a minute; no restart is needed.")
	return nil
}

func printCertInfo(e streams, info certgen.Info) {
	kind := "from a file"
	if info.SelfSigned {
		kind = "self-signed"
	}
	_, _ = fmt.Fprintf(e.out, "subject:     %s (%s)\n", info.Subject, kind)
	_, _ = fmt.Fprintf(e.out, "names:       %s\n", strings.Join(info.Names, ", "))
	_, _ = fmt.Fprintf(e.out, "valid:       %s to %s\n", info.NotBefore.UTC().Format(time.DateOnly), info.NotAfter.UTC().Format(time.DateOnly))
	_, _ = fmt.Fprintf(e.out, "fingerprint: SHA-256 %s\n", info.Fingerprint)
}

// configOwner returns the owner of the config file when root runs the command, so the service
// account that reads the config can read the key too. Anyone else keeps their own files.
func configOwner(path string) *certgen.Owner {
	if os.Geteuid() != 0 {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return &certgen.Owner{UID: int(st.Uid), GID: int(st.Gid)}
}
