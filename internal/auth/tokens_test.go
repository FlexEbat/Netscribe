package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
)

var bg = context.Background()

// otherChar returns a base64url character different from c.
func otherChar(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}

func issue(t *testing.T, svc *Service, u model.User, scope string, days int) (string, model.APIToken) {
	t.Helper()
	raw, tok, err := svc.IssueToken(bg, u, "ci", scope, days)
	if err != nil {
		t.Fatalf("IssueToken(%s, %d): %v", scope, days, err)
	}
	return raw, tok
}

func TestIssuedTokenIsShownOnceAndOnlyItsHashIsKept(t *testing.T) {
	svc, repo, clk := newTestService(t)
	u := addUser(t, svc, "olga", model.RoleOperator)

	raw, tok := issue(t, svc, u, ScopeWrite, 0)
	if !strings.HasPrefix(raw, TokenPrefix) || len(raw) != len(TokenPrefix)+43 {
		t.Errorf("token %q, want ndt_ and 43 characters of base64url", raw)
	}
	if tok.Prefix != raw[:8] || tok.Scope != ScopeWrite {
		t.Errorf("token info = %+v", tok)
	}
	if want := clk.now().AddDate(0, 0, DefaultTokenDays); !tok.ExpiresAt.Equal(want) {
		t.Errorf("expires %v, want the default of %d days: %v", tok.ExpiresAt, DefaultTokenDays, want)
	}
	for hash, rec := range repo.tokens {
		if hash != HashID(raw) {
			t.Errorf("stored key %q is not the SHA-256 of the token", hash)
		}
		for _, field := range []string{rec.Name, rec.Prefix, rec.Hash, rec.Scope} {
			if field == raw {
				t.Errorf("the token itself is stored: %+v", rec)
			}
		}
	}
	if raw2, _ := issue(t, svc, u, ScopeRead, 30); raw2 == raw {
		t.Error("two tokens have the same value")
	}
}

