// Package authtest is an identity provider that runs inside a test: the discovery document,
// the keys and a token endpoint that hands out the ID tokens a test prepared. It is for tests
// of single sign-on only and is never imported by the service.
package authtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// IDP is a running provider. URL is its issuer.
type IDP struct {
	URL string
	// Key signs the tokens of Claims unless a test passes another one to Register.
	Key *rsa.PrivateKey

	t     testing.TB
	srv   *httptest.Server
	mu    sync.Mutex
	codes map[string]code
}

type code struct {
	challenge string
	claims    map[string]any
	key       *rsa.PrivateKey
}

// New starts a provider and stops it when the test ends.
func New(t testing.TB) *IDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &IDP{t: t, Key: key, codes: map[string]code{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.URL,
			"authorization_endpoint":                p.URL + "/authorize",
			"token_endpoint":                        p.URL + "/token",
			"jwks_uri":                              p.URL + "/keys",
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
	p.URL = p.srv.URL
	t.Cleanup(p.srv.Close)
	return p
}

// Close stops the provider before the test ends.
func (p *IDP) Close() { p.srv.Close() }

// Register makes code worth one token. challenge is the code_challenge the service sent to
// /authorize: the token endpoint checks the code_verifier against it. A nil key means Key.
func (p *IDP) Register(codeValue, challenge string, claims map[string]any, key *rsa.PrivateKey) {
	if key == nil {
		key = p.Key
	}
	p.mu.Lock()
	p.codes[codeValue] = code{challenge: challenge, claims: claims, key: key}
	p.mu.Unlock()
}

// Claims are the claims of a good ID token of this provider for a client.
func (p *IDP) Claims(nonce, subject, email, clientID string) map[string]any {
	return map[string]any{
		"iss": p.URL, "sub": subject, "aud": clientID,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"nonce": nonce, "email": email, "email_verified": true, "name": "Test Person",
	}
}

func (p *IDP) token(w http.ResponseWriter, r *http.Request) {
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
		"id_token": p.sign(c.key, c.claims),
	})
}

func (p *IDP) sign(key *rsa.PrivateKey, claims map[string]any) string {
	p.t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			p.t.Error(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		p.t.Error(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}
