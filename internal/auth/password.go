package auth

import "golang.org/x/crypto/bcrypt"

// Bcrypt cost 12: ~0.25s per hash on modern hardware. Slows brute force without
// making login feel laggy. DefaultCost (10) is too cheap today.
const bcryptCost = 12

func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// DummyHashCompare runs bcrypt against a pre-baked hash so login equalises
// timing whether or not the username exists — prevents user enumeration via
// response time. The specific password the dummy hash encodes doesn't matter;
// we only use it to consume the same CPU time as a real VerifyPassword call.
var dummyHash = []byte("$2a$12$C6UzMDM.H6dfI/f/IKxGhuxX0PuUFWfK7JjhWZ4YEJpK/9sVF0tJO")

func DummyCompare() {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte("x"))
}
