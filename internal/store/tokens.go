package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

// maxActiveTokens is how many unrevoked, unexpired tokens one user may hold (section 9.2).
const maxActiveTokens = 20

// CreateToken stores a token. A user at the limit of active tokens gets ErrConflict.
func (s *Store) CreateToken(ctx context.Context, rec TokenRecord) (model.APIToken, error) {
	var out model.APIToken
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		n, err := q.CountActiveTokens(ctx, dbgen.CountActiveTokensParams{UserID: rec.UserID, ExpiresAt: formatTime(rec.CreatedAt)})
		if err != nil {
			return err
		}
		if n >= maxActiveTokens {
			return ErrConflict
		}
		row, err := q.InsertToken(ctx, dbgen.InsertTokenParams{
			UserID: rec.UserID, Name: rec.Name, Prefix: rec.Prefix, TokenHash: rec.Hash, Scope: rec.Scope,
			CreatedAt: formatTime(rec.CreatedAt), ExpiresAt: formatTime(rec.ExpiresAt),
		})
		if err != nil {
			return err
		}
		out, err = toAPIToken(row)
		return err
	})
	if errors.Is(err, ErrConflict) {
		return model.APIToken{}, ErrConflict
	}
	if err != nil {
		return model.APIToken{}, fmt.Errorf("create token: %w", err)
	}
	return out, nil
}

// GetTokenByHash returns the token row and its owner, or ErrNotFound.
func (s *Store) GetTokenByHash(ctx context.Context, hash string) (TokenRecord, model.User, error) {
	row, err := s.q.GetTokenByHash(ctx, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return TokenRecord{}, model.User{}, ErrNotFound
	}
	if err != nil {
		return TokenRecord{}, model.User{}, fmt.Errorf("get token: %w", err)
	}
	rec, err := toTokenRecord(row)
	if err != nil {
		return TokenRecord{}, model.User{}, err
	}
	user, err := s.GetUser(ctx, row.UserID)
	if err != nil {
		return TokenRecord{}, model.User{}, err
	}
	return rec, user, nil
}

// ListTokens returns the tokens of one user, newest first, never nil.
func (s *Store) ListTokens(ctx context.Context, userID int64) ([]model.APIToken, error) {
	rows, err := s.q.ListTokens(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	out := make([]model.APIToken, 0, len(rows))
	for _, r := range rows {
		t, err := toAPIToken(r)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// RevokeToken revokes a token of the given user. A token of someone else is ErrNotFound.
func (s *Store) RevokeToken(ctx context.Context, id, userID int64) error {
	n, err := s.q.RevokeToken(ctx, dbgen.RevokeTokenParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("revoke token: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchToken records when a token was last used.
func (s *Store) TouchToken(ctx context.Context, id int64, now time.Time) error {
	err := s.q.TouchToken(ctx, dbgen.TouchTokenParams{LastUsedAt: sql.NullString{String: formatTime(now), Valid: true}, ID: id})
	if err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	return nil
}

func toTokenRecord(r dbgen.ApiToken) (TokenRecord, error) {
	created, err := parseTime(r.CreatedAt)
	if err != nil {
		return TokenRecord{}, fmt.Errorf("token %d: created_at: %w", r.ID, err)
	}
	expires, err := parseTime(r.ExpiresAt)
	if err != nil {
		return TokenRecord{}, fmt.Errorf("token %d: expires_at: %w", r.ID, err)
	}
	rec := TokenRecord{
		ID: r.ID, UserID: r.UserID, Name: r.Name, Prefix: r.Prefix, Hash: r.TokenHash, Scope: r.Scope,
		CreatedAt: created, ExpiresAt: expires, Revoked: r.Revoked != 0,
	}
	if r.LastUsedAt.Valid {
		used, err := parseTime(r.LastUsedAt.String)
		if err != nil {
			return TokenRecord{}, fmt.Errorf("token %d: last_used_at: %w", r.ID, err)
		}
		rec.LastUsedAt = &used
	}
	return rec, nil
}

func toAPIToken(r dbgen.ApiToken) (model.APIToken, error) {
	rec, err := toTokenRecord(r)
	if err != nil {
		return model.APIToken{}, err
	}
	return model.APIToken{
		ID: rec.ID, Name: rec.Name, Prefix: rec.Prefix, Scope: rec.Scope, CreatedAt: rec.CreatedAt,
		ExpiresAt: rec.ExpiresAt, LastUsedAt: rec.LastUsedAt, Revoked: rec.Revoked,
	}, nil
}
