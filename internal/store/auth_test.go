package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

func mkUser(t *testing.T, s *Store, name string, role model.Role) model.User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), model.UserRecord{
		User:         model.User{Username: name, Role: role},
		PasswordHash: "$argon2id$stub",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mkSession(t *testing.T, s *Store, userID int64, id string, created time.Time) {
	t.Helper()
	err := s.CreateSession(context.Background(), SessionRecord{
		IDHash: id, UserID: userID, CSRFToken: "csrf-" + id,
		CreatedAt: created, LastSeenAt: created, ExpiresAt: created.Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func sessionCount(t *testing.T, s *Store, userID int64) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM sessions WHERE user_id = ?", userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateAndFindUsers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if n, err := s.CountUsers(ctx); err != nil || n != 0 {
		t.Fatalf("CountUsers = %d, %v", n, err)
	}
	u := mkUser(t, s, "alice", model.RoleAdmin)
	if u.ID == 0 || u.Username != "alice" || u.Role != model.RoleAdmin || u.CreatedAt.IsZero() || u.Disabled || u.LastLoginAt != nil {
		t.Errorf("created user = %+v", u)
	}
	if n, _ := s.CountUsers(ctx); n != 1 {
		t.Errorf("CountUsers = %d", n)
	}

	rec, err := s.GetUserByName(ctx, "ALICE")
	if err != nil || rec.ID != u.ID || rec.PasswordHash != "$argon2id$stub" {
		t.Errorf("GetUserByName(ALICE) = %+v, %v", rec, err)
	}
	if _, err := s.GetUserByName(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown name = %v", err)
	}
	if got, err := s.GetUser(ctx, u.ID); err != nil || got.Username != "alice" {
		t.Errorf("GetUser = %+v, %v", got, err)
	}
	if _, err := s.GetUser(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id = %v", err)
	}
}

func TestUsernameUniquenessIgnoresCase(t *testing.T) {
	s := newTestStore(t)
	mkUser(t, s, "alice", model.RoleViewer)
	_, err := s.CreateUser(context.Background(), model.UserRecord{User: model.User{Username: "ALICE", Role: model.RoleViewer}, PasswordHash: "h"})
	if !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate name in another case = %v, want ErrConflict", err)
	}
}

func TestListUsersIsSortedByName(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []string{"carol", "alice", "bob"} {
		mkUser(t, s, n, model.RoleViewer)
	}
	got, err := s.ListUsers(context.Background())
	if err != nil || len(got) != 3 || got[0].Username != "alice" || got[2].Username != "carol" {
		t.Errorf("ListUsers = %+v, %v", got, err)
	}
}

func TestSetPasswordRevokesSessionsAndClearsTheLock(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mkUser(t, s, "alice", model.RoleViewer)
	other := mkUser(t, s, "bob", model.RoleViewer)
	now := s.now()
	mkSession(t, s, u.ID, "a1", now)
	mkSession(t, s, u.ID, "a2", now)
	mkSession(t, s, other.ID, "b1", now)
	for range 5 {
		if err := s.RecordLogin(ctx, u.ID, false, 5, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.SetPassword(ctx, u.ID, "$argon2id$new", true); err != nil {
		t.Fatal(err)
	}
	if sessionCount(t, s, u.ID) != 0 {
		t.Error("sessions of the user survived a password change")
	}
	if sessionCount(t, s, other.ID) != 1 {
		t.Error("another user's session was revoked")
	}
	rec, _ := s.GetUserByName(ctx, "alice")
	if rec.PasswordHash != "$argon2id$new" || !rec.MustChangePassword || rec.LockedUntil != nil || rec.FailedLogins != 0 {
		t.Errorf("record = %+v", rec)
	}
	if err := s.SetPassword(ctx, 999, "h", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetPassword(unknown) = %v", err)
	}
}

func TestRecordLoginLocksOnTheFifthFailure(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mkUser(t, s, "alice", model.RoleViewer)

	for i := 1; i <= 4; i++ {
		if err := s.RecordLogin(ctx, u.ID, false, 5, 15*time.Minute); err != nil {
			t.Fatal(err)
		}
		rec, _ := s.GetUserByName(ctx, "alice")
		if rec.LockedUntil != nil || rec.FailedLogins != i {
			t.Fatalf("after %d failures: %+v", i, rec)
		}
	}
	before := s.now()
	if err := s.RecordLogin(ctx, u.ID, false, 5, 15*time.Minute); err != nil {
		t.Fatal(err)
	}
	rec, _ := s.GetUserByName(ctx, "alice")
	if rec.LockedUntil == nil {
		t.Fatal("the fifth failure did not lock the account")
	}
	if d := rec.LockedUntil.Sub(before); d < 15*time.Minute || d > 17*time.Minute {
		t.Errorf("locked for %v, want about 15m", d)
	}
	if rec.FailedLogins != 0 {
		t.Errorf("FailedLogins = %d: the counter must restart after a lock", rec.FailedLogins)
	}
}

func TestRecordLoginSuccessClearsFailuresAndStampsTheTime(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mkUser(t, s, "alice", model.RoleViewer)
	for range 3 {
		_ = s.RecordLogin(ctx, u.ID, false, 5, time.Minute)
	}
	if err := s.RecordLogin(ctx, u.ID, true, 5, time.Minute); err != nil {
		t.Fatal(err)
	}
	rec, _ := s.GetUserByName(ctx, "alice")
	if rec.FailedLogins != 0 || rec.LockedUntil != nil || rec.LastLoginAt == nil {
		t.Errorf("record = %+v", rec)
	}
	if err := s.RecordLogin(ctx, 999, false, 5, time.Minute); !errors.Is(err, ErrNotFound) {
		t.Errorf("RecordLogin(unknown) = %v", err)
	}
}

func TestLastAdministratorIsProtected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	admin := mkUser(t, s, "root", model.RoleAdmin)
	viewer := mkUser(t, s, "vera", model.RoleViewer)

	if _, err := s.UpdateUser(ctx, admin.ID, ptr(model.RoleOperator), nil); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demoting the only admin = %v, want ErrLastAdmin", err)
	}
	if _, err := s.UpdateUser(ctx, admin.ID, nil, ptr(true)); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disabling the only admin = %v, want ErrLastAdmin", err)
	}
	if err := s.DeleteUser(ctx, admin.ID); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("deleting the only admin = %v, want ErrLastAdmin", err)
	}
	if got, _ := s.GetUser(ctx, admin.ID); got.Role != model.RoleAdmin || got.Disabled {
		t.Errorf("a refused change was applied: %+v", got)
	}

	// A second admin makes the first one removable, but never the last one standing.
	second := mkUser(t, s, "second", model.RoleAdmin)
	if _, err := s.UpdateUser(ctx, second.ID, ptr(model.RoleViewer), nil); err != nil {
		t.Fatalf("demoting one of two admins: %v", err)
	}
	if _, err := s.UpdateUser(ctx, admin.ID, nil, ptr(true)); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disabling the admin that is now the only one = %v", err)
	}
	// Non-admins are never "last".
	if err := s.DeleteUser(ctx, viewer.ID); err != nil {
		t.Errorf("deleting a viewer = %v", err)
	}
}

