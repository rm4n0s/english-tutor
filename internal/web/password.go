package web

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// MinPasswordRunes is the shortest password the web server accepts.
const MinPasswordRunes = 8

// maxPasswordBytes bounds what a login form is allowed to hash.
const maxPasswordBytes = 1024

// pbkdf2Rounds is the work factor of the password hash. About a third of a
// second on a current CPU, which also slows down guessing.
const pbkdf2Rounds = 600_000

const hashPrefix = "pbkdf2-sha256"

// HashPassword returns the string that is stored in place of a password:
// "pbkdf2-sha256$<rounds>$<salt>$<hash>".
//
// Error: WebPasswordInvalid (too short or too long).
func HashPassword(password string) (string, error) {
	if utf8.RuneCountInString(password) < MinPasswordRunes {
		return "", errors.New("WebPasswordInvalid",
			"The password must have at least 8 characters.", "length", utf8.RuneCountInString(password))
	}
	if len(password) > maxPasswordBytes {
		return "", errors.New("WebPasswordInvalid", "The password is too long.", "length", len(password))
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		// crypto/rand.Read never returns an error; a salt must not be guessable.
		panic(err)
	}
	sum, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Rounds, 32)
	if err != nil {
		return "", errors.NewErr("WebPasswordInvalid", err).SetMessage("The password could not be hashed.")
	}
	return strings.Join([]string{
		hashPrefix,
		strconv.Itoa(pbkdf2Rounds),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum),
	}, "$"), nil
}

// CheckPassword reports whether password is the one that produced hash. A hash
// that cannot be read matches nothing.
func CheckPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 4 || parts[0] != hashPrefix || len(password) > maxPasswordBytes {
		return false
	}
	rounds, err := strconv.Atoi(parts[1])
	if err != nil || rounds < 1 || rounds > 10_000_000 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, rounds, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
