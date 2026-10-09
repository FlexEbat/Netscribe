package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth"
	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
)

func TestLoginSuccess(t *testing.T) {
	e := newEnv(t)
	e.addUser("alice", model.RoleOperator)
	sess, rec := e.login("alice", goodPassword)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	c := sess.cookie
	if c == nil {
		t.Fatal("no session cookie")
	}
	if c.Name != "netscribe_session" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
		t.Errorf("cookie = %+v, want netscribe_session, HttpOnly, SameSite=Strict, Path=/", c)
	}
	if c.Secure {
		t.Error("the cookie is Secure over plain HTTP: the browser would never send it back")
	}
	if len(c.Value) < 43 {
		t.Errorf("session id %q is too short for 32 random bytes", c.Value)
	}
	if want := e.clk.now().Add(7 * 24 * time.Hour); c.Expires.Sub(want).Abs() > 2*time.Second {
		t.Errorf("cookie expires %v, want about %v", c.Expires, want)
	}

	var me meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.User.Username != "alice" || me.User.Role != model.RoleOperator || me.CSRFToken == "" {
		t.Errorf("body = %+v", me)
	}
	if len(me.Permissions) != 5 || me.Permissions[len(me.Permissions)-1] != model.PermDevicesWrite {
		t.Errorf("permissions = %v, want the operator's five", me.Permissions)
	}
	body := rec.Body.String()
	for _, secret := range []string{"argon2", "passwordHash", "password_hash", goodPassword, c.Value} {
		if strings.Contains(body, secret) {
			t.Errorf("the login response contains %q", secret)
		}
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
}

func TestSessionIdentifierIsStoredOnlyAsAHash(t *testing.T) {
	e := newEnv(t)
	sess, _ := e.login("admin", goodPassword)
	_, _, err := e.store.GetSession(context.Background(), sess.cookie.Value)
	if err == nil {
		t.Error("the cookie value is a key in the sessions table: the identifier is stored in plain")
	}
	if _, _, err := e.store.GetSession(context.Background(), auth.HashID(sess.cookie.Value)); err != nil {
		t.Errorf("the SHA-256 of the cookie is not stored: %v", err)
	}
}

func TestStoredPasswordsAreArgon2idPHC(t *testing.T) {
	e := newEnv(t)
	rec, err := e.store.GetUserByName(context.Background(), "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rec.PasswordHash, "$argon2id$v=19$m=") || strings.Contains(rec.PasswordHash, goodPassword) {
		t.Errorf("stored hash = %q", rec.PasswordHash)
	}
}

func TestCookieIsSecureOverHTTPS(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.trusted = []string{"10.0.0.0/8"} })

	_, viaProxy := e.login("admin", goodPassword, from("10.1.1.1:4000"), withHeader("X-Forwarded-Proto", "https"))
	if c := viaProxy.Result().Cookies(); len(c) == 0 || !c[0].Secure {
		t.Error("the cookie is not Secure behind a trusted proxy that reports https")
	}
	_, spoof := e.login("admin", goodPassword, from("203.0.113.5:4000"), withHeader("X-Forwarded-Proto", "https"))
	if c := spoof.Result().Cookies(); len(c) == 0 || c[0].Secure {
		t.Error("an untrusted client could mark the cookie Secure")
	}
}

func TestFailedLoginsGiveOneIdenticalAnswer(t *testing.T) {
	e := newEnv(t)
	e.addUser("alice", model.RoleViewer)
	dora := e.addUser("dora", model.RoleViewer)
	disabled := true
	if _, err := e.store.UpdateUser(context.Background(), dora.ID, nil, &disabled); err != nil {
		t.Fatal(err)
	}
	e.addUser("lena", model.RoleViewer)
	for range 5 {
		e.login("lena", "wrong wrong wrong", from("198.51.100.1:1")) // locks lena
	}

	var first *struct {
		code int
		body string
	}
	for name, c := range map[string][2]string{
		"wrong password":       {"alice", "wrong wrong wrong"},
		"unknown user":         {"nobody", goodPassword},
		"disabled, right pass": {"dora", goodPassword},
		"locked, right pass":   {"lena", goodPassword},
	} {
		_, rec := e.login(c[0], c[1], from("198.51.100."+strconv.Itoa(10+len(name))+":1"))
		got := struct {
			code int
			body string
		}{rec.Code, rec.Body.String()}
		if got.code != http.StatusUnauthorized || strings.TrimSpace(got.body) != `{"error":"invalid username or password"}` {
			t.Errorf("%s: %d %q", name, got.code, got.body)
		}
		if first == nil {
			first = &got
		} else if got != *first {
			t.Errorf("%s answers differently from the others: %v vs %v", name, got, *first)
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Errorf("%s: a cookie was set", name)
		}
	}
}

