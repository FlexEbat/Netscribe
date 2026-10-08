// Package api serves the HTTP API and the embedded web interface.
package api

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// permPublic marks a route that needs no sign-in.
const permPublic model.Permission = "public"

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

func routes() []Route {
	return []Route{
		route(http.MethodGet, "/healthz", permPublic, healthz),
	}
}

// register adds the routes to r and panics on a route without a permission.
// Failing at startup keeps "deny by default" from depending on code review.
func register(r chi.Router, table []Route) {
	for _, rt := range table {
		if rt.Permission == "" {
			panic(fmt.Sprintf("api: route %s %s has no permission", rt.Method, rt.Path))
		}
		h := rt.Handler
		if rt.Permission != permPublic {
			// Sessions and roles arrive in slice 2. Until then a protected route stays closed.
			h = denyAll
		}
		r.Method(rt.Method, rt.Path, h)
	}
}

// Options configures NewRouter.
type Options struct {
	// Static is the built web interface with index.html at its root.
	Static fs.FS
}

// NewRouter builds the HTTP handler for the whole service.
func NewRouter(opts Options) http.Handler {
	r := chi.NewRouter()
	r.Use(securityHeaders, limitBody)
	register(r, routes())
	r.NotFound(notFound(opts.Static))
	return r
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n")) // a failed write to the client has no recovery
}

func denyAll(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusUnauthorized, "authentication required")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg}) // a failed write to the client has no recovery
}

// notFound answers unknown API paths with JSON and everything else with the web
// interface, so React Router can resolve client-side routes.
func notFound(static fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
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
		if _, err := fs.Stat(static, "index.html"); err != nil {
			http.Error(w, "web interface is not built: run make build", http.StatusServiceUnavailable)
			return
		}
		serveIndex(w, r, static)
	}
}

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
