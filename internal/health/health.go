// Package health runs the checks the Health page shows. They are the checks of "netscribe
// doctor" that make sense for a service that runs without root: what the install can see
// about itself. A check reads and changes nothing, and none of them prints a secret.
package health

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// Status is the result of one check.
type Status string

const (
	OK   Status = "ok"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Check is one line of the page.
type Check struct {
	ID     string `json:"id"`
	Status Status `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	// Fix is a sentence or a command to copy. The panel never runs it.
	Fix string `json:"fix"`
}

// Probe is everything the checks ask of the machine, so a test can answer instead.
type Probe struct {
	Now       func() time.Time
	Stat      func(path string) (fs.FileInfo, error)
	ReadFile  func(path string) ([]byte, error)
	LookPath  func(name string) (string, error)
	FreeBytes func(path string) (uint64, error)
}

// Input is what the checks look at: the running config, where it was read from, the
// numbers of the overview, and whether the update helper is there.
type Input struct {
	Config       config.Config
	ConfigPath   string
	Overview     model.Overview
	UpdateHelper bool
}

const (
	// checkTimeout is how long one check may take. A check that hangs counts as failed.
	checkTimeout = 5 * time.Second
	lowDisk      = 256 << 20
	criticalDisk = 32 << 20
	certWarnDays = 14
	staleScans   = 3 // scans that may be missed before the page says so
)

// Run runs every check. A check that panics or takes too long ends as a failure and does
// not stop the others.
func Run(ctx context.Context, p Probe, in Input) []Check {
	list := []struct {
		id, title string
		run       func() Check
	}{
		{"config.mode", "Config file permissions", func() Check { return configMode(p, in) }},
		{"disk.free", "Free disk space", func() Check { return diskFree(p, in) }},
		{"scan.targets", "Scan targets", func() Check { return scanTargets(in) }},
		{"scan.recent", "Latest scan", func() Check { return scanRecent(p, in) }},
		{"nmap", "Nmap", func() Check { return nmap(p, in) }},
		{"export.browser", "Browser for PDF export", func() Check { return browser(p, in) }},
		{"docker.socket", "Docker socket", func() Check { return docker(p, in) }},
		{"tls.cert", "TLS certificate", func() Check { return tlsCert(p, in) }},
		{"update.helper", "Updates from the panel", func() Check { return updateHelper(in) }},
	}
	out := make([]Check, 0, len(list))
	for _, c := range list {
		out = append(out, runOne(ctx, c.id, c.title, c.run))
	}
	return out
}

func runOne(ctx context.Context, id, title string, f func() Check) Check {
	done := make(chan Check, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- Check{ID: id, Title: title, Status: Fail, Detail: "the check stopped with an internal error", Fix: "Look at the log of the service."}
			}
		}()
		c := f()
		c.ID, c.Title = id, title
		done <- c
	}()
	select {
	case c := <-done:
		return c
	case <-time.After(checkTimeout):
		return Check{ID: id, Title: title, Status: Fail, Detail: "the check did not finish in 5 seconds", Fix: "Look at the log of the service."}
	case <-ctx.Done():
		return Check{ID: id, Title: title, Status: Fail, Detail: "the check was cancelled"}
	}
}

func ok(detail string) Check { return Check{Status: OK, Detail: detail} }

func configMode(p Probe, in Input) Check {
	info, err := p.Stat(in.ConfigPath)
	if err != nil {
		return Check{Status: Warn, Detail: "the file could not be read here", Fix: "Check that the service account can read " + in.ConfigPath + "."}
	}
	if info.Mode().Perm()&0o007 != 0 {
		return Check{Status: Warn, Detail: "every user on this host can read the file, and it holds secrets",
			Fix: "sudo chmod 640 " + in.ConfigPath}
	}
	return ok("only the owner and the group can read it")
}

func diskFree(p Probe, in Input) Check {
	dir := filepath.Dir(in.Config.Database)
	free, err := p.FreeBytes(dir)
	if err != nil {
		return Check{Status: Warn, Detail: "the free space could not be read", Fix: "Check the state directory by hand."}
	}
	mib := free >> 20
	switch {
	case free < criticalDisk:
		return Check{Status: Fail, Detail: fmt.Sprintf("%d MiB free where the database lives: writes will soon fail", mib), Fix: "Free space, or lower history.keepScans and audit.keepDays."}
	case free < lowDisk:
		return Check{Status: Warn, Detail: fmt.Sprintf("%d MiB free where the database lives", mib), Fix: "Free space, or lower history.keepScans and audit.keepDays."}
	}
	return ok(fmt.Sprintf("%d MiB free where the database lives", mib))
}

func scanTargets(in Input) Check {
	if n := len(in.Config.Scan.Targets); n > 0 {
		return ok(fmt.Sprintf("%d networks are set", n))
	}
	return Check{Status: Warn, Detail: "no network is set, so a scan only sees what this host is connected to", Fix: "Set scan.targets on the Settings page."}
}

func scanRecent(p Probe, in Input) Check {
	last := in.Overview.LastScan
	interval := in.Config.Scan.Interval.Std()
	if last == nil {
		return Check{Status: Warn, Detail: "no scan has run yet", Fix: "Run a scan from the Overview page."}
	}
	if last.Status == "failed" {
		return Check{Status: Warn, Detail: "the latest scan failed", Fix: "Open the scan on the Overview page, and run netscribe doctor on the host."}
	}
	if last.Status == "running" || interval <= 0 {
		return ok("a scan is running, or scheduled scans are off")
	}
	at := last.StartedAt
	if last.FinishedAt != nil {
		at = *last.FinishedAt
	}
	if age := p.Now().Sub(at); age > staleScans*interval {
		return Check{Status: Warn, Detail: fmt.Sprintf("the latest scan was %s ago, and one runs every %s", age.Round(time.Minute), interval), Fix: "Check that the service runs: sudo systemctl status netscribe"}
	}
	return ok("the latest scan finished on time")
}

func nmap(p Probe, in Input) Check {
	if !in.Config.Discovery.Nmap {
		return ok("discovery.nmap is off")
	}
	name := in.Config.Discovery.NmapPath
	if name == "" {
		name = "nmap"
	}
	if _, err := p.LookPath(name); err != nil {
		return Check{Status: Warn, Detail: "discovery.nmap is on but nmap was not found", Fix: "Install nmap, or set discovery.nmapPath."}
	}
	return ok("nmap was found")
}

func browser(p Probe, in Input) Check {
	if path := in.Config.Export.ChromePath; path != "" {
		if _, err := p.Stat(path); err != nil {
			return Check{Status: Warn, Detail: "export.chromePath does not exist", Fix: "Set export.chromePath to a browser, or empty it."}
		}
		return ok("the browser of export.chromePath exists")
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if _, err := p.LookPath(name); err == nil {
			return ok("a browser was found: " + name)
		}
	}
	return ok("no browser was found: PDF export is not available, the other formats are")
}

func docker(p Probe, in Input) Check {
	if !in.Config.Docker.Enabled {
		return ok("docker.enabled is off")
	}
	if _, err := p.Stat(in.Config.Docker.Socket); err != nil {
		return Check{Status: Warn, Detail: "docker.enabled is on but the socket cannot be reached", Fix: "Check docker.socket, and that the service account may use it."}
	}
	return ok("the socket is there")
}

func tlsCert(p Probe, in Input) Check {
	path := in.Config.TLS.CertFile
	if path == "" {
		return ok("the panel does not serve TLS itself")
	}
	data, err := p.ReadFile(path)
	if err != nil {
		return Check{Status: Fail, Detail: "the certificate file cannot be read", Fix: "Check tls.certFile and its permissions."}
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return Check{Status: Fail, Detail: "the certificate file holds no certificate", Fix: "Check tls.certFile."}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Check{Status: Fail, Detail: "the certificate cannot be read", Fix: "Check tls.certFile."}
	}
	left := cert.NotAfter.Sub(p.Now())
	days := int(left.Hours() / 24)
	switch {
	case left <= 0:
		return Check{Status: Fail, Detail: "the certificate expired on " + cert.NotAfter.UTC().Format("2006-01-02"), Fix: "Renew the certificate and restart the service."}
	case days < certWarnDays:
		return Check{Status: Warn, Detail: fmt.Sprintf("the certificate expires in %d days", days), Fix: "Renew the certificate and restart the service."}
	}
	return ok(fmt.Sprintf("the certificate is valid for %d more days", days))
}

func updateHelper(in Input) Check {
	if in.UpdateHelper {
		return ok("the update helper is installed")
	}
	return ok("no update helper: the panel shows the update command instead of a button")
}

// Worst is the most serious status of a list, for the dot in the navigation.
func Worst(list []Check) Status {
	worst := OK
	for _, c := range list {
		switch {
		case c.Status == Fail:
			return Fail
		case c.Status == Warn:
			worst = Warn
		}
	}
	return worst
}
