package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

// fakeRepo is an in-memory Repo. Its lockout rule mirrors the store's, so the
// tests here check how the service drives it, not the store.
type fakeRepo struct {
	mu       sync.Mutex
	now      func() time.Time
	users    map[string]*model.UserRecord
	sessions map[string]SessionRecord
	nextID   int64

	countCalls  int
	touchCalls  int
	recorded    []recordedLogin
	passwordSet []passwordSet
}

type recordedLogin struct {
	ok        bool
	lockAfter int
	lockFor   time.Duration
}

type passwordSet struct {
	id         int64
	hash       string
	mustChange bool
}

func newFakeRepo(now func() time.Time) *fakeRepo {
	return &fakeRepo{now: now, users: map[string]*model.UserRecord{}, sessions: map[string]SessionRecord{}}
}

func (r *fakeRepo) CountUsers(context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.countCalls++
	return len(r.users), nil
}

func (r *fakeRepo) CreateUser(_ context.Context, u model.UserRecord) (model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.users[strings.ToLower(u.Username)]; dup {
		return model.User{}, model.ErrConflict
	}
	r.nextID++
	u.ID = r.nextID
	r.users[strings.ToLower(u.Username)] = &u
	return u.User, nil
}

func (r *fakeRepo) GetUserByName(_ context.Context, name string) (model.UserRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[strings.ToLower(name)]
	if !ok {
		return model.UserRecord{}, model.ErrNotFound
	}
	return *u, nil
}

func (r *fakeRepo) ListUsers(context.Context) ([]model.User, error) { return nil, nil }

func (r *fakeRepo) UpdateUser(_ context.Context, id int64, role *model.Role, disabled *bool) (model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.ID == id {
			if role != nil {
				u.Role = *role
			}
			if disabled != nil {
				u.Disabled = *disabled
			}
			return u.User, nil
		}
	}
	return model.User{}, model.ErrNotFound
}

func (r *fakeRepo) SetPassword(_ context.Context, id int64, hash string, must bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.users {
		if u.ID == id {
			u.PasswordHash, u.MustChangePassword = hash, must
			u.FailedLogins, u.LockedUntil = 0, nil
			for h, s := range r.sessions {
				if s.UserID == id {
					delete(r.sessions, h)
				}
			}
			r.passwordSet = append(r.passwordSet, passwordSet{id, hash, must})
			return nil
		}
	}
	return model.ErrNotFound
}

func (r *fakeRepo) RecordLogin(_ context.Context, id int64, ok bool, lockAfter int, lockFor time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, recordedLogin{ok, lockAfter, lockFor})
	for _, u := range r.users {
		if u.ID != id {
			continue
		}
		if ok {
			u.FailedLogins, u.LockedUntil = 0, nil
			return nil
		}
		u.FailedLogins++
		if u.FailedLogins >= lockAfter {
			until := r.now().Add(lockFor)
			u.FailedLogins, u.LockedUntil = 0, &until
		}
		return nil
	}
	return model.ErrNotFound
}

func (r *fakeRepo) CreateSession(_ context.Context, s SessionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[s.IDHash] = s
	return nil
}

func (r *fakeRepo) GetSession(_ context.Context, h string) (SessionRecord, model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[h]
	if !ok {
		return SessionRecord{}, model.User{}, model.ErrNotFound
	}
	for _, u := range r.users {
		if u.ID == s.UserID {
			return s, u.User, nil
		}
	}
	return SessionRecord{}, model.User{}, model.ErrNotFound
}

func (r *fakeRepo) TouchSession(_ context.Context, h string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.touchCalls++
	if s, ok := r.sessions[h]; ok {
		s.LastSeenAt = now
		r.sessions[h] = s
	}
	return nil
}

func (r *fakeRepo) DeleteSession(_ context.Context, h string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, h)
	return nil
}

func testConfig() config.AuthConfig {
	return config.AuthConfig{
		SessionIdleTimeout: config.Duration(8 * time.Hour),
		SessionMaxAge:      config.Duration(168 * time.Hour),
		MinPasswordLength:  12,
		MaxFailedLogins:    5,
		LockoutDuration:    config.Duration(15 * time.Minute),
		Argon2:             config.Argon2Config{MemoryKiB: 19456, Iterations: 2, Parallelism: 1},
	}
}

const goodPassword = "correct horse battery"

