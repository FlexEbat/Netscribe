package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
	"github.com/FlexEbat/Netscribe/internal/totp"
	"github.com/go-chi/chi/v5"
)

const (
	challengeLife   = 5 * time.Minute
	challengeTries  = 5
	maxChallenges   = 1000
	recoveryCount   = 10
	issuerName      = "Netscribe"
	maxTOTPInputLen = 64
)

// MFAStore keeps the second factor of accounts. *store.Store implements it.
type MFAStore interface {
	GetTOTP(ctx context.Context, userID int64) (store.TOTPRecord, error)
	SaveTOTPPending(ctx context.Context, userID int64, sealed []byte, now time.Time) error
	ConfirmTOTP(ctx context.Context, userID, step int64, now time.Time) (bool, error)
	DeleteTOTP(ctx context.Context, userID int64) error
	AdvanceTOTPStep(ctx context.Context, userID, step int64) (bool, error)
	ReplaceRecoveryCodes(ctx context.Context, userID int64, hashes []string) error
	UnusedRecoveryCodes(ctx context.Context, userID int64) ([]store.RecoveryCode, error)
	UseRecoveryCode(ctx context.Context, id int64, now time.Time) (bool, error)
}

// challenge is a password that was right and a second factor that is still to come.
type challenge struct {
	user    model.User
	ip, ua  string
	expires time.Time
	tries   int
}

// challenges lives in memory: a restart asks for the password again, which costs a person
// a few seconds and an attacker nothing.
type challenges struct {
	mu sync.Mutex
	m  map[string]*challenge
}

func (c *challenges) add(ch *challenge, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*challenge{}
	}
	for k, v := range c.m {
		if now.After(v.expires) {
			delete(c.m, k)
		}
	}
	if len(c.m) >= maxChallenges {
		return "", false
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", false
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	c.m[token] = ch
	return token, true
}

// try returns the challenge for a token and counts one try against it. A token that is
// unknown, expired or used up gives nil.
func (c *challenges) try(token string, now time.Time) *challenge {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := c.m[token]
	if ch == nil {
		return nil
	}
	if now.After(ch.expires) || ch.tries >= challengeTries {
		delete(c.m, token)
		return nil
	}
	ch.tries++
	return ch
}

func (c *challenges) drop(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, token)
}

// checkSecondFactor checks a code or a recovery code of a user. A right code is spent: it
// cannot be used again.
func (s *server) checkSecondFactor(ctx context.Context, userID int64, input string) (bool, error) {
	rec, err := s.mfa.GetTOTP(ctx, userID)
	if errors.Is(err, model.ErrNotFound) || (err == nil && !rec.Confirmed) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	secret, err := s.sealer.Open(rec.SecretSealed)
	if err != nil {
		return false, err
	}
	now := s.now()
	if step, ok := totp.Verify(secret, input, now, rec.LastStep); ok {
		adv, err := s.mfa.AdvanceTOTPStep(ctx, userID, step)
		return adv, err
	}
	code := totp.NormalizeRecovery(input)
	if len(code) != 10 {
		return false, nil
	}
	codes, err := s.mfa.UnusedRecoveryCodes(ctx, userID)
	if err != nil {
		return false, err
	}
	// Every hash is as slow as a password hash, so the count and the time are logged: they
	// show whether a slow recovery sign in comes from here (amendment 53, open item 4).
	began := time.Now()
	checked := 0
	defer func() {
		s.log.Info("second factor check of a recovery code", "hashes", checked, "unused", len(codes), "ms", time.Since(began).Milliseconds())
	}()
	for _, c := range codes {
		checked++
		match, err := s.auth.VerifyRecovery(code, c.Hash)
		if err != nil || !match {
			continue
		}
		used, err := s.mfa.UseRecoveryCode(ctx, c.ID, now)
		return used, err
	}
	return false, nil
}

