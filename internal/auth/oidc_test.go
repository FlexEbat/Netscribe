package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FlexEbat/Netscribe/internal/auth/authtest"
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

func idpConfig(p *authtest.IDP) config.OIDCConfig {
	return config.OIDCConfig{
		Name: "Acme", Issuer: p.URL, ClientID: idpClient, ClientSecret: config.Secret(idpSecret),
		RedirectURL: "http://127.0.0.1/api/auth/oidc/callback", Scopes: []string{"openid", "profile", "email"},
		AllowedEmailDomains: []string{"example.com"}, AutoCreate: true, DefaultRole: "viewer",
		RoleClaim: "groups", RoleMap: map[string]string{"ns-admins": "admin", "ns-ops": "operator"},
	}
}

type idpFixture struct {
	idp  *authtest.IDP
	oidc *OIDC
	svc  *Service
	repo *idpRepo
}

func newIDPFixture(t *testing.T, edit func(*config.OIDCConfig)) *idpFixture {
	t.Helper()
	svc, base, clk := newTestService(t)
	repo := newIDPRepo(base)
	idp := authtest.New(t)
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
	if err != nil || !strings.HasPrefix(target, f.idp.URL+"/authorize") {
		t.Fatalf("Begin sent the browser to %q", target)
	}
	return flow, u.Query()
}

// signIn runs a whole sign-in. edit changes the claims before the token is signed.
func (f *idpFixture) signIn(t *testing.T, subject, email string, edit func(map[string]any)) (OIDCResult, error) {
	t.Helper()
	flow, q := f.begin(t)
	claims := f.idp.Claims(q.Get("nonce"), subject, email, idpClient)
	if edit != nil {
		edit(claims)
	}
	code := "code-" + subject + "-" + flow.State[:6]
	f.idp.Register(code, q.Get("code_challenge"), claims, nil)
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
	f.idp.Register("c1", q.Get("code_challenge"), f.idp.Claims(q.Get("nonce"), "s", "s@example.com", idpClient), other)
	if _, err := f.oidc.Complete(context.Background(), flow, flow.State, "c1", "ip", "ua"); !errors.Is(err, ErrOIDCDenied) {
		t.Fatalf("got %v, want ErrOIDCDenied", err)
	}
}

