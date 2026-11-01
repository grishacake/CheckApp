package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory     = 19 * 1024
	passwordIterations = 2
	passwordThreads    = 1
	passwordSaltLength = 16
	passwordKeyLength  = 32
)

var ErrInvalidPasswordHash = errors.New("invalid or unsupported password hash")

func HashPassword(password string) (string, error) {
	salt := make([]byte, passwordSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemory, passwordThreads, passwordKeyLength)
	return passwordHashPrefix() + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func CheckPassword(password, encodedHash string) (bool, error) {
	prefix := passwordHashPrefix()
	saltLength := base64.RawStdEncoding.EncodedLen(passwordSaltLength)
	keyLength := base64.RawStdEncoding.EncodedLen(passwordKeyLength)
	if len(encodedHash) != len(prefix)+saltLength+1+keyLength {
		return false, ErrInvalidPasswordHash
	}
	payload, ok := strings.CutPrefix(encodedHash, prefix)
	if !ok {
		return false, ErrInvalidPasswordHash
	}
	saltText, keyText, ok := strings.Cut(payload, "$")
	if !ok || len(saltText) != saltLength || len(keyText) != keyLength {
		return false, ErrInvalidPasswordHash
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(saltText)
	if err != nil || len(salt) != passwordSaltLength {
		return false, ErrInvalidPasswordHash
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(keyText)
	if err != nil || len(expected) != passwordKeyLength {
		return false, ErrInvalidPasswordHash
	}
	actual := argon2.IDKey([]byte(password), salt, passwordIterations, passwordMemory, passwordThreads, passwordKeyLength)
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func passwordHashPrefix() string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$", argon2.Version, passwordMemory, passwordIterations, passwordThreads)
}