func TestFifthFailureLocksTheAccountAndSuccessResets(t *testing.T) {
	e := newEnv(t)
	e.addUser("alice", model.RoleViewer)

	for i := 1; i <= 4; i++ {
		e.login("alice", "wrong wrong wrong", from("198.51.100."+strconv.Itoa(i)+":1"))
	}
	if _, rec := e.login("alice", goodPassword, from("198.51.100.50:1")); rec.Code != http.StatusOK {
		t.Fatalf("a correct password after four failures = %d", rec.Code)
	}
	// The success cleared the counter: four more failures must not lock the account.
	for i := 1; i <= 4; i++ {
		e.login("alice", "wrong wrong wrong", from("198.51.100."+strconv.Itoa(60+i)+":1"))
	}
	if _, rec := e.login("alice", goodPassword, from("198.51.100.70:1")); rec.Code != http.StatusOK {
		t.Fatalf("the counter was not reset by the success: %d", rec.Code)
	}

	for i := 1; i <= 5; i++ {
		e.login("alice", "wrong wrong wrong", from("198.51.100."+strconv.Itoa(100+i)+":1"))
	}
	if _, rec := e.login("alice", goodPassword, from("198.51.100.120:1")); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a correct password right after the fifth failure = %d, want 401", rec.Code)
	}
	e.clk.advance(14*time.Minute + 59*time.Second)
	if _, rec := e.login("alice", goodPassword, from("198.51.100.121:1")); rec.Code != http.StatusUnauthorized {
		t.Fatal("the lock ended early")
	}
	e.clk.advance(2 * time.Second)
	if _, rec := e.login("alice", goodPassword, from("198.51.100.122:1")); rec.Code != http.StatusOK {
		t.Errorf("login after 15 minutes = %d", rec.Code)
	}
}

func TestMoreThanTenAttemptsPerMinutePerIPGive429(t *testing.T) {
	e := newEnv(t)
	ip := from("198.51.100.7:4000")
	for i := 1; i <= 10; i++ {
		if _, rec := e.login("nobody", "whatever password", ip); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i, rec.Code)
		}
	}
	_, rec := e.login("admin", goodPassword, ip) // even a correct password is refused
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the 11th attempt = %d, want 429", rec.Code)
	}
	ra, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || ra < 1 || ra > 60 {
		t.Errorf("Retry-After = %q, want a number of seconds up to 60", rec.Header().Get("Retry-After"))
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("a refused attempt set a cookie")
	}

	if _, rec := e.login("admin", goodPassword, from("198.51.100.8:4000")); rec.Code != http.StatusOK {
		t.Errorf("another address was affected: %d", rec.Code)
	}
	e.clk.advance(61 * time.Second)
	if _, rec := e.login("admin", goodPassword, ip); rec.Code != http.StatusOK {
		t.Errorf("still limited after a minute: %d", rec.Code)
	}
}

func TestRateLimitCannotBeEvadedWithForwardedHeaders(t *testing.T) {
	e := newEnv(t) // no trusted proxies: X-Forwarded-For must be ignored
	for i := 1; i <= 10; i++ {
		e.login("nobody", "whatever password", from("198.51.100.7:4000"), withHeader("X-Forwarded-For", "10.9.9."+strconv.Itoa(i)))
	}
	_, rec := e.login("nobody", "whatever password", from("198.51.100.7:4000"), withHeader("X-Forwarded-For", "10.9.9.99"))
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("a spoofed X-Forwarded-For reset the limit: %d", rec.Code)
	}
}

func TestRateLimitUsesTheClientBehindATrustedProxy(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.trusted = []string{"10.0.0.0/8"} })
	proxy := from("10.0.0.1:4000")
	for i := 1; i <= 10; i++ {
		// The client sends a made-up first hop, the proxy appends the real address.
		e.login("nobody", "whatever password", proxy, withHeader("X-Forwarded-For", "1.1.1."+strconv.Itoa(i)+", 198.51.100.7"))
	}
	if _, rec := e.login("nobody", "whatever password", proxy, withHeader("X-Forwarded-For", "9.9.9.9, 198.51.100.7")); rec.Code != http.StatusTooManyRequests {
		t.Errorf("the real client was not limited: %d", rec.Code)
	}
	if _, rec := e.login("admin", goodPassword, proxy, withHeader("X-Forwarded-For", "198.51.100.8")); rec.Code != http.StatusOK {
		t.Errorf("a different client behind the same proxy was limited: %d", rec.Code)
	}
}

