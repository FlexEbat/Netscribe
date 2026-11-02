package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// TOTPRecord is the second factor of one account.
type TOTPRecord struct {
	SecretSealed []byte
	Confirmed    bool
	LastStep     int64
}

// RecoveryCode is a stored recovery code: its hash, and nothing that could be typed.
type RecoveryCode struct {
	ID   int64
	Hash string
}

// GetTOTP returns the second factor of a user, or model.ErrNotFound.
func (s *Store) GetTOTP(ctx context.Context, userID int64) (TOTPRecord, error) {
	var rec TOTPRecord
	var confirmed sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT secret_sealed, confirmed_at, last_step FROM user_totp WHERE user_id = ?`, userID).
		Scan(&rec.SecretSealed, &confirmed, &rec.LastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return TOTPRecord{}, model.ErrNotFound
	}
	if err != nil {
		return TOTPRecord{}, fmt.Errorf("read second factor: %w", err)
	}
	rec.Confirmed = confirmed.Valid
	return rec, nil
}

// Required tells the login whether the user must give a second factor.
func (s *Store) Required(ctx context.Context, userID int64) (bool, error) {
	rec, err := s.GetTOTP(ctx, userID)
	if errors.Is(err, model.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return rec.Confirmed, nil
}

// SaveTOTPPending stores a secret that is not confirmed yet, replacing an earlier setup
// that was never finished. It does not touch a confirmed second factor.
func (s *Store) SaveTOTPPending(ctx context.Context, userID int64, sealed []byte, now time.Time) error {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO user_totp (user_id, secret_sealed, created_at, confirmed_at, last_step)
		VALUES (?, ?, ?, NULL, 0)
		ON CONFLICT(user_id) DO UPDATE SET secret_sealed = excluded.secret_sealed, created_at = excluded.created_at, last_step = 0
		WHERE user_totp.confirmed_at IS NULL`, userID, sealed, formatTime(now)); err != nil {
		return fmt.Errorf("save second factor: %w", err)
	}
	return nil
}

// ConfirmTOTP marks the pending secret as in use and records the step of the code that
// proved it. It reports false when there was nothing pending.
func (s *Store) ConfirmTOTP(ctx context.Context, userID, step int64, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE user_totp SET confirmed_at = ?, last_step = ? WHERE user_id = ? AND confirmed_at IS NULL`,
		formatTime(now), step, userID)
	if err != nil {
		return false, fmt.Errorf("confirm second factor: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// DeleteTOTP removes the second factor and the recovery codes of a user.
func (s *Store) DeleteTOTP(ctx context.Context, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("remove second factor: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a no-op after Commit
	for _, q := range []string{`DELETE FROM recovery_codes WHERE user_id = ?`, `DELETE FROM user_totp WHERE user_id = ?`} {
		if _, err := tx.ExecContext(ctx, q, userID); err != nil {
			return fmt.Errorf("remove second factor: %w", err)
		}
	}
	return tx.Commit()
}

// AdvanceTOTPStep records the step of an accepted code, and reports false when that step
// or a later one was already used: the same code must not work twice.
func (s *Store) AdvanceTOTPStep(ctx context.Context, userID, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE user_totp SET last_step = ? WHERE user_id = ? AND last_step < ? AND confirmed_at IS NOT NULL`, step, userID, step)
	if err != nil {
		return false, fmt.Errorf("record code: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ReplaceRecoveryCodes stores the hashes of a new set and forgets the old one.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID int64, hashes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store recovery codes: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // a no-op after Commit
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store recovery codes: %w", err)
	}
	for _, h := range hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (user_id, hash) VALUES (?, ?)`, userID, h); err != nil {
			return fmt.Errorf("store recovery codes: %w", err)
		}
	}
	return tx.Commit()
}

// UnusedRecoveryCodes lists the codes of a user that were not used yet.
func (s *Store) UnusedRecoveryCodes(ctx context.Context, userID int64) ([]RecoveryCode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, hash FROM recovery_codes WHERE user_id = ? AND used_at IS NULL ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("list recovery codes: %w", err)
	}
	defer func() { _ = rows.Close() }() // the loop reports a read error through rows.Err
	var out []RecoveryCode
	for rows.Next() {
		var c RecoveryCode
		if err := rows.Scan(&c.ID, &c.Hash); err != nil {
			return nil, fmt.Errorf("read recovery code: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UseRecoveryCode marks a code as used, and reports false when it already was: two
// requests with the same code cannot both succeed.
func (s *Store) UseRecoveryCode(ctx context.Context, id int64, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE recovery_codes SET used_at = ? WHERE id = ? AND used_at IS NULL`, formatTime(now), id)
	if err != nil {
		return false, fmt.Errorf("use recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