// loginTOTP answers POST /api/auth/login/totp: the second step of a sign in.
func (s *server) loginTOTP(w http.ResponseWriter, r *http.Request) {
	ip := s.clientIP(r)
	if ok, retry := s.limiter.Allow(limitKey(ip)); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds())+1)))
		writeError(w, http.StatusTooManyRequests, "too many attempts")
		return
	}
	var body struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if !decode(r, &body) || body.Challenge == "" || body.Code == "" || len(body.Code) > maxTOTPInputLen || len(body.Challenge) > 64 {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	ch := s.challenges.try(body.Challenge, s.now())
	if ch == nil || s.auth.SecondFactorLocked(ip, ch.user.Username) {
		s.write(r.Context(), model.AuditEntry{Action: "totp_failed", Result: "denied", IP: ip})
		writeError(w, http.StatusUnauthorized, "the code is not right or has expired: sign in again")
		return
	}
	ok, err := s.checkSecondFactor(r.Context(), ch.user.ID, body.Code)
	if err != nil {
		s.internalError(w, "check second factor", err)
		return
	}
	if !ok {
		s.auth.SecondFactorFailed(ip, ch.user.Username)
		s.write(r.Context(), model.AuditEntry{Action: "totp_failed", Result: "denied", IP: ip, UserID: &ch.user.ID, Username: ch.user.Username})
		writeError(w, http.StatusUnauthorized, "the code is not right or has expired: sign in again")
		return
	}
	s.challenges.drop(body.Challenge)
	s.auth.SecondFactorDone(ip, ch.user.Username)
	sess, err := s.auth.OpenSession(r.Context(), ch.user.ID, ip, r.UserAgent())
	if err != nil {
		s.internalError(w, "open session", err)
		return
	}
	s.limiter.Refund(limitKey(ip))
	s.setSessionCookie(w, r, sess)
	s.write(r.Context(), model.AuditEntry{Action: "login", Result: "ok", IP: ip, UserID: &ch.user.ID, Username: ch.user.Username})
	writeJSON(w, http.StatusOK, newMe(auth.Principal{User: ch.user, CSRFToken: sess.CSRFToken}))
}

// totpStatus answers GET /api/auth/totp.
func (s *server) totpStatus(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	rec, err := s.mfa.GetTOTP(r.Context(), p.User.ID)
	enabled := err == nil && rec.Confirmed
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		s.internalError(w, "totp status", err)
		return
	}
	left := 0
	if enabled {
		codes, err := s.mfa.UnusedRecoveryCodes(r.Context(), p.User.ID)
		if err != nil {
			s.internalError(w, "totp status", err)
			return
		}
		left = len(codes)
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "recoveryCodesLeft": left})
}

// confirmBodyPassword reads {"password"} and checks it.
func (s *server) passwordInBody(w http.ResponseWriter, r *http.Request, password string, action string) bool {
	p, _ := principal(r)
	if password == "" || len(password) > maxPasswordBytes {
		writeError(w, http.StatusBadRequest, "invalid request")
		return false
	}
	switch err := s.auth.ConfirmPassword(r.Context(), p, password); {
	case err == nil:
		return true
	case errors.Is(err, auth.ErrWrongPassword):
		s.record(r, "access_denied", "denied", action+": the password was wrong")
		writeError(w, http.StatusForbidden, "the password is not right")
	default:
		s.internalError(w, "confirm password", err)
	}
	return false
}

