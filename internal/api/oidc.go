package api

import (
	"context"
	"net/http"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	oidcCookie   = "netscribe_oidc"
	oidcFlowLife = 10 * time.Minute
	oidcCookieAt = "/api/auth/oidc/"
	// ssoFailedPath is where every refusal and every error of a single sign-on ends: one
	// address, so a person learns nothing from the way it failed.
	ssoFailedPath = "/?sso=failed"
)

// OIDCService is the single sign-on of the config. *auth.OIDC implements it.
type OIDCService interface {
	Name() string
	Begin(ctx context.Context) (string, auth.Flow, error)
	Complete(ctx context.Context, f auth.Flow, state, code, ip, userAgent string) (auth.OIDCResult, error)
}

// providers tells the sign-in page which buttons to draw. It names no address.
func (s *server) providers(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{}
	if s.oidc != nil {
		out["oidc"] = map[string]string{"name": s.oidc.Name()}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) oidcLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ip := s.clientIP(r)
	ok, _ := s.limiter.Allow(limitKey(ip))
	s.limiter.Refund(limitKey(ip)) // starting is not a guess: only an address that is already limited is refused
	if !ok {
		http.Redirect(w, r, ssoFailedPath, http.StatusSeeOther)
		return
	}
	target, flow, err := s.oidc.Begin(r.Context())
	if err != nil {
		s.log.Error("single sign-on: begin", "err", err)
		s.write(r.Context(), model.AuditEntry{Action: "oidc_login_failed", Result: "error", IP: ip, Detail: "provider"})
		http.Redirect(w, r, ssoFailedPath, http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure follows the request scheme, see setSessionCookie
		Name:     oidcCookie,
		Value:    flow.Encode(),
		Path:     oidcCookieAt,
		MaxAge:   int(oidcFlowLife / time.Second),
		HttpOnly: true,
		Secure:   s.isHTTPS(r),
		SameSite: http.SameSiteLaxMode, // the return from the provider is a cross-site navigation
	})
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *server) clearOIDCCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // see oidcLogin
		Name:     oidcCookie,
		Value:    "",
		Path:     oidcCookieAt,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// oidcCallback takes no address to go to afterwards: a person lands on / and nowhere else,
// so the redirect cannot be turned into an open one.
func (s *server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ip := s.clientIP(r)
	s.clearOIDCCookie(w, r) // one try per cookie, whatever the outcome
	fail := func(reason string) {
		// The page says the same to everyone; the operator reads the reason here and in the audit log.
		s.log.Warn("single sign-on refused", "reason", reason)
		s.write(r.Context(), model.AuditEntry{Action: "oidc_login_failed", Result: "denied", IP: ip, Detail: reason})
		http.Redirect(w, r, ssoFailedPath, http.StatusSeeOther)
	}

	// A failed return counts as a guess; a good one is given back below.
	if ok, _ := s.limiter.Allow(limitKey(ip)); !ok {
		http.Redirect(w, r, ssoFailedPath, http.StatusSeeOther)
		return
	}
	c, err := r.Cookie(oidcCookie)
	if err != nil {
		fail("no cookie")
		return
	}
	flow, ok := auth.ParseFlow(c.Value)
	if !ok {
		fail("bad cookie")
		return
	}
	q := r.URL.Query()
	if q.Get("error") != "" {
		fail("provider refused")
		return
	}
	res, err := s.oidc.Complete(r.Context(), flow, q.Get("state"), q.Get("code"), ip, r.UserAgent())
	if err != nil {
		reason := auth.Reason(err)
		if reason == "" {
			s.log.Error("single sign-on: complete", "err", err)
			reason = "error"
		}
		fail(reason)
		return
	}
	s.limiter.Refund(limitKey(ip))
	if res.Created {
		s.write(r.Context(), model.AuditEntry{Action: "oidc_user_create", Result: "ok", IP: ip, UserID: &res.User.ID, Username: res.User.Username})
	}
	s.setSessionCookie(w, r, res.Session)
	s.write(r.Context(), model.AuditEntry{Action: "oidc_login", Result: "ok", IP: ip, UserID: &res.User.ID, Username: res.User.Username})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