func TestLoginRejectsMalformedRequests(t *testing.T) {
	e := newEnv(t)
	long := func(n int) string { return strings.Repeat("a", n) }
	for name, body := range map[string]string{
		"not json":          "not json",
		"empty body":        "",
		"empty object":      "{}",
		"array":             `["admin", "x"]`,
		"unknown field":     `{"username":"admin","password":"` + goodPassword + `","admin":true}`,
		"missing password":  `{"username":"admin"}`,
		"missing username":  `{"password":"` + goodPassword + `"}`,
		"wrong types":       `{"username":1,"password":2}`,
		"trailing data":     `{"username":"admin","password":"` + goodPassword + `"} {}`,
		"huge username":     `{"username":"` + long(65) + `","password":"x"}`,
		"huge password":     `{"username":"admin","password":"` + long(1025) + `"}`,
		"body over the cap": `{"username":"admin","password":"` + long(maxBodyBytes) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := e.do(http.MethodPost, "/api/auth/login", body, from("198.51.100."+strconv.Itoa(len(name))+":1"))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 (body %q)", rec.Code, rec.Body)
			}
		})
	}
}

func TestProtectedRoutesRequireAValidSession(t *testing.T) {
	e := newEnv(t)
	sess := e.mustLogin("admin")

	if rec := e.do(http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "authentication required") {
		t.Errorf("no cookie: %d %q", rec.Code, rec.Body)
	}
	bad := e.do(http.MethodGet, "/api/auth/me", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"})
	})
	if bad.Code != http.StatusUnauthorized {
		t.Errorf("unknown id: %d", bad.Code)
	}
	if c := bad.Result().Cookies(); len(c) == 0 || c[0].MaxAge >= 0 {
		t.Error("the stale cookie was not cleared")
	}
	if rec := e.do(http.MethodGet, "/api/auth/me", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: auth.HashID(sess.cookie.Value)}) // the stored value is not a credential
	}); rec.Code != http.StatusUnauthorized {
		t.Errorf("the stored hash was accepted as a session: %d", rec.Code)
	}
	if rec := e.as(sess, http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusOK {
		t.Errorf("valid session: %d", rec.Code)
	}
}

func TestSessionExpiresAfterEightHoursIdle(t *testing.T) {
	e := newEnv(t)
	sess := e.mustLogin("admin")
	e.clk.advance(8*time.Hour - time.Minute)
	if e.as(sess, http.MethodGet, "/api/auth/me", nil).Code != http.StatusOK {
		t.Fatal("the session expired early")
	}
	e.clk.advance(8*time.Hour + time.Minute)
	if rec := e.as(sess, http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("after more than 8 idle hours: %d, want 401", rec.Code)
	}
}

func TestSessionExpiresAfterSevenDaysAbsolutely(t *testing.T) {
	e := newEnv(t)
	sess := e.mustLogin("admin")
	for range 27 { // active every 6 hours
		e.clk.advance(6 * time.Hour)
		if e.as(sess, http.MethodGet, "/api/auth/me", nil).Code != http.StatusOK {
			t.Fatal("expired before 7 days")
		}
	}
	e.clk.advance(6 * time.Hour)
	if rec := e.as(sess, http.MethodGet, "/api/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("an always-active session lived past 7 days: %d", rec.Code)
	}
}

func TestUnsafeMethodsNeedTheCSRFToken(t *testing.T) {
	e := newEnv(t, withRoutes(permissionRoutes()...))
	e.addUser("olga", model.RoleOperator)
	sess := e.mustLogin("olga")

	send := func(token string, set bool) int {
		return e.do(http.MethodPost, "/api/test/scans", nil, func(r *http.Request) {
			r.AddCookie(sess.cookie)
			if set {
				r.Header.Set(csrfHeader, token)
			}
		}).Code
	}
	if code := send("", false); code != http.StatusForbidden {
		t.Errorf("no token = %d, want 403", code)
	}
	if code := send("", true); code != http.StatusForbidden {
		t.Errorf("empty token = %d, want 403", code)
	}
	if code := send("not-the-token", true); code != http.StatusForbidden {
		t.Errorf("wrong token = %d, want 403", code)
	}
	if code := send(sess.csrf+"x", true); code != http.StatusForbidden {
		t.Errorf("token with a suffix = %d, want 403", code)
	}
	if code := send(sess.csrf, true); code != http.StatusOK {
		t.Errorf("right token = %d, want 200", code)
	}
	// Reads need no token.
	if code := e.do(http.MethodGet, "/api/test/topology", nil, func(r *http.Request) { r.AddCookie(sess.cookie) }).Code; code != http.StatusOK {
		t.Errorf("GET without a token = %d", code)
	}
	// The token of another session does not work.
	other := e.mustLogin("olga")
	if code := send(other.csrf, true); code != http.StatusForbidden {
		t.Errorf("a token of another session = %d, want 403", code)
	}
}

