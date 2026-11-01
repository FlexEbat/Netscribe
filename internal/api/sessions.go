package api

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/go-chi/chi/v5"
)

// SessionStore lists and ends the sessions of one user. *store.Store implements it.
type SessionStore interface {
	ListUserSessions(ctx context.Context, userID int64, now time.Time) ([]auth.SessionRecord, error)
	DeleteSessionOf(ctx context.Context, userID int64, idHash string) (bool, error)
	DeleteUserSessionsExcept(ctx context.Context, userID int64, keep string) (int64, error)
}

// sessionView is one session as the account page shows it. The id is a piece of the hash
// of the cookie: it names the session in a request and cannot be turned into the cookie.
type sessionView struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"userAgent"`
	Current    bool      `json:"current"`
}

// publicSessionID is what a session is called outside the server.
func publicSessionID(idHash string) string {
	if len(idHash) > 20 {
		return idHash[:20]
	}
	return idHash
}

// clipAgent keeps the first 120 printable characters of a browser's name, so a long or
// hostile header cannot fill the page.
func clipAgent(s string) string {
	out := make([]rune, 0, 120)
	for _, r := range s {
		if !unicode.IsPrint(r) {
			continue
		}
		out = append(out, r)
		if len(out) == 120 {
			break
		}
	}
	return strings.TrimSpace(string(out))
}

// currentHash is the hash of the cookie of this request, or empty.
func currentHash(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return ""
	}
	return auth.HashID(c.Value)
}

// listSessions answers GET /api/auth/sessions with the caller's own sessions.
func (s *server) listSessions(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	recs, err := s.sessions.ListUserSessions(r.Context(), p.User.ID, s.now())
	if err != nil {
		s.internalError(w, "list sessions", err)
		return
	}
	here := currentHash(r)
	out := make([]sessionView, 0, len(recs))
	for _, rec := range recs {
		out = append(out, sessionView{
			ID: publicSessionID(rec.IDHash), CreatedAt: rec.CreatedAt, LastSeenAt: rec.LastSeenAt,
			IP: rec.IP, UserAgent: clipAgent(rec.UserAgent), Current: rec.IDHash == here,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// revokeSession answers DELETE /api/auth/sessions/{id}. A session of another person is
// not found, the same as one that does not exist.
func (s *server) revokeSession(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	id := chi.URLParam(r, "id")
	recs, err := s.sessions.ListUserSessions(r.Context(), p.User.ID, s.now())
	if err != nil {
		s.internalError(w, "revoke session", err)
		return
	}
	for _, rec := range recs {
		if publicSessionID(rec.IDHash) != id || id == "" {
			continue
		}
		if _, err := s.sessions.DeleteSessionOf(r.Context(), p.User.ID, rec.IDHash); err != nil {
			s.internalError(w, "revoke session", err)
			return
		}
		s.record(r, "session_revoke", "ok", "one session")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusNotFound, "that session was not found")
}

// revokeOtherSessions answers DELETE /api/auth/sessions: every session but this one.
func (s *server) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	here := currentHash(r)
	if here == "" {
		writeError(w, http.StatusBadRequest, "there is no session cookie on this request")
		return
	}
	n, err := s.sessions.DeleteUserSessionsExcept(r.Context(), p.User.ID, here)
	if err != nil {
		s.internalError(w, "revoke sessions", err)
		return
	}
	s.record(r, "session_revoke", "ok", "all other sessions")
	writeJSON(w, http.StatusOK, map[string]int64{"ended": n})
}
