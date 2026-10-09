// Package api serves the HTTP API and the embedded web interface.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"path"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	// permPublic marks a route that needs no sign-in.
	permPublic model.Permission = "public"
	// permSession marks a route that needs a signed-in user but no particular right.
	permSession model.Permission = "session"
)

// Route is one row of the route table. The table is the only way to register a handler.
type Route struct {
	Method     string
	Path       string
	Permission model.Permission
	Handler    http.HandlerFunc
}

func route(method, path string, perm model.Permission, h http.HandlerFunc) Route {
	return Route{Method: method, Path: path, Permission: perm, Handler: h}
}

// Auditor writes entries of the audit log.
type Auditor interface {
	AddAudit(ctx context.Context, e model.AuditEntry) error
}

// Options configures NewRouter.
type Options struct {
	// Static is the built web interface with index.html at its root.
	Static fs.FS
	// Auth checks sign-ins and sessions. Required.
	Auth *auth.Service
	// Audit receives security events. Required.
	Audit Auditor
	// TrustedProxies are the addresses whose X-Forwarded-For and X-Forwarded-Proto are believed.
	TrustedProxies []netip.Prefix
	// Logger receives server-side errors. Nil discards them.
	Logger *slog.Logger
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
}

type server struct {
	auth    *auth.Service
	audit   Auditor
	trusted []netip.Prefix
	log     *slog.Logger
	limiter *auth.Limiter // sign-in attempts per client address
}

const (
	loginAttemptsPerMinute = 10
)

func (s *server) routes() []Route {
	return []Route{
		route(http.MethodGet, "/healthz", permPublic, healthz),
		route(http.MethodPost, "/api/auth/login", permPublic, s.login),
		route(http.MethodPost, "/api/auth/logout", permSession, s.logout),
		route(http.MethodGet, "/api/auth/me", permSession, s.me),
		route(http.MethodPut, "/api/auth/password", permSession, s.changePassword),
	}
}

// NewRouter builds the HTTP handler for the whole service.
func NewRouter(opts Options) http.Handler {
	s := newServer(opts)
	return s.handler(opts.Static, s.routes())
}

func newServer(opts Options) *server {
	if opts.Auth == nil || opts.Audit == nil {
		panic("api: Options.Auth and Options.Audit are required")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &server{
		auth:    opts.Auth,
		audit:   opts.Audit,
		trusted: opts.TrustedProxies,
		log:     log,
		limiter: auth.NewLimiter(loginAttemptsPerMinute, time.Minute, opts.Now),
	}
}

// handler assembles the chain: security headers, body limit, the no-users gate, then
// per route: authentication, CSRF, permission check, handler.
func (s *server) handler(static fs.FS, table []Route) http.Handler {
	r := chi.NewRouter()
	r.Use(s.securityHeaders, limitBody, s.requireUsers)
	s.register(r, table)
	r.NotFound(notFound(static))
	return r
}

// register adds the routes to r and panics on a route without a permission.
// Failing at startup keeps "deny by default" from depending on code review.
func (s *server) register(r chi.Router, table []Route) {
	for _, rt := range table {
		if rt.Permission == "" {
			panic(fmt.Sprintf("api: route %s %s has no permission", rt.Method, rt.Path))
		}
		var h http.Handler = rt.Handler
		if rt.Permission != permPublic {
			h = s.authenticate(s.csrf(s.authorize(rt, h)))
		}
		r.Method(rt.Method, rt.Path, h)
	}
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n")) // a failed write to the client has no recovery
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // a failed write to the client has no recovery
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// notFound answers unknown API paths with JSON and everything else with the web
// interface, so React Router can resolve client-side routes.
func notFound(static fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		// path.Clean on a rooted path removes every ".." segment, and fs.ValidPath
		// refuses anything that could still leave the embedded tree.
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && name != "index.html" && fs.ValidPath(name) {
			if info, err := fs.Stat(static, name); err == nil && !info.IsDir() {
				http.ServeFileFS(w, r, static, name) //nolint:gosec // name is cleaned and checked with fs.ValidPath above
				return
			}
		}
		serveIndex(w, r, static)
	}
}

func isAPIPath(p string) bool { return p == "/api" || strings.HasPrefix(p, "/api/") }

// serveIndex writes index.html without the redirect http.ServeFileFS applies to /index.html URLs.
func serveIndex(w http.ResponseWriter, r *http.Request, static fs.FS) {
	data, err := fs.ReadFile(static, "index.html")
	if err != nil {
		http.Error(w, "web interface is not built: run make build", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data) // a failed write to the client has no recovery
}
