package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	touchInterval = time.Minute // last_seen_at is written at most this often per session
	maxIDLength   = 128         // longer cookie values cannot be ours
)

var (
	// ErrInvalidCredentials covers a wrong password, an unknown user and a locked
	// or disabled account alike: the caller must not be able to tell them apart.
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUnauthenticated    = errors.New("authentication required")
	ErrWrongPassword      = errors.New("current password is incorrect")
)

// SessionRecord is a row of the sessions table. The cookie value itself is never stored.
type SessionRecord struct {
	IDHash     string
	UserID     int64
	CSRFToken  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	IP         string
	UserAgent  string
}

// Repo is the part of the data layer the auth package uses.
type Repo interface {
	CountUsers(ctx context.Context) (int, error)
	CreateUser(ctx context.Context, u model.UserRecord) (model.User, error) // model.ErrConflict
	GetUserByName(ctx context.Context, username string) (model.UserRecord, error)
	ListUsers(ctx context.Context) ([]model.User, error)
	UpdateUser(ctx context.Context, id int64, role *model.Role, disabled *bool) (model.User, error)
	SetPassword(ctx context.Context, id int64, hash string, mustChange bool) error // revokes the user's sessions
	RecordLogin(ctx context.Context, id int64, ok bool, lockAfter int, lockFor time.Duration) error

	CreateSession(ctx context.Context, s SessionRecord) error
	GetSession(ctx context.Context, idHash string) (SessionRecord, model.User, error) // model.ErrNotFound
	TouchSession(ctx context.Context, idHash string, now time.Time) error
	DeleteSession(ctx context.Context, idHash string) error
}

// Session is what a successful login hands to the client.
type Session struct {
	ID        string // the cookie value, shown once
	CSRFToken string
	ExpiresAt time.Time
}

// Principal is an authenticated request's identity.
type Principal struct {
	User      model.User
	CSRFToken string
}

// NewUser describes an account to create.
type NewUser struct {
	Username           string
	DisplayName        string
	Role               model.Role
	Password           string
	MustChangePassword bool
}

// Service implements login, session checks and password changes.
type Service struct {
	repo     Repo
	cfg      config.AuthConfig
	now      func() time.Time
	params   Params
	dummy    string // hash checked for unknown users, so timing does not reveal them
	hasUsers atomic.Bool
}

// NewService returns a service. A nil now means time.Now.
func NewService(repo Repo, cfg config.AuthConfig, now func() time.Time) (*Service, error) {
	if now == nil {
		now = time.Now
	}
	params := Params{MemoryKiB: cfg.Argon2.MemoryKiB, Iterations: cfg.Argon2.Iterations, Parallelism: cfg.Argon2.Parallelism}
	dummy, err := Hash(randomToken(), params)
	if err != nil {
		return nil, err
	}
	return &Service{repo: repo, cfg: cfg, now: now, params: params, dummy: dummy}, nil
}

// HasUsers reports whether at least one account exists. Accounts are never all
// removed once created (the last administrator is protected), so a positive answer is cached.
func (s *Service) HasUsers(ctx context.Context) (bool, error) {
	if s.hasUsers.Load() {
		return true, nil
	}
	n, err := s.repo.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 {
		s.hasUsers.Store(true)
	}
	return n > 0, nil
}

// CreateUser validates and stores a new account.
func (s *Service) CreateUser(ctx context.Context, in NewUser) (model.User, error) {
	name := NormalizeUsername(in.Username)
	if err := ValidateUsername(name); err != nil {
		return model.User{}, err
	}
	role, err := ParseRole(string(in.Role))
	if err != nil {
		return model.User{}, err
	}
	if err := ValidatePassword(in.Password, name, s.cfg.MinPasswordLength); err != nil {
		return model.User{}, err
	}
	hash, err := Hash(in.Password, s.params)
	if err != nil {
		return model.User{}, err
	}
	u, err := s.repo.CreateUser(ctx, model.UserRecord{
		User:         model.User{Username: name, DisplayName: in.DisplayName, Role: role, MustChangePassword: in.MustChangePassword},
		PasswordHash: hash,
	})
	if err != nil {
		return model.User{}, err
	}
	s.hasUsers.Store(true)
	return u, nil
}

// ResetPassword sets a new password for a named account and revokes its sessions.
func (s *Service) ResetPassword(ctx context.Context, username, password string, mustChange bool) error {
	rec, err := s.repo.GetUserByName(ctx, NormalizeUsername(username))
	if err != nil {
		return err
	}
	if err := ValidatePassword(password, rec.Username, s.cfg.MinPasswordLength); err != nil {
		return err
	}
	hash, err := Hash(password, s.params)
	if err != nil {
		return err
	}
	return s.repo.SetPassword(ctx, rec.ID, hash, mustChange)
}

// SetDisabled enables or disables a named account. Disabling revokes its sessions.
func (s *Service) SetDisabled(ctx context.Context, username string, disabled bool) (model.User, error) {
	rec, err := s.repo.GetUserByName(ctx, NormalizeUsername(username))
	if err != nil {
		return model.User{}, err
	}
	return s.repo.UpdateUser(ctx, rec.ID, nil, &disabled)
}

// ListUsers returns every account.
func (s *Service) ListUsers(ctx context.Context) ([]model.User, error) {
	return s.repo.ListUsers(ctx)
}

