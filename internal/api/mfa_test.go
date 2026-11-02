package api

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/model"
	"github.com/FlexEbat/Netscribe/internal/store"
	"github.com/FlexEbat/Netscribe/internal/totp"
)

const mfaIP = "198.18.77.7:1"

type enrolled struct {
	secret   []byte
	recovery []string
}

// enroll turns the second factor on for the signed in user and returns what a phone would hold.
func enroll(t *testing.T, e *env, s session) enrolled {
	t.Helper()
	rec := asWith(e, s, http.MethodPost, "/api/auth/totp/setup", map[string]string{"password": goodPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	var setup struct{ Secret, URI string }
	if err := json.Unmarshal(rec.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Secret)
	if err != nil {
		t.Fatal(err)
	}
	code := totp.Code(secret, e.clk.now())
	rec = asWith(e, s, http.MethodPost, "/api/auth/totp/enable", map[string]string{"code": code})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body)
	}
	var done struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &done); err != nil {
		t.Fatal(err)
	}
	return enrolled{secret: secret, recovery: done.RecoveryCodes}
}

// passwordStep signs in with the password and returns the challenge.
func passwordStep(t *testing.T, e *env, name string) string {
	t.Helper()
	s, rec := e.login(name, goodPassword, from(mfaIP))
	if rec.Code != http.StatusOK || s.cookie != nil {
		t.Fatalf("password step: %d, cookie %v, body %s", rec.Code, s.cookie, rec.Body)
	}
	var body struct {
		MFA       bool   `json:"mfa"`
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || !body.MFA || body.Challenge == "" {
		t.Fatalf("body = %s", rec.Body)
	}
	return body.Challenge
}

func codeStep(e *env, challenge, code string) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, "/api/auth/login/totp", map[string]string{"challenge": challenge, "code": code}, from(mfaIP))
}

func TestSetupNeedsThePasswordAndTurnsNothingOn(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	if code := asWith(e, admin, http.MethodPost, "/api/auth/totp/setup", map[string]string{"password": "wrong wrong"}).Code; code != http.StatusForbidden {
		t.Fatalf("wrong password: %d, want 403", code)
	}
	rec := asWith(e, admin, http.MethodPost, "/api/auth/totp/setup", map[string]string{"password": goodPassword})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "otpauth://totp/Netscribe:admin") {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}
	// a begun setup does not change how people sign in
	if _, rec := e.login("admin", goodPassword, from(mfaIP)); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user"`) {
		t.Errorf("an unfinished setup asked for a code: %s", rec.Body)
	}
	if code := asWith(e, admin, http.MethodPost, "/api/auth/totp/enable", map[string]string{"code": "000000"}).Code; code != http.StatusBadRequest {
		t.Errorf("a wrong code: %d, want 400", code)
	}
}

func TestSignInAsksForTheSecondFactorAndOpensNoSessionWithoutIt(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	phone := enroll(t, e, admin)
	if len(phone.recovery) != 10 {
		t.Fatalf("%d recovery codes", len(phone.recovery))
	}
	challenge := passwordStep(t, e, "admin")

	rec := codeStep(e, challenge, "000000")
	if rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
		t.Fatalf("a wrong code: %d, cookies %v", rec.Code, rec.Result().Cookies())
	}
	e.clk.advance(31 * time.Second)
	rec = codeStep(e, challenge, totp.Code(phone.secret, e.clk.now()))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Result().Header.Get("Set-Cookie"), sessionCookie+"=") {
		t.Fatalf("the right code: %d, %s", rec.Code, rec.Body)
	}
	if rec := codeStep(e, challenge, totp.Code(phone.secret, e.clk.now())); rec.Code != http.StatusUnauthorized {
		t.Errorf("a challenge worked twice: %d", rec.Code)
	}
}

func TestACodeWorksOnceAcrossTwoSignIns(t *testing.T) {
	e := newEnv(t)
	phone := enroll(t, e, e.mustLogin("admin"))
	e.clk.advance(31 * time.Second)
	code := totp.Code(phone.secret, e.clk.now())
	if rec := codeStep(e, passwordStep(t, e, "admin"), code); rec.Code != http.StatusOK {
		t.Fatalf("first use: %d", rec.Code)
	}
	if rec := codeStep(e, passwordStep(t, e, "admin"), code); rec.Code != http.StatusUnauthorized {
		t.Errorf("the same code worked twice: %d", rec.Code)
	}
}

func TestAChallengeEndsAfterFiveTriesAndAfterFiveMinutes(t *testing.T) {
	e := newEnv(t)
	phone := enroll(t, e, e.mustLogin("admin"))
	e.clk.advance(31 * time.Second)
	challenge := passwordStep(t, e, "admin")
	for i := 0; i < 5; i++ {
		codeStep(e, challenge, "111111")
	}
	if rec := codeStep(e, challenge, totp.Code(phone.secret, e.clk.now())); rec.Code != http.StatusUnauthorized {
		t.Errorf("a right code after five wrong ones: %d, want 401", rec.Code)
	}
	challenge = passwordStep(t, e, "admin")
	e.clk.advance(6 * time.Minute)
	if rec := codeStep(e, challenge, totp.Code(phone.secret, e.clk.now())); rec.Code != http.StatusUnauthorized {
		t.Errorf("an expired challenge worked: %d", rec.Code)
	}
}

