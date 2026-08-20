// Package password provides password hashing and verification.
//
// Passwords are hashed with bcrypt, which embeds a per-password salt and a
// cost factor in the resulting hash, so no separate salt column is needed.
package password

import (
	"os"
	"strconv"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

// MaxLength is the maximum password length bcrypt accepts. Anything longer is
// silently truncated by the algorithm, so it is rejected explicitly instead.
const MaxLength = 72

// ErrPasswordTooLong is returned when a password exceeds MaxLength bytes.
var ErrPasswordTooLong = errors.Errorf("password must be at most %d bytes", MaxLength)

// cost is the bcrypt cost factor. It defaults to bcrypt.DefaultCost (10, about
// 50-100ms per hash on current hardware). Load tests can lower it via
// GOCHAT_BCRYPT_COST so that the login path measures the service instead of the
// KDF; never lower it in production.
var cost = resolveCost()

func resolveCost() int {
	raw := os.Getenv("GOCHAT_BCRYPT_COST")
	if raw == "" {
		return bcrypt.DefaultCost
	}
	c, err := strconv.Atoi(raw)
	if err != nil || c < bcrypt.MinCost || c > bcrypt.MaxCost {
		logrus.Warnf("invalid GOCHAT_BCRYPT_COST %q, falling back to default", raw)
		return bcrypt.DefaultCost
	}
	if c < bcrypt.DefaultCost {
		logrus.Warnf("bcrypt cost %d is below the default %d, this is only safe for load testing", c, bcrypt.DefaultCost)
	}
	return c
}

// Cost returns the bcrypt cost factor currently in use.
func Cost() int {
	return cost
}

// dummyHash is a bcrypt hash, at the configured cost, of a value nobody can log
// in with. It is compared against when the user does not exist so that a login
// attempt for an unknown user costs the same as one for a known user and cannot
// be told apart by response time.
var dummyHash = mustDummyHash()

func mustDummyHash() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("gochat-nonexistent-user"), cost)
	if err != nil {
		logrus.Errorf("failed to build dummy password hash: %v", err)
		return nil
	}
	return h
}

// Hash returns the bcrypt hash of a plaintext password.
func Hash(plain string) (string, error) {
	if len(plain) > MaxLength {
		return "", ErrPasswordTooLong
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", errors.Wrap(err, "hash password")
	}
	return string(h), nil
}

// Verify reports whether plain matches the stored bcrypt hash.
func Verify(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// VerifyDummy performs a throwaway comparison with the same cost as a real one.
// Call it on the "user not found" branch so that both branches take comparable
// time and the response cannot be used to enumerate accounts.
func VerifyDummy(plain string) {
	if dummyHash == nil {
		return
	}
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(plain))
}