// Login checks the credentials and opens a session with a fresh identifier.
func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (Session, model.User, error) {
	now := s.now()
	rec, err := s.repo.GetUserByName(ctx, NormalizeUsername(username))
	if errors.Is(err, model.ErrNotFound) {
		s.spendTime(password)
		return Session{}, model.User{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, model.User{}, fmt.Errorf("find user: %w", err)
	}
	if rec.Disabled || (rec.LockedUntil != nil && now.Before(*rec.LockedUntil)) {
		s.spendTime(password)
		return Session{}, model.User{}, ErrInvalidCredentials
	}

	ok, err := Verify(password, rec.PasswordHash)
	if err != nil {
		return Session{}, model.User{}, fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		if err := s.repo.RecordLogin(ctx, rec.ID, false, s.cfg.MaxFailedLogins, s.cfg.LockoutDuration.Std()); err != nil {
			return Session{}, model.User{}, fmt.Errorf("record failed login: %w", err)
		}
		return Session{}, model.User{}, ErrInvalidCredentials
	}
	if err := s.repo.RecordLogin(ctx, rec.ID, true, s.cfg.MaxFailedLogins, s.cfg.LockoutDuration.Std()); err != nil {
		return Session{}, model.User{}, fmt.Errorf("record login: %w", err)
	}
	if NeedsRehash(rec.PasswordHash, s.params) {
		// Best effort: a failed upgrade must not block a correct login.
		if hash, err := Hash(password, s.params); err == nil {
			_ = s.repo.SetPassword(ctx, rec.ID, hash, rec.MustChangePassword)
		}
	}

	sess, err := s.openSession(ctx, rec.ID, ip, userAgent, now)
	if err != nil {
		return Session{}, model.User{}, err
	}
	return sess, rec.User, nil
}

// spendTime burns the cost of one password check, so a missing, locked or disabled
// account answers as slowly as a wrong password.
func (s *Service) spendTime(password string) {
	_, _ = Verify(password, s.dummy) // the result is irrelevant, the dummy hash is well-formed
}

func (s *Service) openSession(ctx context.Context, userID int64, ip, userAgent string, now time.Time) (Session, error) {
	id, csrf := randomToken(), randomToken()
	err := s.repo.CreateSession(ctx, SessionRecord{
		IDHash:     HashID(id),
		UserID:     userID,
		CSRFToken:  csrf,
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(s.cfg.SessionMaxAge.Std()),
		IP:         ip,
		UserAgent:  truncate(userAgent, 256),
	})
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return Session{ID: id, CSRFToken: csrf, ExpiresAt: now.Add(s.cfg.SessionMaxAge.Std())}, nil
}

// Authenticate resolves a cookie value to its principal. A session that is unknown,
// expired (absolute or idle) or owned by a disabled account is ErrUnauthenticated.
func (s *Service) Authenticate(ctx context.Context, rawID string) (Principal, error) {
	if rawID == "" || len(rawID) > maxIDLength {
		return Principal{}, ErrUnauthenticated
	}
	hash := HashID(rawID)
	rec, user, err := s.repo.GetSession(ctx, hash)
	if errors.Is(err, model.ErrNotFound) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("get session: %w", err)
	}
	now := s.now()
	if !now.Before(rec.ExpiresAt) || now.Sub(rec.LastSeenAt) > s.cfg.SessionIdleTimeout.Std() {
		_ = s.repo.DeleteSession(ctx, hash) // an expired row is garbage, failing to remove it changes nothing
		return Principal{}, ErrUnauthenticated
	}
	if user.Disabled {
		return Principal{}, ErrUnauthenticated
	}
	if now.Sub(rec.LastSeenAt) >= touchInterval {
		_ = s.repo.TouchSession(ctx, hash, now) // losing one idle refresh is harmless
	}
	return Principal{User: user, CSRFToken: rec.CSRFToken}, nil
}

// Logout ends the session behind a cookie value.
func (s *Service) Logout(ctx context.Context, rawID string) error {
	if rawID == "" || len(rawID) > maxIDLength {
		return nil
	}
	return s.repo.DeleteSession(ctx, HashID(rawID))
}

// ChangePassword replaces the caller's password. It revokes all of the user's
// sessions, the current one included. A wrong current password counts as a failed
// login, so a stolen session cannot be used to guess it without limit.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next string) error {
	rec, err := s.repo.GetUserByName(ctx, p.User.Username)
	if err != nil {
		return fmt.Errorf("find user: %w", err)
	}
	if rec.LockedUntil != nil && s.now().Before(*rec.LockedUntil) {
		s.spendTime(current)
		return ErrWrongPassword
	}
	ok, err := Verify(current, rec.PasswordHash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		if err := s.repo.RecordLogin(ctx, rec.ID, false, s.cfg.MaxFailedLogins, s.cfg.LockoutDuration.Std()); err != nil {
			return fmt.Errorf("record failed attempt: %w", err)
		}
		return ErrWrongPassword
	}
	if err := ValidatePassword(next, rec.Username, s.cfg.MinPasswordLength); err != nil {
		return err
	}
	hash, err := Hash(next, s.params)
	if err != nil {
		return err
	}
	return s.repo.SetPassword(ctx, rec.ID, hash, false)
}

// CSRFMatches compares a submitted token with the session's in constant time.
func CSRFMatches(want, got string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// HashID is the stored form of a session identifier: hex SHA-256 of the cookie value.
func HashID(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// randomToken returns 32 random bytes as base64url. A failing system random
// source leaves nothing safe to do, so it panics, as crypto/rand itself now does.
func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("auth: system random source failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
