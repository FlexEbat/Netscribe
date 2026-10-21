package api

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	maxUsernameBytes = 64
	maxPasswordBytes = 1024 // far above the 128 characters allowed, only to bound the work
)

type meResponse struct {
	User        model.User         `json:"user"`
	Permissions []model.Permission `json:"permissions"`
	CSRFToken   string             `json:"csrfToken"`
}

func newMe(p auth.Principal) meResponse {
	return meResponse{User: p.User, Permissions: p.Permissions(), CSRFToken: p.CSRFToken}
}

// decode reads one JSON object and rejects unknown fields and trailing data.
func decode(r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return false
	}
	// Only a clean end of input may follow the object. Any other result, a second
	// value or garbage that does not even parse, means trailing data.
	var extra json.RawMessage
	return errors.Is(dec.Decode(&extra), io.EOF)
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if ok, retry := s.limiter.Allow(limitKey(ip)); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(retry.Seconds())))))
		writeError(w, http.StatusTooManyRequests, "too many attempts")
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(r, &body) || body.Username == "" || body.Password == "" ||
		len(body.Username) > maxUsernameBytes || len(body.Password) > maxPasswordBytes {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	sess, user, err := s.auth.Login(r.Context(), body.Username, body.Password, ip, r.UserAgent())
	if errors.Is(err, auth.ErrInvalidCredentials) {
		s.write(r.Context(), model.AuditEntry{
			Action: "login_failed", Result: "denied", IP: ip,
			Username: oneLine(auth.NormalizeUsername(body.Username), maxUsernameBytes),
		})
		writeError(w, http.StatusUnauthorized, auth.ErrInvalidCredentials.Error())
		return
	}
	if err != nil {
		s.internalError(w, "login", err)
		return
	}

	s.setSessionCookie(w, r, sess)
	s.write(r.Context(), model.AuditEntry{Action: "login", Result: "ok", IP: ip, UserID: &user.ID, Username: user.Username})
	writeJSON(w, http.StatusOK, newMe(auth.Principal{User: user, CSRFToken: sess.CSRFToken}))
}

// limitKey is the key under which sign-in attempts are counted. A client that owns an IPv6
// block can pick any address in it, so the attempts of one /64 count together: otherwise
// one client could fill the limiter with keys and slow every later request down.
func limitKey(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is6() || addr.Is4In6() {
		return ip
	}
	prefix, err := addr.Prefix(64)
	if err != nil {
		return ip
	}
	return prefix.String()
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := s.auth.Logout(r.Context(), c.Value); err != nil {
			s.internalError(w, "logout", err)
			return
		}
	}
	s.record(r, "logout", "ok", "")
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	writeJSON(w, http.StatusOK, newMe(p))
}

func (s *server) changePassword(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decode(r, &body) || body.CurrentPassword == "" || body.NewPassword == "" ||
		len(body.CurrentPassword) > maxPasswordBytes || len(body.NewPassword) > maxPasswordBytes {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	err := s.auth.ChangePassword(r.Context(), p, body.CurrentPassword, body.NewPassword)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrWrongPassword),
		errors.Is(err, auth.ErrPasswordShort),
		errors.Is(err, auth.ErrPasswordLong),
		errors.Is(err, auth.ErrPasswordIsUsername),
		errors.Is(err, auth.ErrPasswordUnchanged):
		s.record(r, "password_change", "denied", err.Error())
		writeError(w, http.StatusBadRequest, err.Error())
		return
	default:
		s.internalError(w, "change password", err)
		return
	}
	s.record(r, "password_change", "ok", "")
	// Every session of the user is gone, this one included.
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, sess auth.Session) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure follows the request scheme: a plain-HTTP deployment cannot use Secure cookies
		Name:     sessionCookie,
		Value:    sess.ID,
		Path:     "/",
		Expires:  sess.ExpiresAt.UTC().Truncate(time.Second),
		HttpOnly: true,
		Secure:   s.isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // see setSessionCookie
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}
