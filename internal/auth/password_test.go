package auth

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// fast is the lowest cost the configuration accepts (section 9.2), to keep tests quick.
var fast = Params{MemoryKiB: 19456, Iterations: 2, Parallelism: 1}

func TestHashIsPHCArgon2id(t *testing.T) {
	h, err := Hash("correct horse battery", Params{MemoryKiB: 65536, Iterations: 3, Parallelism: 2})
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^\$argon2id\$v=19\$m=65536,t=3,p=2\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)
	if !re.MatchString(h) {
		t.Errorf("hash %q is not PHC argon2id with 64 MiB, 3 passes, parallelism 2, a 16 byte salt and a 32 byte key", h)
	}
}

func TestVerify(t *testing.T) {
	h, err := Hash("correct horse battery", fast)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		pw   string
		want bool
	}{
		{"correct horse battery", true},
		{"correct horse batter", false},
		{"Correct horse battery", false},
		{"", false},
		{"correct horse battery ", false},
	} {
		got, err := Verify(tt.pw, h)
		if err != nil || got != tt.want {
			t.Errorf("Verify(%q) = %v, %v; want %v", tt.pw, got, err, tt.want)
		}
	}
}

func TestHashesOfOnePasswordDiffer(t *testing.T) {
	a, _ := Hash("same password here", fast)
	b, _ := Hash("same password here", fast)
	if a == b {
		t.Error("two hashes of one password are identical: the salt is not random")
	}
}

