package store

import (
	"context"
	"fmt"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth"
)

// ListUserSessions returns the sessions of one user that have not expired, newest first.
func (s *Store) ListUserSessions(ctx context.Context, userID int64, now time.Time) ([]auth.SessionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id_hash, user_id, created_at, last_seen_at, expires_at, ip, user_agent
		FROM sessions WHERE user_id = ? AND expires_at > ? ORDER BY last_seen_at DESC, created_at DESC`, userID, formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }() // the loop below reports a read error through rows.Err
	var out []auth.SessionRecord
	for rows.Next() {
		var rec auth.SessionRecord
		var created, seen, expires string
		if err := rows.Scan(&rec.IDHash, &rec.UserID, &created, &seen, &expires, &rec.IP, &rec.UserAgent); err != nil {
			return nil, fmt.Errorf("read session: %w", err)
		}
		var perr error
		if rec.CreatedAt, perr = parseTime(created); perr != nil {
			return nil, fmt.Errorf("read session time: %w", perr)
		}
		if rec.LastSeenAt, perr = parseTime(seen); perr != nil {
			return nil, fmt.Errorf("read session time: %w", perr)
		}
		if rec.ExpiresAt, perr = parseTime(expires); perr != nil {
			return nil, fmt.Errorf("read session time: %w", perr)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	return out, nil
}

// DeleteSessionOf removes one session, but only when it belongs to the user. It reports
// whether a session was removed.
func (s *Store) DeleteSessionOf(ctx context.Context, userID int64, idHash string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id_hash = ?`, userID, idHash)
	if err != nil {
		return false, fmt.Errorf("delete session: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete session: %w", err)
	}
	return n > 0, nil
}

// DeleteUserSessionsExcept removes every session of the user but the one named, and
// reports how many it removed.
func (s *Store) DeleteUserSessionsExcept(ctx context.Context, userID int64, keep string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id_hash <> ?`, userID, keep)
	if err != nil {
		return 0, fmt.Errorf("delete sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete sessions: %w", err)
	}
	return n, nil
}
