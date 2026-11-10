package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/FlexEbat/Netscribe/internal/config"
	"github.com/FlexEbat/Netscribe/internal/model"
)

const (
	oidcTimeout   = 10 * time.Second // every request to the provider
	oidcBodyLimit = 1 << 20          // the largest answer read from the provider
)

// ErrOIDCDenied is every refusal of a single sign-on: the caller shows the same page for all
// of them, and the reason is only for the log and the audit entry.
var ErrOIDCDenied = errors.New("single sign-on refused")

type oidcDenial struct{ reason string }

func (d oidcDenial) Error() string        { return "single sign-on refused: " + d.reason }
func (d oidcDenial) Is(target error) bool { return target == ErrOIDCDenied }

// Reason returns the short reason of a refusal, or "" for any other error.
func Reason(err error) string {
	var d oidcDenial
	if errors.As(err, &d) {
		return d.reason
	}
	return ""
}

func denied(reason string) error { return oidcDenial{reason: reason} }

// IdentityRepo is the part of the data layer single sign-on uses. *store.Store implements it.
type IdentityRepo interface {
	FindIdentityUser(ctx context.Context, issuer, subject string) (model.User, error) // model.ErrNotFound
	CreateIdentityUser(ctx context.Context, u model.UserRecord, issuer, subject, email string) (model.User, error)
	UpdateUser(ctx context.Context, id int64, role *model.Role, disabled *bool) (model.User, error)
	RecordLogin(ctx context.Context, id int64, ok bool, lockAfter int, lockFor time.Duration) error
}

// Flow is what a sign-in keeps between the redirect to the provider and the return. It lives
// in a short cookie of the browser that began the sign-in.
type Flow struct {
	State, Nonce, Verifier string
}

// Encode packs a flow into a cookie value.
func (f Flow) Encode() string { return f.State + "." + f.Nonce + "." + f.Verifier }

// ParseFlow reads a cookie value. A malformed one gives ok false.
func ParseFlow(v string) (Flow, bool) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" || len(v) > 512 {
		return Flow{}, false
	}
	return Flow{State: parts[0], Nonce: parts[1], Verifier: parts[2]}, true
}

// OIDC signs people in through an identity provider with the authorization code flow, PKCE,
// a state and a nonce. The provider is asked on first use and kept, so a provider that is
// down when the service starts does not stop the service.
type OIDC struct {
	cfg  config.OIDCConfig
	repo IdentityRepo
	svc  *Service
	now  func() time.Time
	hc   *http.Client

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// NewOIDC returns the sign-in with the provider named in cfg. cfg must be valid and enabled.
// A nil client means a client with the limits of section 9.6: 10 seconds, 1 MiB and no
// redirect to another host.
func NewOIDC(cfg config.OIDCConfig, repo IdentityRepo, svc *Service, now func() time.Time, hc *http.Client) *OIDC {
	if now == nil {
		now = time.Now
	}
	if hc == nil {
		hc = newOIDCClient()
	}
	return &OIDC{cfg: cfg, repo: repo, svc: svc, now: now, hc: hc}
}

// Name is the label of the sign-in button.
func (o *OIDC) Name() string { return o.cfg.ButtonName() }

func newOIDCClient() *http.Client {
	return &http.Client{
		Timeout:   oidcTimeout,
		Transport: limitedTransport{next: http.DefaultTransport},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Host != via[0].URL.Host {
				return errors.New("redirect to another host")
			}
			return nil
		},
	}
}

// limitedTransport cuts every answer at oidcBodyLimit.
type limitedTransport struct{ next http.RoundTripper }

func (t limitedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	resp.Body = limitedBody{Reader: io.LimitReader(resp.Body, oidcBodyLimit), Closer: resp.Body}
	return resp, nil
}

type limitedBody struct {
	io.Reader
	io.Closer
}

func (o *OIDC) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 2*oidcTimeout)
	return oidc.ClientContext(ctx, o.hc), cancel
}