func TestTokenLifetimeNameAndScopeAreChecked(t *testing.T) {
	svc, _, _ := newTestService(t)
	op := addUser(t, svc, "olga", model.RoleOperator)
	viewer := addUser(t, svc, "vera", model.RoleViewer)

	for _, tc := range []struct {
		name  string
		owner model.User
		tname string
		scope string
		days  int
		want  error
	}{
		{"a year", op, "ci", ScopeRead, 365, nil},
		{"a year and a day", op, "ci", ScopeRead, 366, ErrTokenLifetime},
		{"negative", op, "ci", ScopeRead, -1, ErrTokenLifetime},
		{"no name", op, "", ScopeRead, 0, ErrTokenNameInvalid},
		{"long name", op, strings.Repeat("n", 65), ScopeRead, 0, ErrTokenNameInvalid},
		{"unknown scope", op, "ci", "admin", 0, ErrTokenScope},
		{"write for a viewer", viewer, "ci", ScopeWrite, 0, ErrScopeAboveRole},
		{"read for a viewer", viewer, "ci", ScopeRead, 0, nil},
		{"write for an operator", op, "ci", ScopeWrite, 0, nil},
		{"empty scope means read", op, "ci", "", 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.IssueToken(bg, tc.owner, tc.tname, tc.scope, tc.days)
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestOnlyTwentyActiveTokensPerUser(t *testing.T) {
	svc, _, _ := newTestService(t)
	u := addUser(t, svc, "olga", model.RoleOperator)
	var first model.APIToken
	for i := 0; i < 20; i++ {
		_, tok := issue(t, svc, u, ScopeRead, 0)
		if i == 0 {
			first = tok
		}
	}
	if _, _, err := svc.IssueToken(bg, u, "one too many", ScopeRead, 0); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("21st token: error = %v, want ErrConflict", err)
	}
	if err := svc.RevokeToken(bg, first.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.IssueToken(bg, u, "fits again", ScopeRead, 0); err != nil {
		t.Errorf("after a revocation: %v", err)
	}
}

func TestAuthenticateTokenAcceptsAValidTokenAndRefusesTheRest(t *testing.T) {
	svc, repo, _ := newTestService(t)
	u := addUser(t, svc, "olga", model.RoleOperator)
	raw, tok := issue(t, svc, u, ScopeWrite, 10)

	p, err := svc.AuthenticateToken(bg, raw)
	if err != nil || p.User.ID != u.ID || p.Scope != ScopeWrite || p.CSRFToken != "" {
		t.Fatalf("AuthenticateToken = %+v, %v", p, err)
	}

	for name, value := range map[string]string{
		"empty":           "",
		"only the prefix": TokenPrefix,
		"no prefix":       strings.TrimPrefix(raw, TokenPrefix),
		"one char off":    raw[:len(raw)-1] + otherChar(raw[len(raw)-1]),
		"a session id":    "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopq",
		"too long":        TokenPrefix + strings.Repeat("a", 500),
	} {
		if _, err := svc.AuthenticateToken(bg, value); !errors.Is(err, ErrUnauthenticated) {
			t.Errorf("%s: error = %v, want ErrUnauthenticated", name, err)
		}
	}

	if err := svc.RevokeToken(bg, tok.ID, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(bg, raw); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("revoked token: error = %v", err)
	}
	if len(repo.tokens) != 1 {
		t.Errorf("revoking removed the row: %d tokens", len(repo.tokens))
	}
}

func TestExpiredTokenAndDisabledOwnerAreRefused(t *testing.T) {
	svc, _, clk := newTestService(t)
	u := addUser(t, svc, "olga", model.RoleOperator)
	raw, _ := issue(t, svc, u, ScopeRead, 1)

	clk.advance(24*time.Hour - time.Second)
	if _, err := svc.AuthenticateToken(bg, raw); err != nil {
		t.Fatalf("a second before the end: %v", err)
	}
	clk.advance(time.Second)
	if _, err := svc.AuthenticateToken(bg, raw); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("at the end: error = %v, want ErrUnauthenticated", err)
	}

	raw2, _ := issue(t, svc, u, ScopeRead, 30)
	if _, err := svc.SetDisabled(bg, "olga", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateToken(bg, raw2); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("disabled owner: error = %v, want ErrUnauthenticated", err)
	}
}

func TestLastUsedIsWrittenAtMostOncePerMinute(t *testing.T) {
	svc, repo, clk := newTestService(t)
	u := addUser(t, svc, "olga", model.RoleOperator)
	raw, _ := issue(t, svc, u, ScopeRead, 0)
	for i := 0; i < 5; i++ {
		if _, err := svc.AuthenticateToken(bg, raw); err != nil {
			t.Fatal(err)
		}
	}
	if repo.tokenTouches != 1 {
		t.Errorf("touches = %d, want 1 for five quick requests", repo.tokenTouches)
	}
	clk.advance(2 * time.Minute)
	if _, err := svc.AuthenticateToken(bg, raw); err != nil {
		t.Fatal(err)
	}
	if repo.tokenTouches != 2 {
		t.Errorf("touches = %d, want 2 after two minutes", repo.tokenTouches)
	}
}

func TestATokenNeverHoldsUserAuditOrTokenRights(t *testing.T) {
	forbidden := []model.Permission{model.PermUsersManage, model.PermAuditRead, model.PermTokensManage}
	for _, role := range []model.Role{model.RoleViewer, model.RoleOperator, model.RoleAdmin} {
		for _, scope := range []string{ScopeRead, ScopeWrite} {
			p := Principal{User: model.User{Role: role}, Scope: scope}
			for _, perm := range forbidden {
				if p.Can(perm) {
					t.Errorf("%s token of a %s holds %s", scope, role, perm)
				}
			}
			for _, perm := range p.Permissions() {
				if !Can(role, perm) {
					t.Errorf("%s token of a %s holds %s, which the role does not", scope, role, perm)
				}
			}
		}
	}
}

func TestTokenRightsFollowScopeAndTheCurrentRole(t *testing.T) {
	read := Principal{User: model.User{Role: model.RoleAdmin}, Scope: ScopeRead}
	if !read.Can(model.PermTopologyRead) || read.Can(model.PermDevicesWrite) || read.Can(model.PermScansRun) {
		t.Errorf("read token of an admin holds %v", read.Permissions())
	}
	write := Principal{User: model.User{Role: model.RoleOperator}, Scope: ScopeWrite}
	if !write.Can(model.PermDevicesWrite) || !write.Can(model.PermScansRun) {
		t.Errorf("write token of an operator holds %v", write.Permissions())
	}
	// The owner was demoted after the token was issued: the token follows the role down.
	demoted := Principal{User: model.User{Role: model.RoleViewer}, Scope: ScopeWrite}
	if demoted.Can(model.PermDevicesWrite) || !demoted.Can(model.PermTopologyRead) {
		t.Errorf("write token of a demoted viewer holds %v", demoted.Permissions())
	}
	session := Principal{User: model.User{Role: model.RoleAdmin}}
	if !session.Can(model.PermUsersManage) || !session.Can(model.PermAuditRead) {
		t.Error("a session of an admin lost rights")
	}
}

func TestResetPasswordByIDValidatesAndRequiresAChange(t *testing.T) {
	svc, repo, _ := newTestService(t)
	u := addUser(t, svc, "olga", model.RoleOperator)

	if err := svc.ResetPasswordByID(bg, u.ID, "short"); !errors.Is(err, ErrPasswordShort) {
		t.Errorf("short password: error = %v", err)
	}
	if err := svc.ResetPasswordByID(bg, 999, goodPassword); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("unknown user: error = %v", err)
	}
	if err := svc.ResetPasswordByID(bg, u.ID, "another long password"); err != nil {
		t.Fatal(err)
	}
	last := repo.passwordSet[len(repo.passwordSet)-1]
	if !last.mustChange || last.id != u.ID {
		t.Errorf("password set = %+v, want a temporary password for the user", last)
	}
}
