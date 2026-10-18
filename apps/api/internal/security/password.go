package security

import "golang.org/x/crypto/bcrypt"

// BcryptCost is deliberately above the library minimum: operator sign-in is
// interactive and rare, so the extra work is invisible but raises the cost of
// an offline guess against a leaked hash.
const BcryptCost = 12

// Bcrypt hashes an operator password.
func Bcrypt(plain string) (string, error) {
	sum, err := bcrypt.GenerateFromPassword([]byte(plain), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(sum), nil
}

// BcryptCompare reports whether plain matches the stored hash. A malformed hash
// is a failed comparison, never a panic.
func BcryptCompare(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