// ready asks the provider once and keeps the answer.
func (o *OIDC) ready(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.oauth != nil {
		return o.oauth, o.verifier, nil
	}
	p, err := oidc.NewProvider(ctx, o.cfg.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("ask the provider: %w", err)
	}
	o.oauth = &oauth2.Config{
		ClientID:     o.cfg.ClientID,
		ClientSecret: string(o.cfg.ClientSecret),
		Endpoint:     p.Endpoint(),
		RedirectURL:  o.cfg.RedirectURL,
		Scopes:       o.cfg.Scopes,
	}
	o.verifier = p.Verifier(&oidc.Config{ClientID: o.cfg.ClientID})
	return o.oauth, o.verifier, nil
}

// Begin starts a sign-in: it returns the address of the provider and the flow to keep.
func (o *OIDC) Begin(ctx context.Context) (string, Flow, error) {
	ctx, cancel := o.ctx(ctx)
	defer cancel()
	conf, _, err := o.ready(ctx)
	if err != nil {
		return "", Flow{}, err
	}
	f := Flow{State: randomToken(), Nonce: randomToken(), Verifier: oauth2.GenerateVerifier()}
	return conf.AuthCodeURL(f.State, oidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), f, nil
}

type idClaims struct {
	Email string
	Name  string
}

// identityOf reads the e-mail address and the name out of the claims of an ID token the way
// the provider of the config puts them there. The second result is the reason of a refusal.
func identityOf(cfg config.OIDCConfig, all map[string]any) (idClaims, string) {
	var c idClaims
	for _, name := range cfg.EmailClaims() {
		v, _ := all[name].(string)
		v = strings.TrimSpace(v)
		// preferred_username is a login name at many providers, and an address at some.
		if v == "" || (name == "preferred_username" && !strings.Contains(v, "@")) {
			continue
		}
		c.Email = v
		break
	}
	if c.Email == "" {
		return idClaims{}, "no email"
	}
	if cfg.VerifiedEmailRequired() && !truthy(all["email_verified"]) {
		return idClaims{}, "email not verified"
	}
	c.Name, _ = all["name"].(string)
	return c, ""
}

// truthy reads a flag that most providers send as a boolean and some as the text "true".
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	}
	return false
}

// OIDCResult is the outcome of a sign-in that worked.
type OIDCResult struct {
	Session Session
	User    model.User
	Created bool // the user was made by this sign-in
}

// Complete finishes a sign-in: it checks the state against the flow, exchanges the code,
// checks the ID token and the e-mail, finds or creates the user, applies the role and opens
// a session. Every refusal is ErrOIDCDenied.
func (o *OIDC) Complete(ctx context.Context, f Flow, state, code, ip, userAgent string) (OIDCResult, error) {
	if f.State == "" || subtle.ConstantTimeCompare([]byte(state), []byte(f.State)) != 1 {
		return OIDCResult{}, denied("state")
	}
	if code == "" || len(code) > 2048 {
		return OIDCResult{}, denied("code")
	}
	ctx, cancel := o.ctx(ctx)
	defer cancel()
	conf, verifier, err := o.ready(ctx)
	if err != nil {
		return OIDCResult{}, err
	}
	tok, err := conf.Exchange(ctx, code, oauth2.VerifierOption(f.Verifier))
	if err != nil {
		return OIDCResult{}, denied("exchange")
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return OIDCResult{}, denied("no id token")
	}
	idt, err := verifier.Verify(ctx, raw)
	if err != nil {
		return OIDCResult{}, denied("id token")
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(f.Nonce)) != 1 {
		return OIDCResult{}, denied("nonce")
	}
	if idt.Subject == "" {
		return OIDCResult{}, denied("subject")
	}
	var all map[string]any
	if idt.Claims(&all) != nil {
		return OIDCResult{}, denied("claims")
	}
	c, reason := identityOf(o.cfg, all)
	if reason != "" {
		return OIDCResult{}, denied(reason)
	}
	role := o.roleFor(all)

	created := false
	user, err := o.repo.FindIdentityUser(ctx, idt.Issuer, idt.Subject)
	switch {
	case errors.Is(err, model.ErrNotFound):
		user, err = o.create(ctx, idt.Issuer, idt.Subject, c, role)
		if err != nil {
			return OIDCResult{}, err
		}
		created = true
	case err != nil:
		return OIDCResult{}, fmt.Errorf("find identity: %w", err)
	default:
		if user.Disabled {
			return OIDCResult{}, denied("disabled")
		}
		user, err = o.syncRole(ctx, user, role)
		if err != nil {
			return OIDCResult{}, err
		}
	}

	if err := o.repo.RecordLogin(ctx, user.ID, true, o.svc.cfg.MaxFailedLogins, o.svc.cfg.LockoutDuration.Std()); err != nil {
		return OIDCResult{}, fmt.Errorf("record login: %w", err)
	}
	sess, err := o.svc.openSession(ctx, user.ID, ip, userAgent, o.now())
	if err != nil {
		return OIDCResult{}, err
	}
	return OIDCResult{Session: sess, User: user, Created: created}, nil
}

