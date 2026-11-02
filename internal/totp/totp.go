// Package totp implements time based one time passwords (RFC 6238) the way authenticator
// apps expect them: HMAC-SHA1, 6 digits, a 30 second step. It also seals a secret so a
// copy of the database alone does not hold it. It uses only the standard library.
package totp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 and every authenticator app use HMAC-SHA1
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// Step is the length of one code's life, in seconds.
	Step = 30
	// Digits is the length of a code.
	Digits = 6
	// SecretBytes is the size of a new secret.
	SecretBytes = 20
	// drift is how many steps either side of now a code is still accepted.
	drift = 1
)

// NewSecret returns a random secret.
func NewSecret() ([]byte, error) {
	b := make([]byte, SecretBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("make a secret: %w", err)
	}
	return b, nil
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// EncodeSecret is the secret as an authenticator app asks for it by hand.
func EncodeSecret(secret []byte) string { return b32.EncodeToString(secret) }

// URI is the otpauth address an authenticator app reads from a QR code.
func URI(issuer, account string, secret []byte) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	q := url.Values{}
	q.Set("secret", EncodeSecret(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(Digits))
	q.Set("period", fmt.Sprint(Step))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// stepOf is the number of the step a time falls in.
func stepOf(t time.Time) int64 { return t.Unix() / Step }

// code is the code of one step.
func code(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // a step is never negative for a real clock
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[off])&0x7f)<<24 | uint32(sum[off+1])<<16 | uint32(sum[off+2])<<8 | uint32(sum[off+3])
	var mod uint32 = 1
	for i := 0; i < Digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", Digits, bin%mod)
}

// Code is the code that is right at the given time. It is for tests and for tools.
func Code(secret []byte, at time.Time) string { return code(secret, stepOf(at)) }

// Verify checks a code against the steps around now. It returns the step the code belongs
// to, which the caller stores: a code is accepted once, so a step at or below lastStep is
// refused. Spaces in the code are ignored.
func Verify(secret []byte, input string, now time.Time, lastStep int64) (int64, bool) {
	input = strings.ReplaceAll(input, " ", "")
	if len(input) != Digits {
		return 0, false
	}
	var found int64
	ok := false
	for s := stepOf(now) - drift; s <= stepOf(now)+drift; s++ {
		// every step is compared, so the time taken does not say which one matched
		if subtle.ConstantTimeCompare([]byte(input), []byte(code(secret, s))) == 1 && s > lastStep && !ok {
			found, ok = s, true
		}
	}
	return found, ok
}

// recoveryAlphabet has no letters that look like digits, 32 characters in all.
const recoveryAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789_"

// RecoveryCodes makes n codes of 10 characters each.
func RecoveryCodes(n int) ([]string, error) {
	out := make([]string, n)
	for i := range out {
		raw := make([]byte, 10)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("make recovery codes: %w", err)
		}
		for j, b := range raw {
			raw[j] = recoveryAlphabet[int(b)%len(recoveryAlphabet)] // 256 is a multiple of 32: no bias
		}
		out[i] = string(raw)
	}
	return out, nil
}

// NormalizeRecovery puts what a person typed in the form the codes are stored in.
func NormalizeRecovery(s string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(s))
}

// A Sealer encrypts secrets with AES-256-GCM under a key kept in a file.
type Sealer struct{ aead cipher.AEAD }

// KeySize is the size of the key file.
const KeySize = 32

// NewSealer builds a sealer from a key of KeySize bytes.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, errors.New("the key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("start the cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("start the cipher: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// LoadOrCreateKey reads the key file, or makes it (mode 0600) when it does not exist.
func LoadOrCreateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the path is the install's own key file
	if err == nil {
		if len(data) != KeySize {
			return nil, fmt.Errorf("the key file %s has the wrong size", path)
		}
		return data, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read the key file: %w", err)
	}
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("make a key: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // the install's own key file
	if err != nil {
		return nil, fmt.Errorf("write the key file: %w", err)
	}
	if _, err := f.Write(key); err != nil {
		_ = f.Close() // the write error is the one worth reporting
		_ = os.Remove(path)
		return nil, fmt.Errorf("write the key file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("write the key file: %w", err)
	}
	return key, nil
}

// Seal encrypts a secret. The result holds a fresh nonce followed by the ciphertext.
func (s *Sealer) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plain, nil), nil
}

// Open decrypts what Seal made. A wrong key or a changed byte is an error.
func (s *Sealer) Open(sealed []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n+s.aead.Overhead() {
		return nil, errors.New("the sealed secret is too short")
	}
	plain, err := s.aead.Open(nil, sealed[:n], sealed[n:], nil)
	if err != nil {
		return nil, errors.New("the sealed secret cannot be opened: the key is wrong or the data changed")
	}
	return plain, nil
}
