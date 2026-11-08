package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	idpClient = "netscribe-test"
	idpSecret = "s3cret-client-value"
)

// idpRepo adds the identity table to the in-memory repository of the session tests.
type idpRepo struct {
	*fakeRepo
	mu     sync.Mutex
	idents map[string]int64
}

func newIDPRepo(base *fakeRepo) *idpRepo { return &idpRepo{fakeRepo: base, idents: map[string]int64{}} }

func identKey(issuer, subject string) string { return issuer + "\x00" + subject }

func (r *idpRepo) FindIdentityUser(ctx context.Context, issuer, subject string) (model.User, error) {
	r.mu.Lock()
	id, ok := r.idents[identKey(issuer, subject)]
	r.mu.Unlock()
	if !ok {
		return model.User{}, model.ErrNotFound
	}
	return r.GetUser(ctx, id)
}

func (r *idpRepo) CreateIdentityUser(ctx context.Context, u model.UserRecord, issuer, subject, _ string) (model.User, error) {
	created, err := r.CreateUser(ctx, u)
	if err != nil {
		return model.User{}, err
	}
	r.mu.Lock()
	r.idents[identKey(issuer, subject)] = created.ID
	r.mu.Unlock()
	return created, nil
}

// testIDP is an identity provider that runs in the test process: the discovery document, the
// keys, and a token endpoint that hands out the ID tokens the test prepared.
type testIDP struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]idpCode
}

type idpCode struct {
	challenge string
	claims    map[string]any
	key       *rsa.PrivateKey
}

func newTestIDP(t *testing.T) *testIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &testIDP{t: t, key: key, codes: map[string]idpCode{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.srv.URL,
			"authorization_endpoint":                p.srv.URL + "/authorize",
			"token_endpoint":                        p.srv.URL + "/token",
			"jwks_uri":                              p.srv.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "k1",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", p.token)
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *testIDP) token(w http.ResponseWriter, r *http.Request) {
	// Without this the library reads a JSON answer as a form and finds no token.
	w.Header().Set("Content-Type", "application/json")
	if r.ParseForm() != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	c, ok := p.codes[r.Form.Get("code")]
	delete(p.codes, r.Form.Get("code")) // a code works once
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at", "token_type": "Bearer", "expires_in": 3600,
		"id_token": signJWT(p.t, c.key, c.claims),
	})
}

func signJWT(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func idpConfig(p *testIDP) config.OIDCConfig {
	return config.OIDCConfig{
		Name: "Acme", Issuer: p.srv.URL, ClientID: idpClient, ClientSecret: config.Secret(idpSecret),
		RedirectURL: "http://127.0.0.1/api/auth/oidc/callback", Scopes: []string{"openid", "profile", "email"},
		AllowedEmailDomains: []string{"example.com"}, AutoCreate: true, DefaultRole: "viewer",
		RoleClaim: "groups", RoleMap: map[string]string{"ns-admins": "admin", "ns-ops": "operator"},
	}
}

// claimsFor are the claims of a good ID token for this provider.
func (p *testIDP) claimsFor(nonce, subject, email string) map[string]any {
	return map[string]any{
		"iss": p.srv.URL, "sub": subject, "aud": idpClient,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"nonce": nonce, "email": email, "email_verified": true, "name": "Test Person",
	}
}

type idpFixture struct {
	idp  *testIDP
	oidc *OIDC
	svc  *Service
	repo *idpRepo
}

func newIDPFixture(t *testing.T, edit func(*config.OIDCConfig)) *idpFixture {
	t.Helper()
	svc, base, clk := newTestService(t)
	repo := newIDPRepo(base)
	idp := newTestIDP(t)
	cfg := idpConfig(idp)
	if edit != nil {
		edit(&cfg)
	}
	return &idpFixture{idp: idp, oidc: NewOIDC(cfg, repo, svc, clk.now, nil), svc: svc, repo: repo}
}

// begin starts a sign-in and reads back what the provider would have been sent.
func (f *idpFixture) begin(t *testing.T) (Flow, url.Values) {
	t.Helper()
	target, flow, err := f.oidc.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	u, err := url.Parse(target)
	if err != nil || !strings.HasPrefix(target, f.idp.srv.URL+"/authorize") {
		t.Fatalf("Begin sent the browser to %q", target)
	}
	return flow, u.Query()
}

// signIn runs a whole sign-in. edit changes the claims before the token is signed.
func (f *idpFixture) signIn(t *testing.T, subject, email string, edit func(map[string]any)) (OIDCResult, error) {
	t.Helper()
	flow, q := f.begin(t)
	claims := f.idp.claimsFor(q.Get("nonce"), subject, email)
	if edit != nil {
		edit(claims)
	}
	code := "code-" + subject + "-" + flow.State[:6]
	f.idp.mu.Lock()
	f.idp.codes[code] = idpCode{challenge: q.Get("code_challenge"), claims: claims, key: f.idp.key}
	f.idp.mu.Unlock()
	return f.oidc.Complete(context.Background(), flow, flow.State, code, "10.0.0.9", "agent")
}

func TestOIDCSignInCreatesAUserAndOpensASession(t *testing.T) {
	f := newIDPFixture(t, nil)
	flow, q := f.begin(t)
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") != flow.State || q.Get("nonce") != flow.Nonce {
		t.Fatalf("the request to the provider lacks PKCE, state or nonce: %v", q)
	}
	res, err := f.signIn(t, "sub-1", "Jane.Doe@Example.com", func(c map[string]any) { c["groups"] = []string{"staff", "ns-ops"} })
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !res.Created || res.User.Role != model.RoleOperator {
		t.Fatalf("created %v role %q, want a new operator", res.Created, res.User.Role)
	}
	if !strings.HasPrefix(res.User.Username, "jane.doe-") || ValidateUsername(res.User.Username) != nil {
		t.Fatalf("username %q is not a valid generated name", res.User.Username)
	}
	p, err := f.svc.Authenticate(context.Background(), res.Session.ID)
	if err != nil || p.User.ID != res.User.ID {
		t.Fatalf("the session does not belong to the new user: %v", err)
	}

	// The second sign-in finds the same user and creates nothing.
	again, err := f.signIn(t, "sub-1", "Jane.Doe@Example.com", nil)
	if err != nil || again.Created || again.User.ID != res.User.ID {
		t.Fatalf("second sign in: %+v, %v", again, err)
	}
}

