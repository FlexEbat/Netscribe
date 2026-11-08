package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// FindIdentityUser returns the account linked to an issuer and subject, or ErrNotFound.
func (s *Store) FindIdentityUser(ctx context.Context, issuer, subject string) (model.User, error) {
	row, err := s.q.GetUserByIdentity(ctx, dbgen.GetUserByIdentityParams{Issuer: issuer, Subject: subject})
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("find identity: %w", err)
	}
	return toUser(row)
}

// CreateIdentityUser stores a new account together with its identity in one transaction.
// The account has no password. A taken username or identity is ErrConflict.
func (s *Store) CreateIdentityUser(ctx context.Context, u model.UserRecord, issuer, subject, email string) (model.User, error) {
	created := u.CreatedAt
	if created.IsZero() {
		created = s.now()
	}
	var out model.User
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.InsertUser(ctx, dbgen.InsertUserParams{
			Username:    u.Username,
			DisplayName: u.DisplayName,
			Role:        string(u.Role),
			CreatedAt:   formatTime(created),
		})
		if err != nil {
			return err
		}
		if err := q.InsertIdentity(ctx, dbgen.InsertIdentityParams{
			Issuer: issuer, Subject: subject, UserID: row.ID, Email: email, CreatedAt: formatTime(created),
		}); err != nil {
			return err
		}
		out, err = toUser(row)
		return err
	})
	if isUniqueViolation(err) {
		return model.User{}, ErrConflict
	}
	if err != nil {
		return model.User{}, fmt.Errorf("create identity user: %w", err)
	}
	return out, nil
}
