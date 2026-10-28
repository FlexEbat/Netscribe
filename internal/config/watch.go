package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

// reloadDelay lets an editor finish its write-and-rename before the file is read.
const reloadDelay = 200 * time.Millisecond

// Watcher holds the live config and replaces it when the file changes or SIGHUP arrives.
// Only the sections that can change at runtime are taken from the new file: scan, snmp,
// ssh, docker, history and export. A change to any other key needs a restart, and the
// watcher logs a warning instead of applying it, so a security setting such as auth or
// tls never changes under a running server.
type Watcher struct {
	path       string
	allowLoose bool
	log        *slog.Logger
	delay      time.Duration
	cur        atomic.Pointer[Config]

	// watching, when set, is called once the directory watch is in place. Tests use it
	// to know when a file change can no longer be missed.
	watching func()
}

// NewWatcher returns a watcher whose current config is initial, loaded from path.
func NewWatcher(path string, allowLoose bool, initial Config, log *slog.Logger) *Watcher {
	w := &Watcher{path: path, allowLoose: allowLoose, log: log, delay: reloadDelay}
	w.cur.Store(&initial)
	return w
}

// Current returns the config in force. The value is a copy, a running scan keeps the
// one it started with.
func (w *Watcher) Current() Config { return *w.cur.Load() }

// Reload reads the file and, when it is valid, makes it the current config. An invalid
// file leaves the current config in place and returns the reason.
func (w *Watcher) Reload() (Config, error) {
	next, err := Load(w.path, w.allowLoose)
	if err != nil {
		return Config{}, err
	}
	applied, restart := mergeHot(w.Current(), next)
	// The kept keys and the new ones must still agree, for example public scan targets
	// need allowPublicTargets, which only a restart changes.
	if err := applied.Validate(); err != nil {
		return Config{}, err
	}
	for _, key := range restart {
		w.log.Warn("config key changed, restart to apply it", "key", key)
	}
	w.cur.Store(&applied)
	return applied, nil
}

// mergeHot takes the runtime sections from next and everything else from cur. It also
// returns the keys whose new value was not applied.
func mergeHot(cur, next Config) (Config, []string) {
	applied := cur
	applied.Scan, applied.SNMP, applied.SSH = next.Scan, next.SNMP, next.SSH
	applied.Docker, applied.History, applied.Export = next.Docker, next.History, next.Export
	applied.Updates = next.Updates

	var restart []string
	for _, k := range []struct {
		name     string
		old, new any
	}{
		{"listen", cur.Listen, next.Listen},
		{"database", cur.Database, next.Database},
		{"allowPublicTargets", cur.AllowPublicTargets, next.AllowPublicTargets},
		{"audit", cur.Audit, next.Audit},
		{"auth", cur.Auth, next.Auth},
		{"tls", cur.TLS, next.TLS},
		{"trustedProxies", cur.TrustedProxies, next.TrustedProxies},
		{"discovery", cur.Discovery, next.Discovery},
	} {
		if !reflect.DeepEqual(k.old, k.new) {
			restart = append(restart, k.name)
		}
	}
	return applied, restart
}

// Run reloads the config when its file changes and when a signal arrives on hup, until
// ctx ends. It watches the directory, because editors and mv replace the file instead of
// writing into it. reloaded gets the new config, failed the reason a file was refused.
func (w *Watcher) Run(ctx context.Context, hup <-chan os.Signal, reloaded func(Config), failed func(error)) error {
	path, err := filepath.Abs(w.path)
	if err != nil {
		return fmt.Errorf("watch config: %w", err)
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch config: %w", err)
	}
	defer func() {
		if err := fw.Close(); err != nil {
			w.log.Warn("close config watcher", "err", err)
		}
	}()
	if err := fw.Add(filepath.Dir(path)); err != nil {
		return fmt.Errorf("watch config: %w", err)
	}
	if w.watching != nil {
		w.watching()
	}

	apply := func() {
		cfg, err := w.Reload()
		if err != nil {
			w.log.Warn("config not reloaded, the previous one stays", "err", err)
			failed(err)
			return
		}
		w.log.Info("config reloaded", "path", path)
		reloaded(cfg)
	}

	timer := time.NewTimer(w.delay)
	timer.Stop()
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-fw.Events:
			if !ok {
				return errors.New("watch config: event channel closed")
			}
			if filepath.Clean(ev.Name) == path && ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0 {
				timer.Reset(w.delay)
			}
		case err, ok := <-fw.Errors:
			if !ok {
				return errors.New("watch config: error channel closed")
			}
			w.log.Warn("config watcher", "err", err)
		case <-timer.C:
			apply()
		case <-hup:
			timer.Stop()
			apply()
		}
	}
}
