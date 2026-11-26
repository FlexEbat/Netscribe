package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/FlexEbat/Netscribe/internal/api"
	"github.com/FlexEbat/Netscribe/internal/certgen"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/setup"
)

// tlsService is the api.TLSService of the panel. It reads the config file, not the running
// config, so the card shows what a restart will serve.
type tlsService struct {
	configPath string
	live       func() config.Config
	env        setup.Env
	opts       setup.Options
	mu         sync.Mutex
}

func newTLSService(configPath string, live func() config.Config) *tlsService {
	o := baseOptions()
	o.ConfigPath = configPath
	return &tlsService{
		configPath: configPath,
		live:       live,
		env:        setup.NewEnv(strings.NewReader(""), io.Discard, false, false),
		opts:       o,
	}
}

func (t *tlsService) Status(context.Context) (api.TLSStatus, error) {
	cfg, err := config.Load(t.configPath, true)
	if err != nil {
		return api.TLSStatus{}, err
	}
	if cfg.TLS.CertFile == "" {
		return api.TLSStatus{Names: []string{}}, nil
	}
	data, err := os.ReadFile(cfg.TLS.CertFile)
	if err != nil {
		return api.TLSStatus{}, fmt.Errorf("read the certificate: %w", err)
	}
	info, err := certgen.Describe(data)
	if err != nil {
		return api.TLSStatus{}, err
	}
	return api.TLSStatus{
		Enabled: true, SelfSigned: info.SelfSigned, Subject: info.Subject, Names: nonNilNames(info.Names),
		NotBefore: info.NotBefore, NotAfter: info.NotAfter, Fingerprint: info.Fingerprint,
	}, nil
}

func (t *tlsService) GenerateSelfSigned(_ context.Context, names []string, days int, replace bool) (api.TLSResult, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	cfg, err := config.Load(t.configPath, true)
	if err != nil {
		return api.TLSResult{}, err
	}
	if cfg.TLS.CertFile != "" && !replace {
		return api.TLSResult{}, api.TLSConflict("a certificate is set already: send replace to make a new one")
	}
	if days < 0 || days > certgen.MaxDays {
		return api.TLSResult{}, api.TLSRefusal(fmt.Sprintf("days must be from 1 to %d", certgen.MaxDays))
	}
	if len(names) == 0 {
		names = certgen.DefaultNames(cfg.Listen)
	}
	clean, err := certgen.NormalizeNames(names)
	if err != nil {
		return api.TLSResult{}, api.TLSRefusal(err.Error())
	}
	if len(clean) == 0 {
		return api.TLSResult{}, api.TLSRefusal(certgen.ErrNoNames.Error())
	}
	certPEM, keyPEM, err := certgen.Generate(certgen.Options{Names: clean, Days: days})
	if err != nil {
		return api.TLSResult{}, err
	}
	dir := filepath.Join(filepath.Dir(t.configPath), "tls")
	if err := certgen.WriteFiles(dir, certPEM, keyPEM, replace, configOwner(t.configPath)); err != nil {
		if errors.Is(err, certgen.ErrExists) {
			return api.TLSResult{}, api.TLSConflict(err.Error())
		}
		return api.TLSResult{}, err
	}
	certFile, keyFile := filepath.Join(dir, certgen.CertName), filepath.Join(dir, certgen.KeyName)
	if err := setup.ConfigureTLS(t.env, t.opts, certFile, keyFile); err != nil {
		return api.TLSResult{}, err
	}
	info, err := certgen.Describe(certPEM)
	if err != nil {
		return api.TLSResult{}, err
	}
	// A renewal of the files the service serves is picked up by the reloader. Anything else,
	// the first https and files at another place, changes at start.
	running := t.live().TLS
	return api.TLSResult{
		Subject: info.Subject, Names: nonNilNames(info.Names), NotAfter: info.NotAfter, Fingerprint: info.Fingerprint,
		NeedRestart: running.CertFile != certFile || running.KeyFile != keyFile,
	}, nil
}

func nonNilNames(n []string) []string {
	if n == nil {
		return []string{}
	}
	return n
}
