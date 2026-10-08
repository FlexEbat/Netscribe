// Command netscribe documents a local network from live scans.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/FlexEbat/Netscribe/internal/api"
	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/store"
	"github.com/FlexEbat/Netscribe/web"
)

const usage = `usage: netscribe <command> [flags]

commands:
  serve   run the web panel

run "netscribe <command> -h" for the flags of a command`

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

func run(args []string, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		if err := serve(args[1:], stderr); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			_, _ = fmt.Fprintf(stderr, "netscribe: %v\n", err)
			return 1
		}
		return 0
	case "-h", "--help", "help":
		_, _ = fmt.Fprintln(stderr, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "netscribe: unknown command %q\n\n%s\n", args[0], usage)
		return 2
	}
}

func serve(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "netscribe.yaml", "path to the config file")
	allowLoose := fs.Bool("allow-loose-permissions", false, "start although the config file is readable by others")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logger := newLogger(stderr)
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath, *allowLoose)
	if err != nil {
		return err
	}
	if insecureListen(cfg) {
		logger.Warn("listening beyond loopback without TLS or a trusted proxy: traffic is not encrypted", "listen", cfg.Listen)
	}

	db, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("close database", "err", err)
		}
	}()

	static, err := web.Dist()
	if err != nil {
		return fmt.Errorf("open web interface: %w", err)
	}
	srv := newServer(cfg, api.NewRouter(api.Options{Static: static}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Listen, "tls", cfg.TLS.CertFile != "")
		if cfg.TLS.CertFile != "" {
			errc <- srv.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
			return
		}
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// newServer applies the timeouts and limits of section 9.5 and the TLS floor of section 9.4.
func newServer(cfg config.Config, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Listen,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
}

// insecureListen reports a listen address outside loopback that has neither
// built-in TLS nor a trusted reverse proxy in front of it.
func insecureListen(cfg config.Config) bool {
	if cfg.TLS.CertFile != "" || len(cfg.TrustedProxies) > 0 {
		return false
	}
	host, _, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return true
	}
	if host == "localhost" {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}

var sensitiveKeyParts = []string{"password", "token", "cookie", "authorization", "community", "communities", "secret"}

// newLogger returns a text logger that masks attributes whose key names a secret.
// It backs up config.Secret for values that never went through that type.
func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			key := strings.ToLower(a.Key)
			for _, part := range sensitiveKeyParts {
				if strings.Contains(key, part) {
					return slog.String(a.Key, "***")
				}
			}
			return a
		},
	}))
}
