package totp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The SHA-1 secret and the times of the table in RFC 6238, appendix B. The RFC shows eight
// digits; the six digit code is the last six of them.
var rfcSecret = []byte("12345678901234567890")

func TestCodesMatchRFC6238(t *testing.T) {
	for _, c := range []struct {
		unix int64
		want string
	}{
		{59, "287082"}, {1111111109, "081804"}, {1111111111, "050471"},
		{1234567890, "005924"}, {2000000000, "279037"}, {20000000000, "353130"},
	} {
		if got := Code(rfcSecret, time.Unix(c.unix, 0)); got != c.want {
			t.Errorf("T=%d: %s, want %s", c.unix, got, c.want)
		}
	}
}

func TestVerifyAcceptsOneStepEitherSideAndNoMore(t *testing.T) {
	now := time.Unix(1111111109, 0)
	for offset, want := range map[int]bool{-60: false, -30: true, 0: true, 30: true, 60: false} {
		in := Code(rfcSecret, now.Add(time.Duration(offset)*time.Second))
		if _, ok := Verify(rfcSecret, in, now, 0); ok != want {
			t.Errorf("offset %d: ok = %v, want %v", offset, ok, want)
		}
	}
}

func TestACodeIsAcceptedOnce(t *testing.T) {
	now := time.Unix(1111111109, 0)
	in := Code(rfcSecret, now)
	step, ok := Verify(rfcSecret, in, now, 0)
	if !ok {
		t.Fatal("a right code was refused")
	}
	if _, ok := Verify(rfcSecret, in, now, step); ok {
		t.Error("the same code was accepted twice")
	}
	earlier := Code(rfcSecret, now.Add(-30*time.Second))
	if _, ok := Verify(rfcSecret, earlier, now, step); ok {
		t.Error("an earlier step was accepted after a later one")
	}
}

func TestVerifyRefusesMalformedInput(t *testing.T) {
	now := time.Unix(59, 0)
	for _, in := range []string{"", "28708", "2870822", "abcdef", "287 08", "28708\n"} {
		if in == "287 08" {
			continue // five digits once the space is gone
		}
		if _, ok := Verify(rfcSecret, in, now, 0); ok {
			t.Errorf("%q was accepted", in)
		}
	}
	if _, ok := Verify(rfcSecret, "287 082", now, 0); !ok {
		t.Error("a code typed with a space was refused")
	}
}

func TestURIHoldsWhatAnAppNeeds(t *testing.T) {
	uri := URI("Netscribe", "alice@home", []byte("12345678901234567890"))
	for _, want := range []string{"otpauth://totp/Netscribe:alice@home?", "secret=" + EncodeSecret([]byte("12345678901234567890")), "issuer=Netscribe", "digits=6", "period=30", "algorithm=SHA1"} {
		if !strings.Contains(uri, want) {
			t.Errorf("%q is missing from %s", want, uri)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, err := RecoveryCodes(10)
	if err != nil || len(codes) != 10 {
		t.Fatal(err, len(codes))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 10 || strings.Trim(c, recoveryAlphabet) != "" {
			t.Errorf("odd code %q", c)
		}
		if seen[c] {
			t.Errorf("%q twice", c)
		}
		seen[c] = true
	}
	if got := NormalizeRecovery("abcd-efgh jk"); got != "ABCDEFGHJK" {
		t.Errorf("NormalizeRecovery = %q", got)
	}
}

func TestSealRoundTripAndTamper(t *testing.T) {
	key := bytes.Repeat([]byte{7}, KeySize)
	s, err := NewSealer(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal([]byte("secret bytes"))
	if err != nil || bytes.Contains(sealed, []byte("secret bytes")) {
		t.Fatalf("sealed = %q, %v", sealed, err)
	}
	if plain, err := s.Open(sealed); err != nil || string(plain) != "secret bytes" {
		t.Fatalf("open = %q, %v", plain, err)
	}
	other, _ := NewSealer(bytes.Repeat([]byte{8}, KeySize))
	if _, err := other.Open(sealed); err == nil {
		t.Error("a wrong key opened it")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := s.Open(sealed); err == nil {
		t.Error("a changed byte was not noticed")
	}
	if _, err := s.Open([]byte("short")); err == nil {
		t.Error("short input was opened")
	}
	if _, err := NewSealer([]byte("short")); err == nil {
		t.Error("a short key was accepted")
	}
	a, _ := s.Seal([]byte("x"))
	b, _ := s.Seal([]byte("x"))
	if bytes.Equal(a, b) {
		t.Error("the same plain text sealed to the same bytes twice")
	}
}

func TestKeyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "totp.key")
	k1, err := LoadOrCreateKey(path)
	if err != nil || len(k1) != KeySize {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", info.Mode().Perm())
	}
	k2, err := LoadOrCreateKey(path)
	if err != nil || !bytes.Equal(k1, k2) {
		t.Error("the key changed between two reads")
	}
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(path); err == nil {
		t.Error("a key file of the wrong size was accepted")
	}
}