func TestDisabledAdminsDoNotCountAsAdmins(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := mkUser(t, s, "alpha", model.RoleAdmin)
	b := mkUser(t, s, "bravo", model.RoleAdmin)
	if _, err := s.UpdateUser(ctx, a.ID, nil, ptr(true)); err != nil {
		t.Fatal(err)
	}
	// alpha is disabled, so bravo is the only enabled administrator.
	if _, err := s.UpdateUser(ctx, b.ID, nil, ptr(true)); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disabling the only enabled admin = %v", err)
	}
	if err := s.DeleteUser(ctx, a.ID); err != nil {
		t.Errorf("deleting a disabled admin = %v", err)
	}
}

func TestRoleChangeAndDisableRevokeSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mkUser(t, s, "root", model.RoleAdmin)
	u := mkUser(t, s, "alice", model.RoleViewer)

	mkSession(t, s, u.ID, "s1", s.now())
	if got, err := s.UpdateUser(ctx, u.ID, ptr(model.RoleOperator), nil); err != nil || got.Role != model.RoleOperator {
		t.Fatalf("role change = %+v, %v", got, err)
	}
	if sessionCount(t, s, u.ID) != 0 {
		t.Error("a role change did not revoke sessions")
	}

	mkSession(t, s, u.ID, "s2", s.now())
	if got, err := s.UpdateUser(ctx, u.ID, nil, ptr(true)); err != nil || !got.Disabled {
		t.Fatalf("disable = %+v, %v", got, err)
	}
	if sessionCount(t, s, u.ID) != 0 {
		t.Error("disabling did not revoke sessions")
	}

	// A no-op update must not log the user out.
	mkSession(t, s, u.ID, "s3", s.now())
	if _, err := s.UpdateUser(ctx, u.ID, ptr(model.RoleOperator), ptr(true)); err != nil {
		t.Fatal(err)
	}
	if sessionCount(t, s, u.ID) != 1 {
		t.Error("an update that changed nothing revoked sessions")
	}
}

