package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	// TokenPrefix starts every access token, so that one is easy to recognise in a leak.
	TokenPrefix = "ndt_"
	// DefaultTokenDays and MaxTokenDays are the lifetime of a token (section 9.2). A token
	// without an end is never issued.
	DefaultTokenDays = 90
	MaxTokenDays     = 365

	maxTokenNameRunes = 64
	tokenPrefixLen    = 8
	tokenRandomBytes  = 32
)

// Scopes of an access token.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

var (
	ErrTokenNameInvalid = errors.New("token name is invalid")
	ErrTokenScope       = errors.New("scope is unknown")
	ErrScopeAboveRole   = errors.New("scope is above your role")
	ErrTokenLifetime    = errors.New("token lifetime is out of range")
)

// tokenScopeRights is what a scope may do at most. Neither scope holds users:manage,
// audit:read or tokens:manage: a stolen token cannot read the log, manage accounts or
// mint more tokens.
var tokenScopeRights = map[string][]model.Permission{
	ScopeRead:  {model.PermTopologyRead, model.PermExportRun},
	ScopeWrite: {model.PermTopologyRead, model.PermExportRun, model.PermScansRun, model.PermDevicesWrite},
}

// TokenPermissions lists what a token of the given scope may do for an owner with the
// given role: the rights of the scope that the role still holds.
func TokenPermissions(role model.Role, scope string) []model.Permission {
	var out []model.Permission
	for _, p := range tokenScopeRights[scope] {
		if Can(role, p) {
			out = append(out, p)
		}
	}
	return out
}

// Can reports whether the caller holds a right: a session holds what the role holds,
// a token what its scope and the role of its owner both allow.
func (p Principal) Can(perm model.Permission) bool {
	if p.Scope == "" {
		return Can(p.User.Role, perm)
	}
	return slices.Contains(TokenPermissions(p.User.Role, p.Scope), perm)
}

// Permissions lists the rights of the caller.
func (p Principal) Permissions() []model.Permission {
	if p.Scope == "" {
		return Permissions(p.User.Role)
	}
	return TokenPermissions(p.User.Role, p.Scope)
}

// IssueToken creates an access token for owner and returns its value, which is shown
// once and never stored. days is 0 for the default lifetime.
func (s *Service) IssueToken(ctx context.Context, owner model.User, name, scope string, days int) (string, model.APIToken, error) {
	if n := utf8.RuneCountInString(name); n < 1 || n > maxTokenNameRunes {
		return "", model.APIToken{}, ErrTokenNameInvalid
	}
	if scope == "" {
		scope = ScopeRead
	}
	if _, ok := tokenScopeRights[scope]; !ok {
		return "", model.APIToken{}, ErrTokenScope
	}
	if scope == ScopeWrite && !Can(owner.Role, model.PermDevicesWrite) {
		return "", model.APIToken{}, ErrScopeAboveRole
	}
	if days == 0 {
		days = DefaultTokenDays
	}
	if days < 1 || days > MaxTokenDays {
		return "", model.APIToken{}, ErrTokenLifetime
	}

	buf := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", model.APIToken{}, fmt.Errorf("random token: %w", err)
	}
	raw := TokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	now := s.now()
	tok, err := s.repo.CreateToken(ctx, TokenRecord{
		UserID: owner.ID, Name: name, Prefix: raw[:tokenPrefixLen], Hash: HashID(raw), Scope: scope,
		CreatedAt: now, ExpiresAt: now.AddDate(0, 0, days),
	})
	if err != nil {
		return "", model.APIToken{}, err
	}
	return raw, tok, nil
}

// AuthenticateToken resolves the value of an access token. A token that is unknown,
// revoked or expired, or whose owner is disabled, is ErrUnauthenticated.
func (s *Service) AuthenticateToken(ctx context.Context, raw string) (Principal, error) {
	if len(raw) <= len(TokenPrefix) || len(raw) > maxIDLength || raw[:len(TokenPrefix)] != TokenPrefix {
		return Principal{}, ErrUnauthenticated
	}
	rec, user, err := s.repo.GetTokenByHash(ctx, HashID(raw))
	if errors.Is(err, model.ErrNotFound) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, fmt.Errorf("get token: %w", err)
	}
	now := s.now()
	if rec.Revoked || !now.Before(rec.ExpiresAt) || user.Disabled {
		return Principal{}, ErrUnauthenticated
	}
	if rec.LastUsedAt == nil || now.Sub(*rec.LastUsedAt) >= touchInterval {
		_ = s.repo.TouchToken(ctx, rec.ID, now) // one lost timestamp changes nothing
	}
	return Principal{User: user, Scope: rec.Scope}, nil
}

// ListTokens returns the tokens of one user.
func (s *Service) ListTokens(ctx context.Context, userID int64) ([]model.APIToken, error) {
	return s.repo.ListTokens(ctx, userID)
}

// RevokeToken revokes a token of the given user.
func (s *Service) RevokeToken(ctx context.Context, id, userID int64) error {
	return s.repo.RevokeToken(ctx, id, userID)
}

// UpdateUser changes the role or the disabled flag of an account and revokes its sessions.
func (s *Service) UpdateUser(ctx context.Context, id int64, role *string, disabled *bool) (model.User, error) {
	var r *model.Role
	if role != nil {
		parsed, err := ParseRole(*role)
		if err != nil {
			return model.User{}, err
		}
		r = &parsed
	}
	return s.repo.UpdateUser(ctx, id, r, disabled)
}

// GetUser returns one account, or model.ErrNotFound.
func (s *Service) GetUser(ctx context.Context, id int64) (model.User, error) {
	return s.repo.GetUser(ctx, id)
}

// DeleteUser removes an account, its sessions and its tokens.
func (s *Service) DeleteUser(ctx context.Context, id int64) error {
	return s.repo.DeleteUser(ctx, id)
}

// ResetPasswordByID sets a temporary password: the user must change it at the next
// login, and every session of the user is revoked.
func (s *Service) ResetPasswordByID(ctx context.Context, id int64, password string) error {
	u, err := s.repo.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if err := ValidatePassword(password, u.Username, s.cfg.MinPasswordLength); err != nil {
		return err
	}
	hash, err := Hash(password, s.params)
	if err != nil {
		return err
	}
	return s.repo.SetPassword(ctx, id, hash, true)
}
