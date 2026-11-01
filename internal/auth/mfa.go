package auth

import (
	"context"
	"errors"
	"fmt"
)

// ErrMFARequired is what Login returns when the password was right and the account has a
// second factor. The caller asks for it before a session is opened.
var ErrMFARequired = errors.New("a second factor is needed")

// MFAGate tells whether an account has a second factor that must be given at login.
type MFAGate interface {
	Required(ctx context.Context, userID int64) (bool, error)
}

// SetMFAGate turns the second factor on for accounts that have set one up. Without a gate
// Login never asks for one.
func (s *Service) SetMFAGate(g MFAGate) { s.mfa = g }

// OpenSession opens a session for a user whose second factor was just checked.
func (s *Service) OpenSession(ctx context.Context, userID int64, ip, userAgent string) (Session, error) {
	return s.openSession(ctx, userID, ip, userAgent, s.now())
}

func mfaKey(ip, username string) string { return "mfa\x00" + lockKey(ip, username) }

// SecondFactorLocked tells whether too many wrong codes came from this address for this
// account. It is counted apart from wrong passwords, so the two cannot hide each other.
func (s *Service) SecondFactorLocked(ip, username string) bool {
	return s.locks.locked(mfaKey(ip, username), s.now())
}

// SecondFactorFailed counts a wrong code.
func (s *Service) SecondFactorFailed(ip, username string) {
	s.locks.fail(mfaKey(ip, username), s.now())
}

// SecondFactorDone forgets the wrong codes after a right one.
func (s *Service) SecondFactorDone(ip, username string) { s.locks.reset(mfaKey(ip, username)) }

// HashRecovery hashes a recovery code the way a password is hashed.
func (s *Service) HashRecovery(code string) (string, error) {
	h, err := Hash(code, s.params)
	if err != nil {
		return "", fmt.Errorf("hash recovery code: %w", err)
	}
	return h, nil
}

// VerifyRecovery checks a recovery code against its hash.
func (s *Service) VerifyRecovery(code, hash string) (bool, error) { return Verify(code, hash) }