func TestEveryUnsafeMethodIsCovered(t *testing.T) {
	e := newEnv(t, withRoutes(permissionRoutes()...))
	e.addUser("olga", model.RoleOperator)
	sess := e.mustLogin("olga")
	for _, tt := range []struct{ method, path string }{
		{"POST", "/api/test/scans"}, {"PUT", "/api/test/devices"}, {"POST", "/api/auth/logout"}, {"PUT", "/api/auth/password"},
	} {
		rec := e.do(tt.method, tt.path, nil, func(r *http.Request) { r.AddCookie(sess.cookie) })
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without a token = %d, want 403", tt.method, tt.path, rec.Code)
		}
	}
	if _, _, err := e.store.GetSession(context.Background(), auth.HashID(sess.cookie.Value)); err != nil {
		t.Error("a request without the CSRF token ended the session")
	}
}

func TestMe(t *testing.T) {
	e := newEnv(t)
	e.addUser("vera", model.RoleViewer)
	sess := e.mustLogin("vera")
	rec := e.as(sess, http.MethodGet, "/api/auth/me", nil)
	var me meResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.User.Username != "vera" || me.CSRFToken != sess.csrf || len(me.Permissions) != 3 {
		t.Errorf("me = %+v", me)
	}
	for _, p := range me.Permissions {
		if p == model.PermScansRun || p == model.PermUsersManage {
			t.Errorf("a viewer was told it holds %s", p)
		}
	}
	if strings.Contains(rec.Body.String(), "argon2") {
		t.Error("the hash leaked through /me")
	}
}

func TestLogoutRevokesTheSessionAndClearsTheCookie(t *testing.T) {
	e := newEnv(t)
	sess := e.mustLogin("admin")
	rec := e.as(sess, http.MethodPost, "/api/auth/logout", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}
	if c := rec.Result().Cookies(); len(c) == 0 || c[0].Name != sessionCookie || c[0].MaxAge >= 0 || c[0].Value != "" {
		t.Errorf("cookie after logout = %+v", c)
	}
	if e.as(sess, http.MethodGet, "/api/auth/me", nil).Code != http.StatusUnauthorized {
		t.Error("the session works after logout")
	}
	if _, _, err := e.store.GetSession(context.Background(), auth.HashID(sess.cookie.Value)); err == nil {
		t.Error("the session row survived logout")
	}
}

func TestLogoutEndsOnlyThisSession(t *testing.T) {
	e := newEnv(t)
	a, b := e.mustLogin("admin"), e.mustLogin("admin")
	e.as(a, http.MethodPost, "/api/auth/logout", nil)
	if e.as(b, http.MethodGet, "/api/auth/me", nil).Code != http.StatusOK {
		t.Error("logging out of one session ended another")
	}
}

func TestPasswordChange(t *testing.T) {
	e := newEnv(t)
	e.addUser("alice", model.RoleViewer)
	one, two := e.mustLogin("alice"), e.mustLogin("alice")
	const next = "a brand new passphrase"

	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"wrong current password": {`{"currentPassword":"wrong wrong wrong","newPassword":"` + next + `"}`, "current password is incorrect"},
		"short new password":     {`{"currentPassword":"` + goodPassword + `","newPassword":"short"}`, "password is shorter than the minimum"},
		"long new password":      {`{"currentPassword":"` + goodPassword + `","newPassword":"` + strings.Repeat("a", 129) + `"}`, "password is longer than 128 characters"},
		"missing field":          {`{"currentPassword":"` + goodPassword + `"}`, "invalid request"},
		"unknown field":          {`{"currentPassword":"x","newPassword":"y","role":"admin"}`, "invalid request"},
	} {
		rec := e.as(one, http.MethodPut, "/api/auth/password", tc.body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("%s: %d %q, want 400 %q", name, rec.Code, rec.Body, tc.want)
		}
	}
	if e.as(one, http.MethodGet, "/api/auth/me", nil).Code != http.StatusOK {
		t.Fatal("a refused change ended the session")
	}

	rec := e.as(one, http.MethodPut, "/api/auth/password", map[string]string{"currentPassword": goodPassword, "newPassword": next})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("valid change = %d %s", rec.Code, rec.Body)
	}
	for name, s := range map[string]session{"this session": one, "another session": two} {
		if e.as(s, http.MethodGet, "/api/auth/me", nil).Code != http.StatusUnauthorized {
			t.Errorf("%s survived the password change", name)
		}
	}
	if _, rec := e.login("alice", goodPassword); rec.Code != http.StatusUnauthorized {
		t.Error("the old password still works")
	}
	if _, rec := e.login("alice", next); rec.Code != http.StatusOK {
		t.Errorf("the new password does not work: %d", rec.Code)
	}
}