func TestOIDCRefusalsAreAllTheSameError(t *testing.T) {
	cases := map[string]func(map[string]any){
		"wrong nonce":    func(c map[string]any) { c["nonce"] = "another" },
		"wrong audience": func(c map[string]any) { c["aud"] = "someone-else" },
		"wrong issuer":   func(c map[string]any) { c["iss"] = "https://evil.example.com" },
		"expired":        func(c map[string]any) { c["exp"] = time.Now().Add(-time.Hour).Unix() },
		"unverified":     func(c map[string]any) { c["email_verified"] = false },
		"no email":       func(c map[string]any) { delete(c, "email") },
		"no subject":     func(c map[string]any) { c["sub"] = "" },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			f := newIDPFixture(t, nil)
			_, err := f.signIn(t, "sub-x", "x@example.com", edit)
			if !errors.Is(err, ErrOIDCDenied) {
				t.Fatalf("got %v, want ErrOIDCDenied", err)
			}
			if strings.Contains(err.Error(), idpSecret) {
				t.Fatalf("the error holds the client secret: %v", err)
			}
			if n, _ := f.repo.CountUsers(context.Background()); n != 0 {
				t.Fatalf("a refused sign in created %d users", n)
			}
		})
	}
}

func TestOIDCRefusesATokenSignedWithAnotherKey(t *testing.T) {
	f := newIDPFixture(t, nil)
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	flow, q := f.begin(t)
	f.idp.mu.Lock()
	f.idp.codes["c1"] = idpCode{challenge: q.Get("code_challenge"), claims: f.idp.claimsFor(q.Get("nonce"), "s", "s@example.com"), key: other}
	f.idp.mu.Unlock()
	if _, err := f.oidc.Complete(context.Background(), flow, flow.State, "c1", "ip", "ua"); !errors.Is(err, ErrOIDCDenied) {
		t.Fatalf("got %v, want ErrOIDCDenied", err)
	}
}

func TestOIDCRefusesAWrongStateAndAReplayedCode(t *testing.T) {
	f := newIDPFixture(t, nil)
	flow, q := f.begin(t)
	f.idp.mu.Lock()
	f.idp.codes["c1"] = idpCode{challenge: q.Get("code_challenge"), claims: f.idp.claimsFor(q.Get("nonce"), "s", "s@example.com"), key: f.idp.key}
	f.idp.mu.Unlock()

	if _, err := f.oidc.Complete(context.Background(), flow, "not-the-state", "c1", "ip", "ua"); !errors.Is(err, ErrOIDCDenied) {
		t.Fatalf("wrong state: got %v", err)
	}
	if _, err := f.oidc.Complete(context.Background(), Flow{}, "", "c1", "ip", "ua"); !errors.Is(err, ErrOIDCDenied) {
		t.Fatalf("empty flow: got %v", err)
	}
	if _, err := f.oidc.Complete(context.Background(), flow, flow.State, "c1", "ip", "ua"); err != nil {
		t.Fatalf("the right answer was refused: %v", err)
	}
	if _, err := f.oidc.Complete(context.Background(), flow, flow.State, "c1", "ip", "ua"); !errors.Is(err, ErrOIDCDenied) {
		t.Fatalf("replayed code: got %v", err)
	}
}