func TestUpdateUserErrors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.UpdateUser(ctx, 999, ptr(model.RoleViewer), nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id = %v", err)
	}
	u := mkUser(t, s, "alice", model.RoleViewer)
	if _, err := s.UpdateUser(ctx, u.ID, ptr(model.Role("root")), nil); err == nil {
		t.Error("an unknown role was accepted")
	}
	if got, _ := s.GetUser(ctx, u.ID); got.Role != model.RoleViewer {
		t.Errorf("role = %q after a refused update", got.Role)
	}
	if err := s.DeleteUser(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteUser(unknown) = %v", err)
	}
}

func TestDeletingAUserDeletesTheirSessions(t *testing.T) {
	s := newTestStore(t)
	mkUser(t, s, "root", model.RoleAdmin)
	u := mkUser(t, s, "alice", model.RoleViewer)
	mkSession(t, s, u.ID, "s1", s.now())
	if err := s.DeleteUser(context.Background(), u.ID); err != nil {
		t.Fatal(err)
	}
	if sessionCount(t, s, u.ID) != 0 {
		t.Error("sessions outlived their user")
	}
}

func TestSessionRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mkUser(t, s, "alice", model.RoleOperator)
	created := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	rec := SessionRecord{
		IDHash: "hash1", UserID: u.ID, CSRFToken: "csrf", CreatedAt: created,
		LastSeenAt: created.Add(time.Minute), ExpiresAt: created.Add(7 * 24 * time.Hour), IP: "10.0.0.1", UserAgent: "ua",
	}
	if err := s.CreateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, user, err := s.GetSession(ctx, "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if got != rec {
		t.Errorf("session = %+v, want %+v", got, rec)
	}
	if user.ID != u.ID || user.Role != model.RoleOperator {
		t.Errorf("owner = %+v", user)
	}
	if _, _, err := s.GetSession(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session = %v", err)
	}

	later := created.Add(time.Hour)
	if err := s.TouchSession(ctx, "hash1", later); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetSession(ctx, "hash1")
	if !got.LastSeenAt.Equal(later) {
		t.Errorf("LastSeenAt = %v", got.LastSeenAt)
	}

	if err := s.DeleteSession(ctx, "hash1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetSession(ctx, "hash1"); !errors.Is(err, ErrNotFound) {
		t.Error("the session was not deleted")
	}
	if err := s.DeleteSession(ctx, "hash1"); err != nil {
		t.Errorf("deleting a missing session = %v, want nil", err)
	}
}

func TestOnlyTenSessionsPerUserOldestEvicted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mkUser(t, s, "alice", model.RoleViewer)
	other := mkUser(t, s, "bob", model.RoleViewer)
	base := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	mkSession(t, s, other.ID, "bob-1", base)

	for i := 1; i <= 12; i++ {
		mkSession(t, s, u.ID, "alice-"+strconv.Itoa(i), base.Add(time.Duration(i)*time.Minute))
	}
	if n := sessionCount(t, s, u.ID); n != 10 {
		t.Fatalf("%d sessions, want 10", n)
	}
	for i := 1; i <= 2; i++ {
		if _, _, err := s.GetSession(ctx, "alice-"+strconv.Itoa(i)); !errors.Is(err, ErrNotFound) {
			t.Errorf("alice-%d (one of the two oldest) survived", i)
		}
	}
	for i := 3; i <= 12; i++ {
		if _, _, err := s.GetSession(ctx, "alice-"+strconv.Itoa(i)); err != nil {
			t.Errorf("alice-%d was evicted: %v", i, err)
		}
	}
	if sessionCount(t, s, other.ID) != 1 {
		t.Error("another user's session was evicted")
	}
}