// totpSetup answers POST /api/auth/totp/setup: a new secret, not active until a code of it
// is given to /enable.
func (s *server) totpSetup(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	var body struct {
		Password string `json:"password"`
	}
	if !decode(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !s.passwordInBody(w, r, body.Password, "totp setup") {
		return
	}
	if rec, err := s.mfa.GetTOTP(r.Context(), p.User.ID); err == nil && rec.Confirmed {
		writeError(w, http.StatusConflict, "a second factor is already set up: turn it off first")
		return
	}
	secret, err := totp.NewSecret()
	if err != nil {
		s.internalError(w, "totp setup", err)
		return
	}
	sealed, err := s.sealer.Seal(secret)
	if err != nil {
		s.internalError(w, "totp setup", err)
		return
	}
	if err := s.mfa.SaveTOTPPending(r.Context(), p.User.ID, sealed, s.now()); err != nil {
		s.internalError(w, "totp setup", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret": totp.EncodeSecret(secret),
		"uri":    totp.URI(issuerName, p.User.Username, secret),
	})
}

// totpEnable answers POST /api/auth/totp/enable: a code of the new secret turns it on, and
// the recovery codes are shown once.
func (s *server) totpEnable(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	var body struct {
		Code string `json:"code"`
	}
	if !decode(r, &body) || body.Code == "" || len(body.Code) > maxTOTPInputLen {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	rec, err := s.mfa.GetTOTP(r.Context(), p.User.ID)
	if errors.Is(err, model.ErrNotFound) || (err == nil && rec.Confirmed) {
		writeError(w, http.StatusConflict, "there is no setup to finish: start again")
		return
	}
	if err != nil {
		s.internalError(w, "totp enable", err)
		return
	}
	secret, err := s.sealer.Open(rec.SecretSealed)
	if err != nil {
		s.internalError(w, "totp enable", err)
		return
	}
	step, ok := totp.Verify(secret, body.Code, s.now(), 0)
	if !ok {
		s.record(r, "totp_failed", "denied", "setup")
		writeError(w, http.StatusBadRequest, "that code is not right: check the clock of your phone")
		return
	}
	codes, err := totp.RecoveryCodes(recoveryCount)
	if err != nil {
		s.internalError(w, "totp enable", err)
		return
	}
	hashes := make([]string, 0, len(codes))
	for _, c := range codes {
		h, err := s.auth.HashRecovery(c)
		if err != nil {
			s.internalError(w, "totp enable", err)
			return
		}
		hashes = append(hashes, h)
	}
	if err := s.mfa.ReplaceRecoveryCodes(r.Context(), p.User.ID, hashes); err != nil {
		s.internalError(w, "totp enable", err)
		return
	}
	if done, err := s.mfa.ConfirmTOTP(r.Context(), p.User.ID, step, s.now()); err != nil || !done {
		if err != nil {
			s.internalError(w, "totp enable", err)
			return
		}
		writeError(w, http.StatusConflict, "there is no setup to finish: start again")
		return
	}
	s.record(r, "totp_enable", "ok", "")
	writeJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

// totpDisable answers DELETE /api/auth/totp: it needs the password and a current code or a
// recovery code.
func (s *server) totpDisable(w http.ResponseWriter, r *http.Request) {
	p, _ := principal(r)
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(r, &body) || body.Code == "" || len(body.Code) > maxTOTPInputLen {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if !s.passwordInBody(w, r, body.Password, "totp disable") {
		return
	}
	ok, err := s.checkSecondFactor(r.Context(), p.User.ID, body.Code)
	if err != nil {
		s.internalError(w, "totp disable", err)
		return
	}
	if !ok {
		s.record(r, "totp_failed", "denied", "disable")
		writeError(w, http.StatusForbidden, "that code is not right")
		return
	}
	if err := s.mfa.DeleteTOTP(r.Context(), p.User.ID); err != nil {
		s.internalError(w, "totp disable", err)
		return
	}
	s.record(r, "totp_disable", "ok", "")
	w.WriteHeader(http.StatusNoContent)
}

// totpReset answers POST /api/users/{id}/totp/reset: an administrator removes the second
// factor of a person who lost it, and the person's sessions end.
func (s *server) totpReset(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := s.mfa.DeleteTOTP(r.Context(), id); err != nil {
		s.internalError(w, "totp reset", err)
		return
	}
	if s.sessions != nil {
		if _, err := s.sessions.DeleteUserSessionsExcept(r.Context(), id, ""); err != nil {
			s.internalError(w, "totp reset", err)
			return
		}
	}
	s.record(r, "totp_reset", "ok", "user "+strconv.FormatInt(id, 10))
	w.WriteHeader(http.StatusNoContent)
}
