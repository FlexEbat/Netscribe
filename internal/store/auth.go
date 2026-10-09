package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
	dbgen "github.com/FlexEbat/Netscribe/internal/store/db"
)

const (
	maxUserSessions  = 10 // section 9.2: the oldest session is evicted beyond this
	defaultAuditRows = 50
	maxAuditRows     = 500
)

// inTx runs fn in a transaction and commits when fn returns nil.
func (s *Store) inTx(ctx context.Context, fn func(q *dbgen.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // after Commit this is a no-op
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func parseNullTime(ns sql.NullString) (*time.Time, error) {
	if !ns.Valid {
		return nil, nil
	}
	t, err := parseTime(ns.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func toUserRecord(r dbgen.User) (model.UserRecord, error) {
	created, err := parseTime(r.CreatedAt)
	if err != nil {
		return model.UserRecord{}, fmt.Errorf("user %d: created_at: %w", r.ID, err)
	}
	last, err := parseNullTime(r.LastLoginAt)
	if err != nil {
		return model.UserRecord{}, fmt.Errorf("user %d: last_login_at: %w", r.ID, err)
	}
	locked, err := parseNullTime(r.LockedUntil)
	if err != nil {
		return model.UserRecord{}, fmt.Errorf("user %d: locked_until: %w", r.ID, err)
	}
	return model.UserRecord{
		User: model.User{
			ID:                 r.ID,
			Username:           r.Username,
			DisplayName:        r.DisplayName,
			Role:               model.Role(r.Role),
			Disabled:           r.Disabled != 0,
			MustChangePassword: r.MustChangePassword != 0,
			CreatedAt:          created,
			LastLoginAt:        last,
		},
		PasswordHash: r.PasswordHash,
		FailedLogins: int(r.FailedLogins),
		LockedUntil:  locked,
	}, nil
}

func toUser(r dbgen.User) (model.User, error) {
	rec, err := toUserRecord(r)
	return rec.User, err
}

// CountUsers returns the number of accounts.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	n, err := s.q.CountUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return int(n), nil
}

// CreateUser stores a new account. A name that exists in any letter case is ErrConflict.
func (s *Store) CreateUser(ctx context.Context, u model.UserRecord) (model.User, error) {
	created := u.CreatedAt
	if created.IsZero() {
		created = s.now()
	}
	row, err := s.q.InsertUser(ctx, dbgen.InsertUserParams{
		Username:           u.Username,
		DisplayName:        u.DisplayName,
		PasswordHash:       u.PasswordHash,
		Role:               string(u.Role),
		Disabled:           boolToInt(u.Disabled),
		MustChangePassword: boolToInt(u.MustChangePassword),
		CreatedAt:          formatTime(created),
	})
	if isUniqueViolation(err) {
		return model.User{}, ErrConflict
	}
	if err != nil {
		return model.User{}, fmt.Errorf("create user: %w", err)
	}
	return toUser(row)
}

// GetUserByName returns the account with its secrets. The name is matched without regard to case.
func (s *Store) GetUserByName(ctx context.Context, username string) (model.UserRecord, error) {
	row, err := s.q.GetUserByName(ctx, username)
	if errors.Is(err, sql.ErrNoRows) {
		return model.UserRecord{}, ErrNotFound
	}
	if err != nil {
		return model.UserRecord{}, fmt.Errorf("get user: %w", err)
	}
	return toUserRecord(row)
}

// GetUser returns one account.
func (s *Store) GetUser(ctx context.Context, id int64) (model.User, error) {
	row, err := s.q.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("get user: %w", err)
	}
	return toUser(row)
}

// ListUsers returns every account ordered by name.
func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	out := make([]model.User, 0, len(rows))
	for _, r := range rows {
		u, err := toUser(r)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

// UpdateUser changes the role and/or the disabled flag. Either one revokes the user's
// sessions. The last enabled administrator can be neither demoted nor disabled.
func (s *Store) UpdateUser(ctx context.Context, id int64, role *model.Role, disabled *bool) (model.User, error) {
	if role != nil && !validRole(*role) {
		return model.User{}, errors.New("update user: role is unknown")
	}
	var out model.User
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetUserByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		newRole, newDisabled := model.Role(row.Role), row.Disabled != 0
		if role != nil {
			newRole = *role
		}
		if disabled != nil {
			newDisabled = *disabled
		}

		wasEnabledAdmin := model.Role(row.Role) == model.RoleAdmin && row.Disabled == 0
		staysEnabledAdmin := newRole == model.RoleAdmin && !newDisabled
		if wasEnabledAdmin && !staysEnabledAdmin {
			n, err := q.CountEnabledAdmins(ctx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}

		changed := string(newRole) != row.Role || newDisabled != (row.Disabled != 0)
		if changed {
			if err := q.SetUserRoleAndDisabled(ctx, dbgen.SetUserRoleAndDisabledParams{
				Role: string(newRole), Disabled: boolToInt(newDisabled), ID: id,
			}); err != nil {
				return err
			}
			if err := q.DeleteUserSessions(ctx, id); err != nil {
				return err
			}
		}
		row.Role, row.Disabled = string(newRole), boolToInt(newDisabled)
		out, err = toUser(row)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLastAdmin) {
			return model.User{}, err
		}
		return model.User{}, fmt.Errorf("update user: %w", err)
	}
	return out, nil
}

func validRole(r model.Role) bool {
	return r == model.RoleViewer || r == model.RoleOperator || r == model.RoleAdmin
}

// SetPassword replaces the hash, clears the lockout and revokes every session of the user.
func (s *Store) SetPassword(ctx context.Context, id int64, hash string, mustChange bool) error {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		n, err := q.SetUserPassword(ctx, dbgen.SetUserPasswordParams{
			PasswordHash: hash, MustChangePassword: boolToInt(mustChange), ID: id,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		return q.DeleteUserSessions(ctx, id)
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	return nil
}

// RecordLogin updates the failure counter. After lockAfter failures in a row the
// account is locked for lockFor and the counter starts again. A success clears both.
func (s *Store) RecordLogin(ctx context.Context, id int64, ok bool, lockAfter int, lockFor time.Duration) error {
	now := s.now()
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		if ok {
			return q.RecordLoginSuccess(ctx, dbgen.RecordLoginSuccessParams{
				LastLoginAt: sql.NullString{String: formatTime(now), Valid: true}, ID: id,
			})
		}
		row, err := q.GetUserByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		failed := row.FailedLogins + 1
		locked := row.LockedUntil
		if lockAfter > 0 && failed >= int64(lockAfter) {
			failed = 0
			locked = sql.NullString{String: formatTime(now.Add(lockFor)), Valid: true}
		}
		return q.RecordLoginFailure(ctx, dbgen.RecordLoginFailureParams{FailedLogins: failed, LockedUntil: locked, ID: id})
	})
	if errors.Is(err, ErrNotFound) {
		return err
	}
	if err != nil {
		return fmt.Errorf("record login: %w", err)
	}
	return nil
}

// DeleteUser removes an account and, by cascade, its sessions.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetUserByID(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if model.Role(row.Role) == model.RoleAdmin && row.Disabled == 0 {
			n, err := q.CountEnabledAdmins(ctx)
			if err != nil {
				return err
			}
			if n <= 1 {
				return ErrLastAdmin
			}
		}
		_, err = q.DeleteUser(ctx, id)
		return err
	})
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrLastAdmin) {
		return err
	}
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// CreateSession stores a session and evicts the user's oldest ones beyond ten.
func (s *Store) CreateSession(ctx context.Context, rec SessionRecord) error {
	err := s.inTx(ctx, func(q *dbgen.Queries) error {
		if err := q.InsertSession(ctx, dbgen.InsertSessionParams{
			IDHash:     rec.IDHash,
			UserID:     rec.UserID,
			CsrfToken:  rec.CSRFToken,
			CreatedAt:  formatTime(rec.CreatedAt),
			LastSeenAt: formatTime(rec.LastSeenAt),
			ExpiresAt:  formatTime(rec.ExpiresAt),
			Ip:         rec.IP,
			UserAgent:  rec.UserAgent,
		}); err != nil {
			return err
		}
		return q.TrimUserSessions(ctx, dbgen.TrimUserSessionsParams{OwnerID: rec.UserID, Keep: maxUserSessions})
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// GetSession returns a session with its owner, or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, idHash string) (SessionRecord, model.User, error) {
	row, err := s.q.GetSession(ctx, idHash)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionRecord{}, model.User{}, ErrNotFound
	}
	if err != nil {
		return SessionRecord{}, model.User{}, fmt.Errorf("get session: %w", err)
	}
	created, err := parseTime(row.CreatedAt)
	if err != nil {
		return SessionRecord{}, model.User{}, fmt.Errorf("session: created_at: %w", err)
	}
	seen, err := parseTime(row.LastSeenAt)
	if err != nil {
		return SessionRecord{}, model.User{}, fmt.Errorf("session: last_seen_at: %w", err)
	}
	expires, err := parseTime(row.ExpiresAt)
	if err != nil {
		return SessionRecord{}, model.User{}, fmt.Errorf("session: expires_at: %w", err)
	}
	user, err := s.GetUser(ctx, row.UserID)
	if err != nil {
		return SessionRecord{}, model.User{}, err
	}
	return SessionRecord{
		IDHash: row.IDHash, UserID: row.UserID, CSRFToken: row.CsrfToken,
		CreatedAt: created, LastSeenAt: seen, ExpiresAt: expires, IP: row.Ip, UserAgent: row.UserAgent,
	}, user, nil
}

// TouchSession records activity.
func (s *Store) TouchSession(ctx context.Context, idHash string, now time.Time) error {
	if err := s.q.TouchSession(ctx, dbgen.TouchSessionParams{LastSeenAt: formatTime(now), IDHash: idHash}); err != nil {
		return fmt.Errorf("touch session: %w", err)
	}
	return nil
}

// DeleteSession removes one session. A missing session is not an error.
func (s *Store) DeleteSession(ctx context.Context, idHash string) error {
	if err := s.q.DeleteSession(ctx, idHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions removes every session of a user.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	if err := s.q.DeleteUserSessions(ctx, userID); err != nil {
		return fmt.Errorf("delete user sessions: %w", err)
	}
	return nil
}

// PruneSessions removes sessions past their absolute lifetime.
func (s *Store) PruneSessions(ctx context.Context, now time.Time) error {
	if err := s.q.PruneSessions(ctx, formatTime(now)); err != nil {
		return fmt.Errorf("prune sessions: %w", err)
	}
	return nil
}

// AddAudit appends an entry. The API has no way to change or delete one.
func (s *Store) AddAudit(ctx context.Context, e model.AuditEntry) error {
	at := e.At
	if at.IsZero() {
		at = s.now()
	}
	var uid sql.NullInt64
	if e.UserID != nil {
		uid = sql.NullInt64{Int64: *e.UserID, Valid: true}
	}
	err := s.q.InsertAudit(ctx, dbgen.InsertAuditParams{
		At: formatTime(at), UserID: uid, Username: e.Username, Action: e.Action,
		Entity: e.Entity, EntityID: e.EntityID, Result: e.Result, Ip: e.IP, Detail: e.Detail,
	})
	if err != nil {
		return fmt.Errorf("add audit: %w", err)
	}
	return nil
}

// ListAudit returns entries, newest first.
func (s *Store) ListAudit(ctx context.Context, f AuditFilter) ([]model.AuditEntry, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultAuditRows
	}
	limit = min(limit, maxAuditRows)
	rows, err := s.q.ListAudit(ctx, dbgen.ListAuditParams{
		BeforeID: f.BeforeID, Action: f.Action, Username: f.Username, MaxRows: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list audit: %w", err)
	}
	out := make([]model.AuditEntry, 0, len(rows))
	for _, r := range rows {
		at, err := parseTime(r.At)
		if err != nil {
			return nil, fmt.Errorf("audit %d: at: %w", r.ID, err)
		}
		e := model.AuditEntry{
			ID: r.ID, At: at, Username: r.Username, Action: r.Action, Entity: r.Entity,
			EntityID: r.EntityID, Result: r.Result, IP: r.Ip, Detail: r.Detail,
		}
		if r.UserID.Valid {
			id := r.UserID.Int64
			e.UserID = &id
		}
		out = append(out, e)
	}
	return out, nil
}

// PruneAudit deletes entries older than keepDays days.
func (s *Store) PruneAudit(ctx context.Context, keepDays int) error {
	cutoff := s.now().AddDate(0, 0, -keepDays)
	if err := s.q.PruneAudit(ctx, formatTime(cutoff)); err != nil {
		return fmt.Errorf("prune audit: %w", err)
	}
	return nil
}
