package store

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

var tokenBase = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func mkToken(t *testing.T, s *Store, userID int64, name string, created time.Time, days int) model.APIToken {
	t.Helper()
	tok, err := s.CreateToken(context.Background(), TokenRecord{
		UserID: userID, Name: name, Prefix: "ndt_" + name[:1], Hash: "hash-" + name, Scope: "read",
		CreatedAt: created, ExpiresAt: created.AddDate(0, 0, days),
	})
	if err != nil {
		t.Fatalf("CreateToken(%s): %v", name, err)
	}
	return tok
}

func TestTokenRoundTripKeepsOnlyTheHash(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mkUser(t, s, "olga", model.RoleOperator)
	tok := mkToken(t, s, u.ID, "ci", tokenBase, 90)
	if tok.ID == 0 || tok.Prefix != "ndt_c" || tok.Scope != "read" || tok.Revoked || tok.LastUsedAt != nil {
		t.Errorf("token = %+v", tok)
	}

	rec, owner, err := s.GetTokenByHash(ctx, "hash-ci")
	if err != nil || owner.ID != u.ID || rec.Hash != "hash-ci" || !rec.ExpiresAt.Equal(tokenBase.AddDate(0, 0, 90)) {
		t.Fatalf("GetTokenByHash = %+v, %+v, %v", rec, owner, err)
	}
	if _, _, err := s.GetTokenByHash(ctx, "nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown hash: error = %v, want ErrNotFound", err)
	}

	when := tokenBase.Add(5 * time.Minute)
	if err := s.TouchToken(ctx, tok.ID, when); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListTokens(ctx, u.ID)
	if err != nil || len(list) != 1 || list[0].LastUsedAt == nil || !list[0].LastUsedAt.Equal(when) {
		t.Errorf("list = %+v, %v", list, err)
	}
}

func TestListTokensIsPerUserNewestFirstAndNeverNil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := mkUser(t, s, "anna", model.RoleOperator)
	b := mkUser(t, s, "boris", model.RoleOperator)
	empty, err := s.ListTokens(ctx, a.ID)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("empty list = %#v, %v", empty, err)
	}
	first := mkToken(t, s, a.ID, "first", tokenBase, 90)
	second := mkToken(t, s, a.ID, "second", tokenBase, 90)
	mkToken(t, s, b.ID, "other", tokenBase, 90)

	list, _ := s.ListTokens(ctx, a.ID)
	if len(list) != 2 || list[0].ID != second.ID || list[1].ID != first.ID {
		t.Errorf("list = %+v, want the user's two, newest first", list)
	}
}

func TestOnlyTheOwnerRevokesAToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := mkUser(t, s, "anna", model.RoleOperator)
	b := mkUser(t, s, "boris", model.RoleOperator)
	tok := mkToken(t, s, a.ID, "ci", tokenBase, 90)

	if err := s.RevokeToken(ctx, tok.ID, b.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking someone else's token: error = %v, want ErrNotFound", err)
	}
	if rec, _, _ := s.GetTokenByHash(ctx, "hash-ci"); rec.Revoked {
		t.Fatal("the token was revoked by another user")
	}
	if err := s.RevokeToken(ctx, tok.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if rec, _, _ := s.GetTokenByHash(ctx, "hash-ci"); !rec.Revoked {
		t.Error("the owner's revocation did nothing")
	}
	if err := s.RevokeToken(ctx, 999, a.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown token: error = %v, want ErrNotFound", err)
	}
}

func TestTwentyActiveTokensIsTheLimit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	u := mkUser(t, s, "olga", model.RoleOperator)
	other := mkUser(t, s, "boris", model.RoleOperator)

	// Expired and revoked tokens do not count.
	mkToken(t, s, u.ID, "ancient", tokenBase.AddDate(0, 0, -200), 90)
	gone := mkToken(t, s, u.ID, "gone", tokenBase, 90)
	if err := s.RevokeToken(ctx, gone.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxActiveTokens; i++ {
		mkToken(t, s, u.ID, "t"+strconv.Itoa(i), tokenBase, 90)
	}
	_, err := s.CreateToken(ctx, TokenRecord{UserID: u.ID, Name: "x", Prefix: "ndt_x", Hash: "hash-x", Scope: "read", CreatedAt: tokenBase, ExpiresAt: tokenBase.AddDate(0, 0, 90)})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("21st active token: error = %v, want ErrConflict", err)
	}
	if _, _, err := s.GetTokenByHash(ctx, "hash-x"); !errors.Is(err, ErrNotFound) {
		t.Error("the refused token was stored")
	}
	mkToken(t, s, other.ID, "fine", tokenBase, 90) // another user is not affected
}

func TestDeletingAUserDeletesTheirTokens(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mkUser(t, s, "root", model.RoleAdmin)
	u := mkUser(t, s, "olga", model.RoleOperator)
	mkToken(t, s, u.ID, "ci", tokenBase, 90)
	if err := s.DeleteUser(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.GetTokenByHash(ctx, "hash-ci"); !errors.Is(err, ErrNotFound) {
		t.Errorf("token of a deleted user: error = %v, want ErrNotFound", err)
	}
}

func TestAuditFiltersByResultAndPeriod(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := tokenBase
	s.SetClock(func() time.Time { return now })
	add := func(action, result string, at time.Time) {
		now = at
		if err := s.AddAudit(ctx, model.AuditEntry{Action: action, Result: result, Username: "alice"}); err != nil {
			t.Fatal(err)
		}
	}
	add("login", "ok", tokenBase)
	add("login_failed", "denied", tokenBase.Add(24*time.Hour))
	add("device_edit", "error", tokenBase.Add(48*time.Hour))
	add("logout", "ok", tokenBase.Add(72*time.Hour))

	byResult, _ := s.ListAudit(ctx, AuditFilter{Result: "ok"})
	if len(byResult) != 2 {
		t.Errorf("result ok: %d entries, want 2", len(byResult))
	}
	// From is included, To is not.
	period, _ := s.ListAudit(ctx, AuditFilter{From: tokenBase.Add(24 * time.Hour), To: tokenBase.Add(72 * time.Hour)})
	if len(period) != 2 || period[0].Action != "device_edit" || period[1].Action != "login_failed" {
		t.Errorf("period: %+v, want login_failed and device_edit", period)
	}
	onlyFrom, _ := s.ListAudit(ctx, AuditFilter{From: tokenBase.Add(48 * time.Hour)})
	if len(onlyFrom) != 2 {
		t.Errorf("only from: %d entries, want 2", len(onlyFrom))
	}
	combined, _ := s.ListAudit(ctx, AuditFilter{Result: "ok", To: tokenBase.Add(48 * time.Hour)})
	if len(combined) != 1 || combined[0].Action != "login" {
		t.Errorf("combined: %+v", combined)
	}
}