func TestVerifyUnicodePassword(t *testing.T) {
	pw := "пароль-с-юникодом-🔐"
	h, _ := Hash(pw, fast)
	if ok, err := Verify(pw, h); err != nil || !ok {
		t.Errorf("Verify(unicode) = %v, %v", ok, err)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	good, _ := Hash("some long password", fast)
	parts := strings.Split(good, "$") // "", argon2id, v=19, params, salt, key
	join := func(p ...string) string { return strings.Join(p, "$") }
	tests := map[string]string{
		"empty":             "",
		"not a hash":        "plaintext",
		"wrong algorithm":   join(parts[0], "argon2i", parts[2], parts[3], parts[4], parts[5]),
		"wrong version":     join(parts[0], parts[1], "v=16", parts[3], parts[4], parts[5]),
		"missing key":       join(parts[0], parts[1], parts[2], parts[3], parts[4]),
		"extra field":       good + "$x",
		"bad params":        join(parts[0], parts[1], parts[2], "m=19456,t=2", parts[4], parts[5]),
		"unknown parameter": join(parts[0], parts[1], parts[2], "m=19456,t=2,p=1,x=1", parts[4], parts[5]),
		"zero memory":       join(parts[0], parts[1], parts[2], "m=0,t=2,p=1", parts[4], parts[5]),
		"huge memory":       join(parts[0], parts[1], parts[2], "m=4194304,t=2,p=1", parts[4], parts[5]),
		"huge iterations":   join(parts[0], parts[1], parts[2], "m=19456,t=1000,p=1", parts[4], parts[5]),
		"huge parallelism":  join(parts[0], parts[1], parts[2], "m=19456,t=2,p=200", parts[4], parts[5]),
		"salt not base64":   join(parts[0], parts[1], parts[2], parts[3], "!!!", parts[5]),
		"salt too short":    join(parts[0], parts[1], parts[2], parts[3], "AAAA", parts[5]),
		"key too short":     join(parts[0], parts[1], parts[2], parts[3], parts[4], "AAAA"),
	}
	for name, h := range tests {
		t.Run(name, func(t *testing.T) {
			if ok, err := Verify("some long password", h); err == nil || ok {
				t.Errorf("Verify() = %v, %v; want an error", ok, err)
			}
		})
	}
}

func TestNeedsRehash(t *testing.T) {
	want := Params{MemoryKiB: 65536, Iterations: 3, Parallelism: 2}
	weak, _ := Hash("some long password", fast)
	strong, _ := Hash("some long password", want)
	moreIter, _ := Hash("some long password", Params{MemoryKiB: 65536, Iterations: 4, Parallelism: 2})

	for name, tt := range map[string]struct {
		hash string
		want bool
	}{
		"weaker parameters":   {weak, true},
		"equal parameters":    {strong, false},
		"stronger parameters": {moreIter, false},
		"unparsable hash":     {"garbage", true},
	} {
		if got := NeedsRehash(tt.hash, want); got != tt.want {
			t.Errorf("%s: NeedsRehash = %v, want %v", name, got, tt.want)
		}
	}
	// Each parameter alone is enough to ask for a rehash.
	for name, p := range map[string]Params{
		"memory":      {MemoryKiB: 65536, Iterations: 3, Parallelism: 2},
		"iterations":  {MemoryKiB: 19456, Iterations: 3, Parallelism: 2},
		"parallelism": {MemoryKiB: 19456, Iterations: 2, Parallelism: 2},
	} {
		if !NeedsRehash(weak, p) {
			t.Errorf("a weaker %s did not ask for a rehash", name)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name, pw, user string
		min            int
		want           error
	}{
		{"12 characters", "abcdefghijkl", "bob", 12, nil},
		{"11 characters", "abcdefghijk", "bob", 12, ErrPasswordShort},
		{"empty", "", "bob", 12, ErrPasswordShort},
		{"128 characters", strings.Repeat("a", 128), "bob", 12, nil},
		{"129 characters", strings.Repeat("a", 129), "bob", 12, ErrPasswordLong},
		{"12 Cyrillic characters are 24 bytes", "парольпарол1", "bob", 12, nil},
		{"11 Cyrillic characters are not enough", "парольпарол", "bob", 12, ErrPasswordShort},
		{"128 Cyrillic characters", strings.Repeat("я", 128), "bob", 12, nil},
		{"129 Cyrillic characters", strings.Repeat("я", 129), "bob", 12, ErrPasswordLong},
		{"emoji count as one character", strings.Repeat("🔐", 12), "bob", 12, nil},
		{"configured minimum is honored", "abcdefghijkl", "bob", 14, ErrPasswordShort},
		{"no composition rules", "aaaaaaaaaaaa", "bob", 12, nil},
		{"equals the username", "administrator", "administrator", 12, ErrPasswordIsUsername},
		{"equals the username in another case", "ADMINISTRATOR", "administrator", 12, ErrPasswordIsUsername},
		{"contains the username", "administrator1", "administrator", 12, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidatePassword(tt.pw, tt.user, tt.min); !errors.Is(err, tt.want) {
				t.Errorf("ValidatePassword() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	for name, ok := range map[string]bool{
		"bob": true, "alice.smith": true, "a-b_c.d": true, "user123": true,
		strings.Repeat("a", 32): true,
		"ab":                    false, // too short
		strings.Repeat("a", 33): false,
		"":                      false,
		"Alice":                 false, // callers normalize first
		"al ice":                false,
		"alice!":                false,
		"алиса":                 false,
		"a/b/c":                 false,
		"bob\n":                 false,
	} {
		err := ValidateUsername(name)
		if (err == nil) != ok {
			t.Errorf("ValidateUsername(%q) = %v, want ok=%v", name, err, ok)
		}
		if err != nil && err.Error() != "username is invalid" {
			t.Errorf("message = %q", err)
		}
	}
	if NormalizeUsername("AlIcE") != "alice" {
		t.Error("NormalizeUsername does not lower-case")
	}
}

func TestErrorMessagesMatchTheSpecification(t *testing.T) {
	for err, want := range map[error]string{
		ErrPasswordShort:      "password is shorter than the minimum",
		ErrPasswordLong:       "password is longer than 128 characters",
		ErrPasswordIsUsername: "password equals username",
		ErrUsernameInvalid:    "username is invalid",
		ErrRoleUnknown:        "role is unknown",
		ErrInvalidCredentials: "invalid username or password",
	} {
		if err.Error() != want {
			t.Errorf("message %q, want %q", err, want)
		}
	}
}