func TestOIDCFindsAUserByIssuerAndSubjectNeverByEmail(t *testing.T) {
	f := newIDPFixture(t, nil)
	local := addUser(t, f.svc, "jane", model.RoleAdmin)

	a, err := f.signIn(t, "sub-a", "jane@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.signIn(t, "sub-b", "jane@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.User.ID == b.User.ID {
		t.Fatal("two subjects with one e-mail address became one user")
	}
	if a.User.ID == local.ID || b.User.ID == local.ID {
		t.Fatal("a local user was taken over by an e-mail match")
	}
	if a.User.Role == model.RoleAdmin {
		t.Fatal("the new user got the role of the local user with the same name")
	}
}

func TestOIDCCreatesNothingWhenTheConfigDoesNotAllowIt(t *testing.T) {
	cases := map[string]struct {
		edit  func(*config.OIDCConfig)
		email string
	}{
		"autoCreate off":     {edit: func(c *config.OIDCConfig) { c.AutoCreate = false }, email: "a@example.com"},
		"domain not listed":  {email: "a@other.org"},
		"no domain list":     {edit: func(c *config.OIDCConfig) { c.AllowedEmailDomains = nil }, email: "a@example.com"},
		"look-alike domain":  {email: "a@evil-example.com"},
		"subdomain not same": {email: "a@mail.example.com"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newIDPFixture(t, tc.edit)
			if _, err := f.signIn(t, "sub", tc.email, nil); !errors.Is(err, ErrOIDCDenied) {
				t.Fatalf("got %v, want ErrOIDCDenied", err)
			}
			if n, _ := f.repo.CountUsers(context.Background()); n != 0 {
				t.Fatalf("%d users were created", n)
			}
		})
	}
}

func TestOIDCRoleFollowsTheProviderAtEverySignIn(t *testing.T) {
	f := newIDPFixture(t, nil)
	groups := func(g ...string) func(map[string]any) { return func(c map[string]any) { c["groups"] = g } }

	first, err := f.signIn(t, "sub", "p@example.com", groups("ns-admins", "ns-ops"))
	if err != nil || first.User.Role != model.RoleAdmin {
		t.Fatalf("the best mapped role should win: %+v, %v", first.User, err)
	}
	down, err := f.signIn(t, "sub", "p@example.com", groups("ns-ops"))
	if err != nil || down.User.Role != model.RoleOperator {
		t.Fatalf("a role taken away at the provider must apply: %+v, %v", down.User, err)
	}
	none, err := f.signIn(t, "sub", "p@example.com", groups("unknown-group"))
	if err != nil || none.User.Role != model.RoleViewer {
		t.Fatalf("no mapped value must fall to defaultRole: %+v, %v", none.User, err)
	}
	single, err := f.signIn(t, "sub", "p@example.com", func(c map[string]any) { c["groups"] = "ns-ops" })
	if err != nil || single.User.Role != model.RoleOperator {
		t.Fatalf("a single string claim must work: %+v, %v", single.User, err)
	}
}

func TestOIDCRefusesADisabledUser(t *testing.T) {
	f := newIDPFixture(t, nil)
	res, err := f.signIn(t, "sub", "d@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	off := true
	if _, err := f.repo.UpdateUser(context.Background(), res.User.ID, nil, &off); err != nil {
		t.Fatal(err)
	}
	if _, err := f.signIn(t, "sub", "d@example.com", nil); !errors.Is(err, ErrOIDCDenied) {
		t.Fatalf("got %v, want ErrOIDCDenied", err)
	}
}

func TestAnAccountOfSingleSignOnCannotUseAPassword(t *testing.T) {
	f := newIDPFixture(t, nil)
	res, err := f.signIn(t, "sub", "pw@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, _, err := f.svc.Login(ctx, res.User.Username, "any password at all", "10.0.0.1", "ua"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login got %v, want ErrInvalidCredentials", err)
	}
	p := Principal{User: res.User}
	if err := f.svc.ChangePassword(ctx, p, "x", "another long password 1"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("ChangePassword got %v, want ErrWrongPassword", err)
	}
	if err := f.svc.ConfirmPassword(ctx, p, "x"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("ConfirmPassword got %v, want ErrWrongPassword", err)
	}
}

func TestParseFlowRejectsWhatIsNotOurs(t *testing.T) {
	good := Flow{State: "a", Nonce: "b", Verifier: "c"}
	if got, ok := ParseFlow(good.Encode()); !ok || got != good {
		t.Fatalf("round trip: %+v %v", got, ok)
	}
	for _, bad := range []string{"", "a.b", "a.b.c.d", "..", "a..c", strings.Repeat("x", 600) + ".b.c"} {
		if _, ok := ParseFlow(bad); ok {
			t.Errorf("ParseFlow(%q) accepted it", bad)
		}
	}
}

func TestOIDCBeginFailsCleanlyWhenTheProviderIsDown(t *testing.T) {
	f := newIDPFixture(t, nil)
	f.idp.srv.Close()
	if _, _, err := f.oidc.Begin(context.Background()); err == nil {
		t.Fatal("Begin succeeded with no provider")
	} else if errors.Is(err, ErrOIDCDenied) || strings.Contains(err.Error(), idpSecret) {
		t.Fatalf("unexpected error: %v", err)
	}
}
