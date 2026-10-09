// Package auth holds passwords, roles, sessions and login rate limits.
// It reaches the database only through the Repo interface and imports neither store nor api.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	saltLen          = 16
	keyLen           = 32
	maxPasswordRunes = 128

	// Limits for parameters read back from a stored hash, so a corrupted row
	// cannot make a login allocate gigabytes.
	maxHashMemoryKiB   = 1 << 20 // 1 GiB
	maxHashIterations  = 16
	maxHashParallelism = 32
)

// Messages follow section 8 of the specification.
var (
	ErrUsernameInvalid    = errors.New("username is invalid")
	ErrPasswordShort      = errors.New("password is shorter than the minimum")
	ErrPasswordLong       = errors.New("password is longer than 128 characters")
	ErrPasswordIsUsername = errors.New("password equals username")
	errMalformedHash      = errors.New("malformed password hash")
)

var usernamePattern = regexp.MustCompile(`^[a-z0-9._-]{3,32}$`)

// Params are the argon2id cost parameters.
type Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// NormalizeUsername lower-cases a name: usernames are case-insensitive.
func NormalizeUsername(s string) string { return strings.ToLower(s) }

// ValidateUsername checks a normalized name against ^[a-z0-9._-]{3,32}$.
func ValidateUsername(name string) error {
	if !usernamePattern.MatchString(name) {
		return ErrUsernameInvalid
	}
	return nil
}

// ValidatePassword checks the length (in characters, not bytes) and that the
// password differs from the username. There are no composition rules.
func ValidatePassword(password, username string, minLen int) error {
	n := utf8.RuneCountInString(password)
	switch {
	case n < minLen:
		return ErrPasswordShort
	case n > maxPasswordRunes:
		return ErrPasswordLong
	case strings.EqualFold(password, username):
		return ErrPasswordIsUsername
	}
	return nil
}

// Hash returns the argon2id hash of password in PHC string format.
func Hash(password string, p Params) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, keyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

type parsedHash struct {
	params Params
	salt   []byte
	key    []byte
}

func parseHash(encoded string) (parsedHash, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return parsedHash{}, errMalformedHash
	}
	if parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return parsedHash{}, errMalformedHash
	}
	var m, t, p uint64
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return parsedHash{}, errMalformedHash
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return parsedHash{}, errMalformedHash
		}
		switch k {
		case "m":
			m = n
		case "t":
			t = n
		case "p":
			p = n
		default:
			return parsedHash{}, errMalformedHash
		}
	}
	if m == 0 || m > maxHashMemoryKiB || t == 0 || t > maxHashIterations || p == 0 || p > maxHashParallelism {
		return parsedHash{}, errMalformedHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return parsedHash{}, errMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return parsedHash{}, errMalformedHash
	}
	return parsedHash{
		params: Params{MemoryKiB: uint32(m), Iterations: uint32(t), Parallelism: uint8(p)},
		salt:   salt,
		key:    key,
	}, nil
}

// Verify reports whether password matches encoded. A hash that cannot be parsed is an error.
func Verify(password, encoded string) (bool, error) {
	h, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), h.salt, h.params.Iterations, h.params.MemoryKiB, h.params.Parallelism, uint32(len(h.key)))
	return subtle.ConstantTimeCompare(got, h.key) == 1, nil
}

// NeedsRehash reports whether encoded was made with weaker parameters than p.
func NeedsRehash(encoded string, p Params) bool {
	h, err := parseHash(encoded)
	if err != nil {
		return true
	}
	return h.params.MemoryKiB < p.MemoryKiB || h.params.Iterations < p.Iterations || h.params.Parallelism < p.Parallelism
}