func TestPruneSessionsRemovesOnlyExpiredOnes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mkUser(t, s, "alice", model.RoleViewer)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	mkSession(t, s, u.ID, "old", now.Add(-8*24*time.Hour))   // expired a day ago
	mkSession(t, s, u.ID, "fresh", now.Add(-1*24*time.Hour)) // 6 days left
	if err := s.PruneSessions(ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetSession(ctx, "old"); !errors.Is(err, ErrNotFound) {
		t.Error("an expired session was kept")
	}
	if _, _, err := s.GetSession(ctx, "fresh"); err != nil {
		t.Errorf("a live session was pruned: %v", err)
	}
}

func TestSessionsAreDeletedBySQLCascadeNotOnlyByCode(t *testing.T) {
	s := newTestStore(t)
	u := mkUser(t, s, "alice", model.RoleViewer)
	mkSession(t, s, u.ID, "s1", s.now())
	if _, err := s.db.Exec("DELETE FROM users WHERE id = ?", u.ID); err != nil {
		t.Fatal(err)
	}
	if sessionCount(t, s, u.ID) != 0 {
		t.Error("foreign keys are not enforced: the session outlived the user")
	}
}

func TestAudit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	uid := int64(7)
	for i, e := range []model.AuditEntry{
		{Action: "login", Username: "alice", UserID: &uid, Result: "ok", IP: "10.0.0.1", Detail: "first"},
		{Action: "login_failed", Username: "bob", Result: "denied", IP: "10.0.0.2"},
		{Action: "logout", Username: "alice", UserID: &uid, Result: "ok", Entity: "session", EntityID: "x"},
		{Action: "login", Username: "bob", Result: "ok"},
	} {
		if err := s.AddAudit(ctx, e); err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
	}

	all, err := s.ListAudit(ctx, AuditFilter{})
	if err != nil || len(all) != 4 {
		t.Fatalf("ListAudit = %d entries, %v", len(all), err)
	}
	if all[0].Username != "bob" || all[0].Action != "login" || all[3].Detail != "first" {
		t.Errorf("not newest first: %+v ... %+v", all[0], all[3])
	}
	if all[3].UserID == nil || *all[3].UserID != 7 || all[2].UserID != nil {
		t.Errorf("user ids = %v and %v", all[3].UserID, all[2].UserID)
	}
	if all[0].At.IsZero() || all[0].At.Location() != time.UTC {
		t.Errorf("At = %v", all[0].At)
	}

	if got, _ := s.ListAudit(ctx, AuditFilter{Action: "login"}); len(got) != 2 {
		t.Errorf("filter by action: %d entries", len(got))
	}
	if got, _ := s.ListAudit(ctx, AuditFilter{Username: "alice"}); len(got) != 2 {
		t.Errorf("filter by username: %d entries", len(got))
	}
	if got, _ := s.ListAudit(ctx, AuditFilter{Action: "login", Username: "bob"}); len(got) != 1 {
		t.Errorf("combined filter: %d entries", len(got))
	}
	if got, _ := s.ListAudit(ctx, AuditFilter{Limit: 2}); len(got) != 2 {
		t.Errorf("limit: %d entries", len(got))
	}
	page, _ := s.ListAudit(ctx, AuditFilter{Limit: 2, BeforeID: all[1].ID})
	if len(page) != 2 || page[0].ID != all[2].ID {
		t.Errorf("paging: %+v", page)
	}
}

func TestAuditLimitIsCapped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := range 520 {
		if err := s.AddAudit(ctx, model.AuditEntry{Action: "login", Result: "ok", Detail: strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := s.ListAudit(ctx, AuditFilter{Limit: 100000}); len(got) != 500 {
		t.Errorf("%d entries, the cap is 500", len(got))
	}
	if got, _ := s.ListAudit(ctx, AuditFilter{}); len(got) != 50 {
		t.Errorf("%d entries by default, want 50", len(got))
	}
}

func TestPruneAuditKeepsRecentEntries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	for _, age := range []int{400, 366, 364, 1} {
		if err := s.AddAudit(ctx, model.AuditEntry{At: now.AddDate(0, 0, -age), Action: "login", Result: "ok", Detail: strconv.Itoa(age)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PruneAudit(ctx, 365); err != nil {
		t.Fatal(err)
	}
	got, _ := s.ListAudit(ctx, AuditFilter{})
	if len(got) != 2 || got[0].Detail != "1" || got[1].Detail != "364" {
		t.Errorf("after pruning: %+v", got)
	}
}

func ptr[T any](v T) *T { return &v }
