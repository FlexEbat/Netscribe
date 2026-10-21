package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	maxBodyBytes = 1 << 20 // 1 MiB

	sessionCookie = "netscribe_session"
	csrfHeader    = "X-CSRF-Token"
)

// csp is the policy of section 9.4: no inline scripts, same-origin only.
const csp = "default-src 'self'; script-src 'self'; style-src 'self'; style-src-attr 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// securityHeaders sets the headers of section 9.4 on every response, errors included.
func (s *server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if s.isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if isAPIPath(r.URL.Path) {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// limitBody caps request bodies at 1 MiB.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// requireUsers answers 503 everywhere except /healthz until the first account exists.
// There is no default password: the operator creates the first administrator on the command line.
func (s *server) requireUsers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		has, err := s.auth.HasUsers(r.Context())
		if err != nil {
			s.internalError(w, "check for users", err)
			return
		}
		if !has {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`no users: run "netscribe user add"` + "\n")) // a failed write to the client has no recovery
			return
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

// principal returns the signed-in user of a request that passed authenticate.
func principal(r *http.Request) (auth.Principal, bool) {
	p, ok := r.Context().Value(ctxKey{}).(auth.Principal)
	return p, ok
}

// authenticate resolves the caller: an access token in the Authorization header, or else
// the session cookie. A request that presents a token is never judged by its cookie.
// Without a valid credential the answer is 401.
func (s *server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if header := r.Header.Get("Authorization"); header != "" {
			scheme, value, found := strings.Cut(header, " ")
			if !found || !strings.EqualFold(scheme, "Bearer") {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			p, err := s.auth.AuthenticateToken(r.Context(), strings.TrimSpace(value))
			if errors.Is(err, auth.ErrUnauthenticated) {
				writeError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			if err != nil {
				s.internalError(w, "authenticate token", err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
			return
		}
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		p, err := s.auth.Authenticate(r.Context(), c.Value)
		if errors.Is(err, auth.ErrUnauthenticated) {
			s.clearSessionCookie(w, r) // a stale cookie only causes more failed requests
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if err != nil {
			s.internalError(w, "authenticate", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

// reauthenticate repeats the sign-in check for a request that is still open. It does not
// refresh the idle timer of a session.
func (s *server) reauthenticate(r *http.Request) (auth.Principal, error) {
	if header := r.Header.Get("Authorization"); header != "" {
		_, value, _ := strings.Cut(header, " ")
		return s.auth.AuthenticateToken(r.Context(), strings.TrimSpace(value))
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	return s.auth.Recheck(r.Context(), c.Value)
}

// csrf requires the session's token on every request that can change state.
func (s *server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			p, _ := principal(r)
			if p.Scope != "" {
				break // a token is sent by a script, not by a browser that could be tricked
			}
			if !auth.CSRFMatches(p.CSRFToken, r.Header.Get(csrfHeader)) {
				s.record(r, "access_denied", "denied", "missing or wrong csrf token")
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// authorize is the only place where a role is compared with a right. It reads
// the right from the route table, so handlers never check roles themselves.
func (s *server) authorize(rt Route, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		if p.Scope != "" && !s.tokenMayCall(p, rt, r.Method) {
			s.record(r, "access_denied", "denied", rt.Method+" "+rt.Path+" with a token")
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		if rt.Permission != permSession {
			if p.User.MustChangePassword {
				writeError(w, http.StatusForbidden, "password change required")
				return
			}
			if !p.Can(rt.Permission) {
				s.record(r, "access_denied", "denied", rt.Method+" "+rt.Path)
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// tokenMayCall applies the limits that hold for every access token. Of the routes that
// need only a sign-in, a token may call GET /api/auth/me and nothing else under /api/auth,
// and a token of scope read never changes anything.
func (s *server) tokenMayCall(p auth.Principal, rt Route, method string) bool {
	if rt.Permission == permSession && (rt.Method != http.MethodGet || rt.Path != "/api/auth/me") {
		return false
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return p.Scope == auth.ScopeWrite
}

// clientIP is the address of the client. X-Forwarded-For counts only when the
// connection comes from a trusted proxy; then the right-most address that is not
// itself a trusted proxy is the client.
func (s *server) clientIP(r *http.Request) string {
	remote := remoteAddr(r)
	if !s.isTrusted(remote) {
		return remote.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break // a malformed hop ends the trustworthy part of the chain
		}
		addr = addr.Unmap()
		if !s.isTrusted(addr) {
			return addr.String()
		}
	}
	return remote.String()
}

// isHTTPS reports whether the client reached us over TLS, directly or through a trusted proxy.
func (s *server) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return s.isTrusted(remoteAddr(r)) && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}

func (s *server) isTrusted(a netip.Addr) bool {
	for _, p := range s.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func remoteAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// record writes an audit entry for the request. A failure to write is logged and
// never changes the response: the audit log must not become a way to deny service.
func (s *server) record(r *http.Request, action, result, detail string) {
	e := model.AuditEntry{Action: action, Result: result, IP: s.clientIP(r), Detail: oneLine(detail, 200)}
	if p, ok := principal(r); ok {
		id := p.User.ID
		e.UserID, e.Username = &id, p.User.Username
	}
	s.write(r.Context(), e)
}

func (s *server) write(ctx context.Context, e model.AuditEntry) {
	if err := s.audit.AddAudit(ctx, e); err != nil {
		s.log.Error("write audit entry", "action", e.Action, "err", err)
	}
}

// oneLine makes untrusted text safe for a single log line.
func oneLine(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= max {
			break
		}
		if r < 0x20 || r == 0x7f {
			r = '?'
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func (s *server) internalError(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