func TestARecoveryCodeWorksOnce(t *testing.T) {
	e := newEnv(t)
	phone := enroll(t, e, e.mustLogin("admin"))
	code := strings.ToLower(phone.recovery[0])
	if rec := codeStep(e, passwordStep(t, e, "admin"), code); rec.Code != http.StatusOK {
		t.Fatalf("a recovery code: %d %s", rec.Code, rec.Body)
	}
	if rec := codeStep(e, passwordStep(t, e, "admin"), code); rec.Code != http.StatusUnauthorized {
		t.Errorf("a recovery code worked twice: %d", rec.Code)
	}
	rec := codeStep(e, passwordStep(t, e, "admin"), phone.recovery[1])
	if rec.Code != http.StatusOK {
		t.Errorf("another recovery code: %d", rec.Code)
	}
}

func TestAWrongPasswordNeverReachesTheSecondStep(t *testing.T) {
	e := newEnv(t)
	enroll(t, e, e.mustLogin("admin"))
	_, rec := e.login("admin", "not the password", from(mfaIP))
	if rec.Code != http.StatusUnauthorized || strings.Contains(rec.Body.String(), "challenge") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestTurningTheSecondFactorOff(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	phone := enroll(t, e, admin)
	e.clk.advance(31 * time.Second)
	code := totp.Code(phone.secret, e.clk.now())
	if c := asWith(e, admin, http.MethodDelete, "/api/auth/totp", map[string]string{"password": "wrong wrong", "code": code}).Code; c != http.StatusForbidden {
		t.Errorf("wrong password: %d, want 403", c)
	}
	if c := asWith(e, admin, http.MethodDelete, "/api/auth/totp", map[string]string{"password": goodPassword, "code": "000000"}).Code; c != http.StatusForbidden {
		t.Errorf("wrong code: %d, want 403", c)
	}
	if c := asWith(e, admin, http.MethodDelete, "/api/auth/totp", map[string]string{"password": goodPassword, "code": code}).Code; c != http.StatusNoContent {
		t.Fatalf("off: %d, want 204", c)
	}
	if _, rec := e.login("admin", goodPassword, from(mfaIP)); !strings.Contains(rec.Body.String(), `"user"`) {
		t.Errorf("a second factor that is off still asks: %s", rec.Body)
	}
}

func TestAnAdministratorCanResetAnotherPersonsSecondFactor(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	e.addUser("olga", model.RoleOperator)
	olga := e.mustLogin("olga")
	enroll(t, e, olga)
	var id int64
	users, err := e.store.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		if u.Username == "olga" {
			id = u.ID
		}
	}
	path := "/api/users/" + strconv.FormatInt(id, 10) + "/totp/reset"
	if c := asWith(e, olga, http.MethodPost, path, nil).Code; c != http.StatusForbidden {
		t.Errorf("an operator: %d, want 403", c)
	}
	if c := asWith(e, admin, http.MethodPost, path, nil).Code; c != http.StatusNoContent {
		t.Fatalf("an administrator: %d, want 204", c)
	}
	if c := asWith(e, olga, http.MethodGet, "/api/auth/me", nil).Code; c != http.StatusUnauthorized {
		t.Errorf("the sessions of the person did not end: %d", c)
	}
	if _, rec := e.login("olga", goodPassword, from(mfaIP)); !strings.Contains(rec.Body.String(), `"user"`) {
		t.Errorf("after the reset a code is still asked: %s", rec.Body)
	}
	entries, err := e.store.ListAudit(context.Background(), store.AuditFilter{Action: "totp_reset", Limit: 5})
	if err != nil || len(entries) != 1 {
		t.Errorf("audit = %+v, %v", entries, err)
	}
}

func TestSecondFactorRoutesAreNotForTokens(t *testing.T) {
	e := newEnv(t)
	admin := e.mustLogin("admin")
	tok := issueToken(t, e, admin, map[string]any{"name": "x", "scope": "write"})
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/api/auth/totp"}, {http.MethodPost, "/api/auth/totp/setup"},
		{http.MethodPost, "/api/auth/totp/enable"}, {http.MethodDelete, "/api/auth/totp"},
		{http.MethodPost, "/api/users/1/totp/reset"},
	} {
		if code := e.do(c.method, c.path, nil, bearer(tok.Value)).Code; code != http.StatusForbidden {
			t.Errorf("%s %s with a token: %d, want 403", c.method, c.path, code)
		}
	}
}