func TestWrongCurrentPasswordCountsTowardsTheLockout(t *testing.T) {
	e := newEnv(t)
	e.addUser("alice", model.RoleViewer)
	sess := e.mustLogin("alice")
	for range 5 {
		e.as(sess, http.MethodPut, "/api/auth/password", map[string]string{"currentPassword": "wrong wrong wrong", "newPassword": "a brand new passphrase"})
	}
	rec := e.as(sess, http.MethodPut, "/api/auth/password", map[string]string{"currentPassword": goodPassword, "newPassword": "a brand new passphrase"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a stolen session could keep guessing the password: %d", rec.Code)
	}
	if _, lr := e.login("alice", goodPassword, from("198.51.100.99:1")); lr.Code != http.StatusUnauthorized {
		t.Error("the account was not locked by repeated wrong current passwords")
	}
}

func TestMustChangePasswordAllowsOnlyThePasswordChange(t *testing.T) {
	e := newEnv(t, withRoutes(permissionRoutes()...))
	if _, err := e.svc.CreateUser(context.Background(), auth.NewUser{Username: "newbie", Role: model.RoleAdmin, Password: goodPassword, MustChangePassword: true}); err != nil {
		t.Fatal(err)
	}
	sess, rec := e.login("newbie", goodPassword)
	if rec.Code != http.StatusOK {
		t.Fatalf("login = %d", rec.Code)
	}
	var me meResponse
	json.Unmarshal(rec.Body.Bytes(), &me)
	if !me.User.MustChangePassword {
		t.Fatal("the response does not tell the interface to ask for a new password")
	}

	for _, tt := range []struct{ method, path string }{
		{"GET", "/api/test/topology"}, {"GET", "/api/test/audit"}, {"POST", "/api/test/scans"}, {"DELETE", "/api/test/users"},
	} {
		r := e.as(sess, tt.method, tt.path, nil)
		if r.Code != http.StatusForbidden || !strings.Contains(r.Body.String(), "password change required") {
			t.Errorf("%s %s = %d %q, want 403 password change required", tt.method, tt.path, r.Code, r.Body)
		}
	}
	if e.as(sess, http.MethodGet, "/api/auth/me", nil).Code != http.StatusOK {
		t.Error("a user who must change the password cannot read /me")
	}

	if r := e.as(sess, http.MethodPut, "/api/auth/password", map[string]string{"currentPassword": goodPassword, "newPassword": "a brand new passphrase"}); r.Code != http.StatusNoContent {
		t.Fatalf("password change = %d %s", r.Code, r.Body)
	}
	again, rec := e.login("newbie", "a brand new passphrase")
	if rec.Code != http.StatusOK {
		t.Fatal("cannot sign in with the new password")
	}
	if code := e.as(again, http.MethodGet, "/api/test/audit", nil).Code; code != http.StatusOK {
		t.Errorf("after the change the admin still cannot use routes: %d", code)
	}
}

func TestAuditTrail(t *testing.T) {
	e := newEnv(t, withRoutes(permissionRoutes()...))
	e.addUser("vera", model.RoleViewer)

	e.login("bo\nb\x1b[31m", "whatever password", from("198.51.100.9:1")) // a failure with an unprintable name
	sess, _ := e.login("vera", goodPassword, from("198.51.100.10:1"))
	e.as(sess, http.MethodPost, "/api/test/scans", nil)                                               // denied: a viewer cannot scan
	e.do(http.MethodPost, "/api/test/scans", nil, func(r *http.Request) { r.AddCookie(sess.cookie) }) // no CSRF token
	e.as(sess, http.MethodPut, "/api/auth/password", map[string]string{"currentPassword": goodPassword, "newPassword": "a brand new passphrase"})
	again, _ := e.login("vera", "a brand new passphrase", from("198.51.100.10:1"))
	e.as(again, http.MethodPost, "/api/auth/logout", nil)

	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ action, result, user, ip string }
	var got []row
	for i := len(entries) - 1; i >= 0; i-- { // oldest first
		x := entries[i]
		got = append(got, row{x.Action, x.Result, x.Username, x.IP})
	}
	want := []row{
		{"login_failed", "denied", "bo?b?[31m", "198.51.100.9"},
		{"login", "ok", "vera", "198.51.100.10"},
		{"access_denied", "denied", "vera", "192.0.2.1"},
		{"access_denied", "denied", "vera", "192.0.2.1"},
		{"password_change", "ok", "vera", "192.0.2.1"},
		{"login", "ok", "vera", "198.51.100.10"},
		{"logout", "ok", "vera", "192.0.2.1"},
	}
	if len(got) != len(want) {
		t.Fatalf("%d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	for _, x := range entries {
		if strings.ContainsAny(x.Detail+x.Username, "\n\r\x1b") {
			t.Errorf("an audit entry carries control characters: %+v", x)
		}
	}
}

func TestNoSecretsInAuditLogsOrResponses(t *testing.T) {
	e := newEnv(t, withRoutes(permissionRoutes()...))
	e.addUser("vera", model.RoleViewer)
	const pw = "SuperSecretPassw0rd-ZZ"
	const pw2 = "AnotherSecretPassw0rd-YY"
	if _, err := e.svc.CreateUser(context.Background(), auth.NewUser{Username: "sam", Role: model.RoleViewer, Password: pw}); err != nil {
		t.Fatal(err)
	}

	var responses strings.Builder
	grab := func(rec interface{ String() string }) { responses.WriteString(rec.String()) }
	_, r1 := e.login("sam", "not "+pw)
	grab(r1.Body)
	s, r2 := e.login("sam", pw)
	grab(r2.Body)
	grab(e.as(s, http.MethodGet, "/api/auth/me", nil).Body)
	grab(e.as(s, http.MethodPost, "/api/test/scans", nil).Body)
	grab(e.as(s, http.MethodPut, "/api/auth/password", map[string]string{"currentPassword": "not " + pw, "newPassword": pw2}).Body)
	grab(e.as(s, http.MethodPut, "/api/auth/password", map[string]string{"currentPassword": pw, "newPassword": pw2}).Body)

	entries, _ := e.store.ListAudit(context.Background(), store.AuditFilter{Limit: 500})
	var audit strings.Builder
	for _, x := range entries {
		raw, _ := json.Marshal(x)
		audit.Write(raw)
	}
	for place, text := range map[string]string{"responses": responses.String(), "audit log": audit.String(), "server log": e.logs.String()} {
		secrets := []string{pw, pw2, "not " + pw, "argon2id", s.cookie.Value}
		if place != "responses" {
			secrets = append(secrets, s.csrf) // the CSRF token is meant to reach the client, never the logs
		}
		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Errorf("%s contain %q", place, secret)
			}
		}
	}
}

func TestAuditFailureDoesNotBreakTheRequest(t *testing.T) {
	e := newEnv(t, func(o *envOptions) { o.audit = failingAudit{} })
	sess, rec := e.login("admin", goodPassword)
	if rec.Code != http.StatusOK {
		t.Fatalf("login with a failing audit log = %d", rec.Code)
	}
	if rec := e.as(sess, http.MethodPost, "/api/auth/logout", nil); rec.Code != http.StatusNoContent {
		t.Errorf("logout with a failing audit log = %d", rec.Code)
	}
	if !strings.Contains(e.logs.String(), "write audit entry") {
		t.Error("the failure was not logged")
	}
}

func TestMethodsOtherThanTheDeclaredOnesAreRefused(t *testing.T) {
	e := newEnv(t)
	for _, tt := range []struct{ method, path string }{
		{"GET", "/api/auth/login"}, {"PUT", "/api/auth/login"}, {"GET", "/api/auth/logout"}, {"POST", "/api/auth/me"}, {"GET", "/api/auth/password"},
	} {
		if code := e.do(tt.method, tt.path, nil).Code; code != http.StatusMethodNotAllowed && code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 405 or 404", tt.method, tt.path, code)
		}
	}
}