func newTestService(t *testing.T) (*Service, *fakeRepo, *fakeClock) {
	t.Helper()
	clk := newClock()
	repo := newFakeRepo(clk.now)
	svc, err := NewService(repo, testConfig(), clk.now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, repo, clk
}

func addUser(t *testing.T, svc *Service, name string, role model.Role) model.User {
	t.Helper()
	u, err := svc.CreateUser(context.Background(), NewUser{Username: name, Role: role, Password: goodPassword})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLoginOpensASessionStoredOnlyAsAHash(t *testing.T) {
	svc, repo, clk := newTestService(t)
	addUser(t, svc, "alice", model.RoleOperator)

	sess, user, err := svc.Login(context.Background(), "alice", goodPassword, "10.0.0.1", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if user.Username != "alice" || user.Role != model.RoleOperator {
		t.Errorf("user = %+v", user)
	}
	if len(sess.ID) < 43 || strings.ContainsAny(sess.ID, "+/=") {
		t.Errorf("session id %q is not 32 random bytes in base64url", sess.ID)
	}
	if sess.CSRFToken == "" || sess.CSRFToken == sess.ID {
		t.Errorf("csrf token %q", sess.CSRFToken)
	}
	if want := clk.now().Add(7 * 24 * time.Hour); !sess.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt = %v, want 7 days from now (%v)", sess.ExpiresAt, want)
	}

	if len(repo.sessions) != 1 {
		t.Fatalf("%d sessions stored", len(repo.sessions))
	}
	for hash, rec := range repo.sessions {
		if hash == sess.ID || rec.IDHash == sess.ID || strings.Contains(hash, sess.ID) {
			t.Error("the raw session id was stored")
		}
		if rec.IDHash != HashID(sess.ID) || len(rec.IDHash) != 64 {
			t.Errorf("stored id %q is not the hex SHA-256 of the cookie value", rec.IDHash)
		}
		if rec.IP != "10.0.0.1" || rec.UserAgent != "test-agent" {
			t.Errorf("record = %+v", rec)
		}
	}
}

func TestEveryLoginGetsANewIdentifier(t *testing.T) {
	svc, _, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	a, _, _ := svc.Login(context.Background(), "alice", goodPassword, "", "")
	b, _, _ := svc.Login(context.Background(), "alice", goodPassword, "", "")
	if a.ID == b.ID || a.CSRFToken == b.CSRFToken {
		t.Error("two logins share an identifier or a CSRF token")
	}
}

func TestUsernameIsCaseInsensitiveAtLogin(t *testing.T) {
	svc, _, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	if _, _, err := svc.Login(context.Background(), "ALICE", goodPassword, "", ""); err != nil {
		t.Errorf("Login(ALICE) = %v", err)
	}
}

func TestFailedLoginsAreIndistinguishable(t *testing.T) {
	svc, repo, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	addUser(t, svc, "dora", model.RoleViewer)
	if _, err := repo.UpdateUser(context.Background(), repo.users["dora"].ID, nil, ptr(true)); err != nil {
		t.Fatal(err)
	}
	locked := addUser(t, svc, "lena", model.RoleViewer)
	until := newClock().now().Add(time.Hour)
	repo.users["lena"].LockedUntil = &until
	_ = locked

	cases := map[string][2]string{
		"wrong password":       {"alice", "not the password"},
		"unknown user":         {"nobody", goodPassword},
		"disabled, right pass": {"dora", goodPassword},
		"locked, right pass":   {"lena", goodPassword},
		"invalid username":     {"a b", goodPassword},
		"empty username":       {"", goodPassword},
	}
	for name, c := range cases {
		_, _, err := svc.Login(context.Background(), c[0], c[1], "", "")
		if err != ErrInvalidCredentials {
			t.Errorf("%s: got %v, want exactly ErrInvalidCredentials", name, err)
		}
	}
}

func TestOnlyAWrongPasswordCountsAsAFailure(t *testing.T) {
	svc, repo, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	_, _, _ = svc.Login(context.Background(), "nobody", "whatever password", "", "")
	if len(repo.recorded) != 0 {
		t.Fatalf("an unknown user was recorded: %+v", repo.recorded)
	}
	_, _, _ = svc.Login(context.Background(), "alice", "wrong wrong wrong", "", "")
	if len(repo.recorded) != 1 || repo.recorded[0].ok || repo.recorded[0].lockAfter != 5 || repo.recorded[0].lockFor != 15*time.Minute {
		t.Errorf("recorded = %+v, want one failure with lock after 5 for 15m", repo.recorded)
	}
}

func TestFifthFailureLocksTheAccountForFifteenMinutes(t *testing.T) {
	svc, _, clk := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		if _, _, err := svc.Login(ctx, "alice", "wrong wrong wrong", "", ""); err != ErrInvalidCredentials {
			t.Fatalf("failure %d: %v", i, err)
		}
	}
	if _, _, err := svc.Login(ctx, "alice", goodPassword, "", ""); err != ErrInvalidCredentials {
		t.Fatalf("a correct password during the lock = %v, want 401-equivalent", err)
	}
	clk.advance(14*time.Minute + 59*time.Second)
	if _, _, err := svc.Login(ctx, "alice", goodPassword, "", ""); err != ErrInvalidCredentials {
		t.Fatal("the lock ended early")
	}
	clk.advance(2 * time.Second)
	if _, _, err := svc.Login(ctx, "alice", goodPassword, "", ""); err != nil {
		t.Errorf("login after 15 minutes = %v", err)
	}
}

func TestSuccessfulLoginResetsTheFailureCounter(t *testing.T) {
	svc, _, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()
	for range 4 {
		_, _, _ = svc.Login(ctx, "alice", "wrong wrong wrong", "", "")
	}
	if _, _, err := svc.Login(ctx, "alice", goodPassword, "", ""); err != nil {
		t.Fatal(err)
	}
	for range 4 { // four more would lock the account if the counter had not been reset
		_, _, _ = svc.Login(ctx, "alice", "wrong wrong wrong", "", "")
	}
	if _, _, err := svc.Login(ctx, "alice", goodPassword, "", ""); err != nil {
		t.Errorf("the counter was not reset by the successful login: %v", err)
	}
}

func TestLoginUpgradesAWeakHash(t *testing.T) {
	clk := newClock()
	repo := newFakeRepo(clk.now)
	cfg := testConfig()
	weak, _ := Hash(goodPassword, Params{MemoryKiB: 19456, Iterations: 2, Parallelism: 1})
	repo.users["alice"] = &model.UserRecord{
		User:         model.User{ID: 1, Username: "alice", Role: model.RoleViewer, MustChangePassword: true},
		PasswordHash: weak,
	}
	cfg.Argon2.Iterations = 3
	svc, err := NewService(repo, cfg, clk.now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(context.Background(), "alice", goodPassword, "", ""); err != nil {
		t.Fatal(err)
	}
	if len(repo.passwordSet) != 1 {
		t.Fatalf("%d password updates, want 1", len(repo.passwordSet))
	}
	got := repo.passwordSet[0]
	if !got.mustChange {
		t.Error("the rehash cleared must_change_password")
	}
	if NeedsRehash(got.hash, Params{MemoryKiB: 19456, Iterations: 3, Parallelism: 1}) {
		t.Error("the stored hash is still weaker than the configuration")
	}
	if ok, _ := Verify(goodPassword, got.hash); !ok {
		t.Error("the upgraded hash does not verify the password")
	}
	if len(repo.sessions) != 1 {
		t.Errorf("%d sessions: the rehash must not cost the new session", len(repo.sessions))
	}
}

func TestLoginKeepsAStrongHashAsItIs(t *testing.T) {
	svc, repo, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	svc.Login(context.Background(), "alice", goodPassword, "", "")
	if len(repo.passwordSet) != 0 {
		t.Error("a current hash was rewritten at login")
	}
}

func TestAuthenticate(t *testing.T) {
	svc, _, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleOperator)
	ctx := context.Background()
	sess, _, _ := svc.Login(ctx, "alice", goodPassword, "", "")

	p, err := svc.Authenticate(ctx, sess.ID)
	if err != nil || p.User.Username != "alice" || p.CSRFToken != sess.CSRFToken {
		t.Errorf("Authenticate = %+v, %v", p, err)
	}
	for name, raw := range map[string]string{
		"empty":     "",
		"unknown":   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"too long":  strings.Repeat("a", 500),
		"the hash":  HashID(sess.ID), // knowing the stored value must not be enough
		"truncated": sess.ID[:20],
	} {
		if _, err := svc.Authenticate(ctx, raw); err != ErrUnauthenticated {
			t.Errorf("%s: %v, want ErrUnauthenticated", name, err)
		}
	}
}

func TestSessionExpiresAfterEightHoursIdle(t *testing.T) {
	svc, repo, clk := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()
	sess, _, _ := svc.Login(ctx, "alice", goodPassword, "", "")

	clk.advance(8*time.Hour - time.Second)
	if _, err := svc.Authenticate(ctx, sess.ID); err != nil {
		t.Fatalf("expired a second early: %v", err)
	}
	// The call above counted as activity, so the idle clock restarted.
	clk.advance(8*time.Hour + time.Second)
	if _, err := svc.Authenticate(ctx, sess.ID); err != ErrUnauthenticated {
		t.Errorf("idle for over 8h: %v, want ErrUnauthenticated", err)
	}
	if len(repo.sessions) != 0 {
		t.Error("the expired session row was left behind")
	}
}

func TestActiveSessionStillDiesAfterSevenDays(t *testing.T) {
	svc, _, clk := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()
	start := clk.now()
	sess, _, _ := svc.Login(ctx, "alice", goodPassword, "", "")

	for range 27 { // active every 6 hours for 162 hours, well inside the idle limit
		clk.advance(6 * time.Hour)
		if _, err := svc.Authenticate(ctx, sess.ID); err != nil {
			t.Fatalf("at %v: %v", clk.now().Sub(start), err)
		}
	}
	clk.advance(6 * time.Hour) // exactly 7 days after the login
	if _, err := svc.Authenticate(ctx, sess.ID); err != ErrUnauthenticated {
		t.Errorf("an always-active session outlived its 7 days: %v", err)
	}
}

func TestActivityIsRecordedAtMostOncePerMinute(t *testing.T) {
	svc, repo, clk := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()
	sess, _, _ := svc.Login(ctx, "alice", goodPassword, "", "")

	for range 20 {
		clk.advance(2 * time.Second)
		_, _ = svc.Authenticate(ctx, sess.ID)
	}
	if repo.touchCalls != 0 {
		t.Errorf("%d writes within the first minute, want 0", repo.touchCalls)
	}
	clk.advance(time.Minute)
	_, _ = svc.Authenticate(ctx, sess.ID)
	if repo.touchCalls != 1 {
		t.Errorf("%d writes after a minute, want 1", repo.touchCalls)
	}
}

func TestDisabledUsersSessionIsRefused(t *testing.T) {
	svc, repo, _ := newTestService(t)
	u := addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()
	sess, _, _ := svc.Login(ctx, "alice", goodPassword, "", "")
	_, _ = repo.UpdateUser(ctx, u.ID, nil, ptr(true))
	if _, err := svc.Authenticate(ctx, sess.ID); err != ErrUnauthenticated {
		t.Errorf("a disabled user's session = %v", err)
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	svc, _, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()
	sess, _, _ := svc.Login(ctx, "alice", goodPassword, "", "")
	if err := svc.Logout(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, sess.ID); err != ErrUnauthenticated {
		t.Errorf("after logout: %v", err)
	}
	if err := svc.Logout(ctx, ""); err != nil {
		t.Errorf("Logout with no cookie = %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	svc, repo, _ := newTestService(t)
	u := addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()
	sess, _, _ := svc.Login(ctx, "alice", goodPassword, "", "")
	p, _ := svc.Authenticate(ctx, sess.ID)
	_ = u

	if err := svc.ChangePassword(ctx, p, "wrong wrong wrong", "a brand new password"); err != ErrWrongPassword {
		t.Errorf("wrong current password = %v", err)
	}
	if last := repo.recorded[len(repo.recorded)-1]; last.ok {
		t.Error("a wrong current password was not counted as a failed attempt")
	}
	if err := svc.ChangePassword(ctx, p, goodPassword, "short"); !errors.Is(err, ErrPasswordShort) {
		t.Errorf("short new password = %v", err)
	}
	if err := svc.ChangePassword(ctx, p, goodPassword, "a brand new password"); err != nil {
		t.Fatalf("valid change = %v", err)
	}
	if _, err := svc.Authenticate(ctx, sess.ID); err != ErrUnauthenticated {
		t.Error("the session survived a password change")
	}
	if _, _, err := svc.Login(ctx, "alice", goodPassword, "", ""); err != ErrInvalidCredentials {
		t.Error("the old password still works")
	}
	if _, _, err := svc.Login(ctx, "alice", "a brand new password", "", ""); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
}

func TestChangePasswordClearsMustChange(t *testing.T) {
	svc, repo, _ := newTestService(t)
	if _, err := svc.CreateUser(context.Background(), NewUser{Username: "alice", Role: model.RoleViewer, Password: goodPassword, MustChangePassword: true}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sess, user, _ := svc.Login(ctx, "alice", goodPassword, "", "")
	if !user.MustChangePassword {
		t.Fatal("the flag was lost")
	}
	p, _ := svc.Authenticate(ctx, sess.ID)
	if err := svc.ChangePassword(ctx, p, goodPassword, "a brand new password"); err != nil {
		t.Fatal(err)
	}
	if repo.users["alice"].MustChangePassword {
		t.Error("must_change_password is still set")
	}
}

func TestChangePasswordRefusedWhileLocked(t *testing.T) {
	svc, repo, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()
	sess, _, _ := svc.Login(ctx, "alice", goodPassword, "", "")
	p, _ := svc.Authenticate(ctx, sess.ID)
	until := newClock().now().Add(time.Hour)
	repo.users["alice"].LockedUntil = &until
	if err := svc.ChangePassword(ctx, p, goodPassword, "a brand new password"); err != ErrWrongPassword {
		t.Errorf("change during a lock = %v", err)
	}
}

func TestCreateUserValidatesAndNormalizes(t *testing.T) {
	svc, repo, _ := newTestService(t)
	ctx := context.Background()
	tests := []struct {
		name string
		in   NewUser
		want error
	}{
		{"valid", NewUser{Username: "Alice", Role: model.RoleAdmin, Password: goodPassword}, nil},
		{"duplicate in another case", NewUser{Username: "ALICE", Role: model.RoleViewer, Password: goodPassword}, model.ErrConflict},
		{"bad username", NewUser{Username: "a", Role: model.RoleViewer, Password: goodPassword}, ErrUsernameInvalid},
		{"bad role", NewUser{Username: "bob", Role: "root", Password: goodPassword}, ErrRoleUnknown},
		{"empty role", NewUser{Username: "bob", Password: goodPassword}, ErrRoleUnknown},
		{"short password", NewUser{Username: "bob", Role: model.RoleViewer, Password: "short"}, ErrPasswordShort},
		{"password equals username", NewUser{Username: "administrator", Role: model.RoleViewer, Password: "administrator"}, ErrPasswordIsUsername},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateUser(ctx, tt.in)
			if !errors.Is(err, tt.want) && (tt.want != nil || err != nil) {
				t.Errorf("CreateUser() = %v, want %v", err, tt.want)
			}
		})
	}
	u := repo.users["alice"]
	if u == nil || u.Username != "alice" {
		t.Fatalf("the name was not normalized: %+v", u)
	}
	if !strings.HasPrefix(u.PasswordHash, "$argon2id$") || strings.Contains(u.PasswordHash, goodPassword) {
		t.Errorf("stored hash = %q", u.PasswordHash)
	}
}

func TestHasUsersCachesAPositiveAnswer(t *testing.T) {
	svc, repo, _ := newTestService(t)
	ctx := context.Background()
	if has, err := svc.HasUsers(ctx); err != nil || has {
		t.Fatalf("empty database: %v, %v", has, err)
	}
	addUser(t, svc, "alice", model.RoleAdmin)
	before := repo.countCalls
	for range 5 {
		if has, _ := svc.HasUsers(ctx); !has {
			t.Fatal("HasUsers = false after a user was created")
		}
	}
	if repo.countCalls != before {
		t.Errorf("%d extra count queries after the first positive answer", repo.countCalls-before)
	}
}

func TestResetPasswordAndSetDisabled(t *testing.T) {
	svc, repo, _ := newTestService(t)
	addUser(t, svc, "alice", model.RoleViewer)
	ctx := context.Background()
	sess, _, _ := svc.Login(ctx, "alice", goodPassword, "", "")

	if err := svc.ResetPassword(ctx, "ALICE", "another long password", true); err != nil {
		t.Fatal(err)
	}
	if !repo.users["alice"].MustChangePassword {
		t.Error("must_change_password was not set")
	}
	if _, err := svc.Authenticate(ctx, sess.ID); err != ErrUnauthenticated {
		t.Error("a password reset left the old session alive")
	}
	if err := svc.ResetPassword(ctx, "alice", "short", false); !errors.Is(err, ErrPasswordShort) {
		t.Errorf("short reset = %v", err)
	}
	if err := svc.ResetPassword(ctx, "ghost", "another long password", false); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("unknown user = %v", err)
	}
	if u, err := svc.SetDisabled(ctx, "alice", true); err != nil || !u.Disabled {
		t.Errorf("SetDisabled = %+v, %v", u, err)
	}
}

func TestCSRFMatches(t *testing.T) {
	for _, tt := range []struct {
		want, got string
		ok        bool
	}{
		{"abc", "abc", true}, {"abc", "abd", false}, {"abc", "", false}, {"", "", false}, {"abc", "abcd", false},
	} {
		if CSRFMatches(tt.want, tt.got) != tt.ok {
			t.Errorf("CSRFMatches(%q, %q) = %v", tt.want, tt.got, !tt.ok)
		}
	}
}

func ptr[T any](v T) *T { return &v }