// create makes the user of a first sign-in, only when the config allows it for this domain.
func (o *OIDC) create(ctx context.Context, issuer, subject string, c idClaims, role model.Role) (model.User, error) {
	if !o.cfg.AutoCreate {
		return model.User{}, denied("no account")
	}
	if !o.domainAllowed(c.Email) {
		return model.User{}, denied("domain")
	}
	display := truncate(strings.TrimSpace(c.Name), 64)
	if display == "" {
		display = truncate(c.Email, 64)
	}
	u, err := o.repo.CreateIdentityUser(ctx, model.UserRecord{
		User: model.User{Username: identityUsername(c.Email, issuer, subject), DisplayName: display, Role: role},
	}, issuer, subject, truncate(c.Email, 254))
	if errors.Is(err, model.ErrConflict) {
		return model.User{}, denied("name taken")
	}
	if err != nil {
		return model.User{}, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

// syncRole applies the role the provider names now. The last administrator keeps the role.
func (o *OIDC) syncRole(ctx context.Context, u model.User, role model.Role) (model.User, error) {
	if u.Role == role {
		return u, nil
	}
	got, err := o.repo.UpdateUser(ctx, u.ID, &role, nil)
	if errors.Is(err, model.ErrLastAdmin) {
		return u, nil
	}
	if err != nil {
		return model.User{}, fmt.Errorf("apply role: %w", err)
	}
	return got, nil
}

func (o *OIDC) domainAllowed(email string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := strings.ToLower(email[at+1:])
	for _, d := range o.cfg.AllowedEmailDomains {
		if strings.ToLower(d) == domain {
			return true
		}
	}
	return false
}

var rank = map[model.Role]int{model.RoleViewer: 1, model.RoleOperator: 2, model.RoleAdmin: 3}

// roleFor reads the role claim: the best role among the values the map knows, else the default.
func (o *OIDC) roleFor(claims map[string]any) model.Role {
	best := model.Role(o.cfg.DefaultRole)
	if o.cfg.RoleClaim == "" || len(o.cfg.RoleMap) == 0 {
		return best
	}
	values := claimStrings(claims, o.cfg.RoleClaim)
	found := false
	for _, v := range values {
		r, ok := o.cfg.RoleMap[v]
		if !ok {
			continue
		}
		role := model.Role(r)
		if !found || rank[role] > rank[best] {
			best, found = role, true
		}
	}
	return best
}

// claimStrings reads a claim that is a text or a list of texts. The name is tried as a whole
// first (a claim such as "https://example.com/roles" has dots), then as a path through nested
// objects: "realm_access.roles" is what Keycloak calls its realm roles.
func claimStrings(claims map[string]any, name string) []string {
	v, ok := claims[name]
	if !ok {
		var cur any = claims
		for _, part := range strings.Split(name, ".") {
			m, isMap := cur.(map[string]any)
			if !isMap {
				return nil
			}
			if cur, ok = m[part]; !ok {
				return nil
			}
		}
		v = cur
	}
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// identityUsername builds a name of the form the accounts use from the e-mail address, with
// a short hash of the identity so that two people with the same local part do not meet.
func identityUsername(email, issuer, subject string) string {
	local, _, _ := strings.Cut(strings.ToLower(email), "@")
	var b strings.Builder
	for _, r := range local {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('.')
		}
	}
	name := b.String()
	if len(name) > 22 {
		name = name[:22]
	}
	sum := sha256.Sum256([]byte(issuer + "\x00" + subject))
	return name + "-" + hex.EncodeToString(sum[:])[:8]
}