func TestOIDCRefusesAWrongStateAndAReplayedCode(t *testing.T) {
	f := newIDPFixture(t, nil)
	flow, q := f.begin(t)
	f.idp.Register("c1", q.Get("code_challenge"), f.idp.Claims(q.Get("nonce"), "s", "s@example.com", idpClient), nil)

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
	f.idp.Close()
	if _, _, err := f.oidc.Begin(context.Background()); err == nil {
		t.Fatal("Begin succeeded with no provider")
	} else if errors.Is(err, ErrOIDCDenied) || strings.Contains(err.Error(), idpSecret) {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The claims of the three providers of slice 45, as their documents describe them.

func TestOIDCKeycloakRolesInANestedClaim(t *testing.T) {
	f := newIDPFixture(t, func(c *config.OIDCConfig) {
		c.Provider = "keycloak"
		c.RoleClaim = "realm_access.roles"
		c.RoleMap = map[string]string{"netscribe-admin": "admin", "netscribe-ops": "operator"}
	})
	res, err := f.signIn(t, "kc-1", "kc@example.com", func(c map[string]any) {
		c["realm_access"] = map[string]any{"roles": []string{"offline_access", "netscribe-ops"}}
		c["resource_access"] = map[string]any{"account": map[string]any{"roles": []string{"view-profile"}}}
	})
	if err != nil || res.User.Role != model.RoleOperator {
		t.Fatalf("a role in realm_access.roles must map: %+v, %v", res.User, err)
	}
	// A path that leads nowhere is no role, not an error.
	res, err = f.signIn(t, "kc-2", "kc2@example.com", nil)
	if err != nil || res.User.Role != model.RoleViewer {
		t.Fatalf("a missing claim must give the default role: %+v, %v", res.User, err)
	}
}

func TestOIDCAClaimNameWithDotsIsTriedWhole(t *testing.T) {
	f := newIDPFixture(t, func(c *config.OIDCConfig) {
		c.RoleClaim = "https://example.com/roles"
		c.RoleMap = map[string]string{"admin": "admin"}
	})
	res, err := f.signIn(t, "s", "d@example.com", func(c map[string]any) {
		c["https://example.com/roles"] = []string{"admin"}
	})
	if err != nil || res.User.Role != model.RoleAdmin {
		t.Fatalf("a claim named with dots must work whole: %+v, %v", res.User, err)
	}
}

func TestOIDCEntraSignsInWithNoVerifiedFlagAndTheAddressInPreferredUsername(t *testing.T) {
	f := newIDPFixture(t, func(c *config.OIDCConfig) {
		c.Provider = "entra"
		c.RoleClaim = "roles" // app roles: a top level list of values
		c.RoleMap = map[string]string{"Netscribe.Admin": "admin"}
	})
	res, err := f.signIn(t, "entra-oid-1", "", func(c map[string]any) {
		delete(c, "email")
		delete(c, "email_verified")
		c["preferred_username"] = "Jane.Doe@example.com"
		c["roles"] = []string{"Netscribe.Admin"}
	})
	if err != nil || res.User.Role != model.RoleAdmin || !res.Created {
		t.Fatalf("entra claims must sign in: %+v, %v", res, err)
	}
}

func TestOIDCEntraPrefersTheEmailClaimAndSkipsALoginNameThatIsNoAddress(t *testing.T) {
	f := newIDPFixture(t, func(c *config.OIDCConfig) { c.Provider = "entra" })
	// preferred_username that is not an address, no email claim: nothing to use.
	_, err := f.signIn(t, "e1", "", func(c map[string]any) {
		delete(c, "email")
		delete(c, "email_verified")
		c["preferred_username"] = "jdoe"
	})
	if !errors.Is(err, ErrOIDCDenied) || Reason(err) != "no email" {
		t.Fatalf("got %v (%q), want a refusal for no email", err, Reason(err))
	}
	// Both given: email wins.
	res, err := f.signIn(t, "e2", "real@example.com", func(c map[string]any) {
		delete(c, "email_verified")
		c["preferred_username"] = "other@example.com"
	})
	if err != nil || !strings.HasPrefix(res.User.Username, "real-") {
		t.Fatalf("the email claim must win: %+v, %v", res.User, err)
	}
}

func TestOIDCAGenericProviderStillWantsAVerifiedAddress(t *testing.T) {
	f := newIDPFixture(t, nil)
	_, err := f.signIn(t, "g1", "g@example.com", func(c map[string]any) { delete(c, "email_verified") })
	if !errors.Is(err, ErrOIDCDenied) || Reason(err) != "email not verified" {
		t.Fatalf("got %v (%q)", err, Reason(err))
	}
}

func TestOIDCReadsAVerifiedFlagSentAsText(t *testing.T) {
	f := newIDPFixture(t, nil)
	if _, err := f.signIn(t, "t1", "t@example.com", func(c map[string]any) { c["email_verified"] = "true" }); err != nil {
		t.Fatalf(`"true" as text must count: %v`, err)
	}
	if _, err := f.signIn(t, "t2", "t2@example.com", func(c map[string]any) { c["email_verified"] = "false" }); !errors.Is(err, ErrOIDCDenied) {
		t.Fatalf(`"false" as text must refuse: %v`, err)
	}
}

func TestOIDCAnotherEmailClaimAndNoVerifiedFlagOnRequest(t *testing.T) {
	no := false
	f := newIDPFixture(t, func(c *config.OIDCConfig) {
		c.EmailClaim = "mail"
		c.RequireVerifiedEmail = &no
	})
	res, err := f.signIn(t, "m1", "", func(c map[string]any) {
		delete(c, "email")
		delete(c, "email_verified")
		c["mail"] = "ldap@example.com"
	})
	if err != nil || !res.Created {
		t.Fatalf("emailClaim and requireVerifiedEmail: %+v, %v", res, err)
	}
}

func TestOIDCAuthentikGroupsAsNames(t *testing.T) {
	f := newIDPFixture(t, func(c *config.OIDCConfig) {
		c.Provider = "authentik"
		c.RoleClaim = "groups"
		c.RoleMap = map[string]string{"authentik Admins": "admin", "netscribe operators": "operator"}
	})
	res, err := f.signIn(t, "a1", "a@example.com", func(c map[string]any) {
		c["groups"] = []string{"authentik Admins", "everyone"}
		c["preferred_username"] = "akadmin"
	})
	if err != nil || res.User.Role != model.RoleAdmin {
		t.Fatalf("authentik groups must map: %+v, %v", res.User, err)
	}
}

func TestClaimStrings(t *testing.T) {
	claims := map[string]any{
		"a":      "x",
		"b":      []any{"y", 3, "z"},
		"c":      map[string]any{"d": map[string]any{"e": []any{"deep"}}},
		"f.g":    []any{"whole"},
		"number": 7,
	}
	tests := []struct {
		name string
		want []string
	}{
		{"a", []string{"x"}},
		{"b", []string{"y", "z"}},
		{"c.d.e", []string{"deep"}},
		{"f.g", []string{"whole"}},
		{"c.d.missing", nil},
		{"a.b", nil},
		{"number", nil},
		{"nothing", nil},
	}
	for _, tt := range tests {
		got := claimStrings(claims, tt.name)
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("claimStrings(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
