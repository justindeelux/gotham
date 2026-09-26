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

// Argon2id parameters. These follow the RFC 9106 second recommended option
// (time=3, memory=64 MiB, threads=4). The hash is stored using the standard
// PHC encoded string form:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<b64salt>$<b64hash>
const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 4
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// errInvalidHash marks an encoded hash that cannot be parsed.
var errInvalidHash = errors.New("auth: invalid password hash")

// HashPassword derives an argon2id hash of password and returns its encoded
// string form. An empty password is rejected. Length bounds are enforced by the
// service layer so callers can surface a validation error before hashing.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", fmt.Errorf("%w: password must not be empty", ErrValidation)
	}

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonTime,
		argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the encoded argon2id hash.
// The parameters are read from the encoded string, so hashes produced with
// older settings keep verifying after the defaults change.
func VerifyPassword(encoded, password string) (bool, error) {
	params, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}

	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(want, got) == 1, nil
}

// argonParams holds the cost parameters parsed from an encoded hash.
type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
}

// decodeHash parses the PHC encoded form into its cost parameters, salt, and
// hash bytes.
func decodeHash(encoded string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return argonParams{}, nil, nil, errInvalidHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return argonParams{}, nil, nil, errInvalidHash
	}
	if version != argon2.Version {
		return argonParams{}, nil, nil, fmt.Errorf("%w: unsupported version %d", errInvalidHash, version)
	}

	var params argonParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memory, &params.time, &params.threads); err != nil {
		return argonParams{}, nil, nil, errInvalidHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return argonParams{}, nil, nil, errInvalidHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return argonParams{}, nil, nil, errInvalidHash
	}
	if len(salt) == 0 || len(key) == 0 {
		return argonParams{}, nil, nil, errInvalidHash
	}

	return params, salt, key, nil
}
