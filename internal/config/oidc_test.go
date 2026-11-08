package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func oidcValid() Config {
	c := valid()
	c.Auth.OIDC = OIDCConfig{
		Issuer: "https://idp.example.com", ClientID: "id", ClientSecret: "secret",
		RedirectURL: "https://netscribe.example.com/api/auth/oidc/callback",
		Scopes:      []string{"openid", "email"}, DefaultRole: "viewer",
	}
	return c
}

func TestOIDCOffByDefaultAndNotChecked(t *testing.T) {
	c := valid()
	if c.Auth.OIDC.Enabled() {
		t.Fatal("single sign-on is on in the defaults")
	}
	c.Auth.OIDC.ClientID = "left over with no issuer"
	if err := c.Validate(); err != nil {
		t.Fatalf("a block with no issuer is off and must not be checked: %v", err)
	}
	if got := c.Auth.OIDC.DefaultRole; got != "viewer" {
		t.Fatalf("default role = %q, want viewer", got)
	}
}

func TestOIDCValidate(t *testing.T) {
	tests := []struct {
		name string
		edit func(*OIDCConfig)
		want string // part of the message, empty means valid
	}{
		{"a full block", func(*OIDCConfig) {}, ""},
		{"issuer over http", func(o *OIDCConfig) { o.Issuer = "http://idp.example.com" }, "auth.oidc.issuer"},
		{"issuer over http on loopback", func(o *OIDCConfig) { o.Issuer = "http://127.0.0.1:9000" }, ""},
		{"issuer over http on localhost", func(o *OIDCConfig) { o.Issuer = "http://localhost:9000" }, ""},
		{"issuer is not an address", func(o *OIDCConfig) { o.Issuer = "idp.example.com" }, "auth.oidc.issuer"},
		{"issuer holds a password", func(o *OIDCConfig) { o.Issuer = "https://u:p@idp.example.com" }, "auth.oidc.issuer"},
		{"redirect over http", func(o *OIDCConfig) { o.RedirectURL = "http://netscribe.example.com/cb" }, "auth.oidc.redirectUrl"},
		{"redirect missing", func(o *OIDCConfig) { o.RedirectURL = "" }, "auth.oidc.redirectUrl"},
		{"no client id", func(o *OIDCConfig) { o.ClientID = "" }, "auth.oidc.clientId"},
		{"no client secret", func(o *OIDCConfig) { o.ClientSecret = "" }, "auth.oidc.clientSecret"},
		{"default role admin", func(o *OIDCConfig) { o.DefaultRole = "admin" }, "must not be admin"},
		{"default role unknown", func(o *OIDCConfig) { o.DefaultRole = "root" }, "auth.oidc.defaultRole"},
		{"default role operator", func(o *OIDCConfig) { o.DefaultRole = "operator" }, ""},
		{"a role map may give admin", func(o *OIDCConfig) {
			o.RoleClaim, o.RoleMap = "groups", map[string]string{"ops": "admin"}
		}, ""},
		{"a role map with an unknown role", func(o *OIDCConfig) {
			o.RoleClaim, o.RoleMap = "groups", map[string]string{"ops": "root"}
		}, "auth.oidc.roleMap"},
		{"a role map with no claim", func(o *OIDCConfig) { o.RoleMap = map[string]string{"ops": "viewer"} }, "auth.oidc.roleClaim"},
		{"a domain with an at sign", func(o *OIDCConfig) { o.AllowedEmailDomains = []string{"@example.com"} }, "auth.oidc.allowedEmailDomains"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := oidcValid()
			tt.edit(&c.Auth.OIDC)
			err := c.Validate()
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("Validate() = %v, want nil", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("Validate() = %v, want a message with %q", err, tt.want)
			}
		})
	}
}

func TestOIDCClientSecretStaysOutOfEveryText(t *testing.T) {
	c := oidcValid()
	c.Auth.OIDC.ClientSecret = "very-secret-value"
	for _, text := range []string{c.Auth.OIDC.ClientSecret.String(), mustJSON(t, c.Auth.OIDC)} {
		if strings.Contains(text, "very-secret-value") {
			t.Fatalf("the client secret leaked into %q", text)
		}
	}
}

func TestOIDCButtonName(t *testing.T) {
	if got := (OIDCConfig{}).ButtonName(); got != "SSO" {
		t.Fatalf("default name = %q", got)
	}
	if got := (OIDCConfig{Name: "Acme"}).ButtonName(); got != "Acme" {
		t.Fatalf("name = %q", got)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
