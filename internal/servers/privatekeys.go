package servers

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/ssh"

	"github.com/justindeelux/gotham/internal/store"
	"github.com/justindeelux/gotham/internal/store/sqlc"
)

const (
	PrivateKeyTypeEd25519 = "ed25519"
	PrivateKeyTypeRSA     = "rsa"
	PrivateKeyTypeECDSA   = "ecdsa"

	defaultRSABits = 4096
	minRSABits     = 2048
	maxRSABits     = 8192
)

var errPrivateKeyPassphrase = errors.New("private key is passphrase protected")

// PrivateKeyUpdateParams carries the optional fields of a PATCH; nil means
// "leave unchanged".
type PrivateKeyUpdateParams struct {
	Name        *string
	Description *string
}

func (s *ServerService) AddPrivateKey(ctx context.Context, name, privateKeyPEM, description, publicKey string) (*PrivateKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrValidation)
	}
	if strings.TrimSpace(privateKeyPEM) == "" {
		return nil, fmt.Errorf("%w: private_key is required", ErrValidation)
	}

	keyType, derivedPublicKey, err := derivePrivateKeyMetadata(privateKeyPEM)
	if err != nil {
		if !errors.Is(err, errPrivateKeyPassphrase) {
			return nil, err
		}
		// A passphrase-protected key cannot be parsed without its passphrase,
		// so its public half cannot be verified. Store no metadata rather than
		// trusting caller-supplied values.
		keyType, derivedPublicKey = "", ""
	}
	if strings.TrimSpace(publicKey) != "" && derivedPublicKey != "" {
		if err := verifyPublicKey(publicKey, derivedPublicKey); err != nil {
			return nil, err
		}
	}

	encrypted, err := EncryptKey(privateKeyPEM, s.secret)
	if err != nil {
		return nil, err
	}

	row, err := s.store.CreatePrivateKey(ctx, sqlc.CreatePrivateKeyParams{
		Name:         name,
		EncryptedKey: encrypted,
		Type:         optionalText(keyType),
		Description:  optionalText(description),
		PublicKey:    optionalText(derivedPublicKey),
	})
	if err != nil {
		return nil, fmt.Errorf("create private key: %w", err)
	}

	s.logger.Debug("servers: private key added", "private_key_id", row.ID.String())
	return privateKeyFromCreateRow(row), nil
}

func (s *ServerService) GeneratePrivateKey(ctx context.Context, name, keyType, description string, rsaBits int) (*PrivateKey, error) {
	privateKeyPEM, err := generatePrivateKeyPEM(keyType, rsaBits)
	if err != nil {
		return nil, err
	}
	return s.AddPrivateKey(ctx, name, privateKeyPEM, description, "")
}

func (s *ServerService) ListPrivateKeys(ctx context.Context) ([]PrivateKey, error) {
	rows, err := s.store.ListPrivateKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list private keys: %w", err)
	}

	keys := make([]PrivateKey, 0, len(rows))
	for _, row := range rows {
		// Legacy rows predate the metadata columns; derive and store it on
		// first read. A row that cannot be parsed stays without metadata
		// instead of hiding the whole list.
		if row.Type == nil || *row.Type == "" || row.PublicKey == nil || *row.PublicKey == "" {
			full, getErr := s.store.GetPrivateKeyByID(ctx, row.ID)
			if getErr != nil {
				s.logger.Warn("servers: private key metadata lookup failed", "private_key_id", row.ID.String(), "error", getErr)
			} else {
				keys = append(keys, *privateKeyFromRow(s.ensurePrivateKeyMetadata(ctx, full)))
				continue
			}
		}
		keys = append(keys, *privateKeyFromListRow(row))
	}
	return keys, nil
}

func (s *ServerService) GetPrivateKey(ctx context.Context, id uuid.UUID) (*PrivateKey, error) {
	key, err := s.store.GetPrivateKeyByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get private key: %w", err)
	}
	return privateKeyFromRow(s.ensurePrivateKeyMetadata(ctx, key)), nil
}

func (s *ServerService) UpdatePrivateKey(ctx context.Context, id uuid.UUID, params PrivateKeyUpdateParams) (*PrivateKey, error) {
	if params.Name == nil && params.Description == nil {
		return nil, fmt.Errorf("%w: name or description is required", ErrValidation)
	}

	current, err := s.GetPrivateKey(ctx, id)
	if err != nil {
		return nil, err
	}

	name := current.Name
	if params.Name != nil {
		name = strings.TrimSpace(*params.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: name is required", ErrValidation)
		}
	}
	description := current.Description
	if params.Description != nil {
		description = optionalText(*params.Description)
	}

	row, err := s.store.UpdatePrivateKeyProfile(ctx, sqlc.UpdatePrivateKeyProfileParams{
		ID:          pgUUID(id),
		Name:        name,
		Description: description,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update private key: %w", err)
	}
	return privateKeyFromUpdateRow(row), nil
}

func (s *ServerService) DeletePrivateKey(ctx context.Context, id uuid.UUID) error {
	return s.store.InTx(ctx, func(tx *store.Store) error {
		// Locking the row first serialises against a concurrent attach: the
		// attach's FK check holds KEY SHARE on this row, so the next statement
		// sees it and the guarded DELETE refuses instead of orphaning a server.
		if _, err := tx.LockPrivateKey(ctx, pgUUID(id)); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("lock private key: %w", err)
		}
		deleted, err := tx.DeletePrivateKeyIfUnused(ctx, pgUUID(id))
		if err != nil {
			return fmt.Errorf("delete private key: %w", err)
		}
		if deleted == 0 {
			return fmt.Errorf("%w: private key is used by a server or application deploy key", ErrConflict)
		}
		return nil
	})
}

// RevealPrivateKey decrypts the stored PEM. Callers must audit the reveal and
// keep the result out of logs.
func (s *ServerService) RevealPrivateKey(ctx context.Context, id uuid.UUID) (string, error) {
	key, err := s.store.GetPrivateKeyByID(ctx, pgUUID(id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get private key: %w", err)
	}
	plain, err := DecryptKey(key.EncryptedKey, s.secret)
	if err != nil {
		return "", fmt.Errorf("decrypt private key: %w", err)
	}
	return plain, nil
}

func (s *ServerService) ensurePrivateKeyMetadata(ctx context.Context, key sqlc.PrivateKey) sqlc.PrivateKey {
	if key.Type != nil && *key.Type != "" && key.PublicKey != nil && *key.PublicKey != "" {
		return key
	}

	plain, err := DecryptKey(key.EncryptedKey, s.secret)
	if err != nil {
		s.logger.Warn("servers: cannot backfill private key metadata", "private_key_id", key.ID.String(), "error", err)
		return key
	}
	keyType, publicKey, err := derivePrivateKeyMetadata(plain)
	if err != nil {
		s.logger.Debug("servers: cannot backfill private key metadata", "private_key_id", key.ID.String(), "error", err)
		return key
	}
	if err := s.store.SetPrivateKeyMetadata(ctx, sqlc.SetPrivateKeyMetadataParams{
		ID:        key.ID,
		Type:      &keyType,
		PublicKey: &publicKey,
	}); err != nil {
		s.logger.Warn("servers: cannot store private key metadata", "private_key_id", key.ID.String(), "error", err)
		return key
	}

	key.Type = &keyType
	key.PublicKey = &publicKey
	return key
}

func generatePrivateKeyPEM(keyType string, rsaBits int) (string, error) {
	var privateKey any
	var err error

	switch strings.ToLower(strings.TrimSpace(keyType)) {
	case PrivateKeyTypeEd25519:
		_, privateKey, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return "", fmt.Errorf("generate ed25519 key: %w", err)
		}
	case PrivateKeyTypeRSA:
		if rsaBits == 0 {
			rsaBits = defaultRSABits
		}
		if rsaBits < minRSABits || rsaBits > maxRSABits {
			return "", fmt.Errorf("%w: rsa bits must be between %d and %d", ErrValidation, minRSABits, maxRSABits)
		}
		privateKey, err = rsa.GenerateKey(rand.Reader, rsaBits)
		if err != nil {
			return "", fmt.Errorf("generate rsa key: %w", err)
		}
	case PrivateKeyTypeECDSA:
		privateKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return "", fmt.Errorf("generate ecdsa key: %w", err)
		}
	default:
		return "", fmt.Errorf("%w: type must be %s, %s or %s", ErrValidation, PrivateKeyTypeEd25519, PrivateKeyTypeRSA, PrivateKeyTypeECDSA)
	}

	block, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return "", fmt.Errorf("marshal private key: %w", err)
	}
	return string(pem.EncodeToMemory(block)), nil
}

func derivePrivateKeyMetadata(privateKeyPEM string) (string, string, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			return "", "", fmt.Errorf("%w: %w", ErrValidation, errPrivateKeyPassphrase)
		}
		return "", "", fmt.Errorf("%w: invalid private key: %v", ErrValidation, err)
	}

	publicKey := signer.PublicKey()
	keyType, ok := privateKeyTypeOf(publicKey.Type())
	if !ok {
		return "", "", fmt.Errorf("%w: unsupported key type %q", ErrValidation, publicKey.Type())
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))
	return keyType, line, nil
}

func verifyPublicKey(provided, derived string) error {
	given, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.TrimSpace(provided)))
	if err != nil {
		return fmt.Errorf("%w: invalid public_key: %v", ErrValidation, err)
	}
	expected, _, _, _, err := ssh.ParseAuthorizedKey([]byte(derived))
	if err != nil {
		return fmt.Errorf("%w: invalid derived public key: %v", ErrValidation, err)
	}
	if !bytes.Equal(given.Marshal(), expected.Marshal()) {
		return fmt.Errorf("%w: public_key does not match the private key", ErrValidation)
	}
	return nil
}

func privateKeyTypeOf(sshType string) (string, bool) {
	switch sshType {
	case ssh.KeyAlgoED25519:
		return PrivateKeyTypeEd25519, true
	case ssh.KeyAlgoRSA:
		return PrivateKeyTypeRSA, true
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		return PrivateKeyTypeECDSA, true
	default:
		return "", false
	}
}

func optionalText(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func privateKeyFromRow(row sqlc.PrivateKey) *PrivateKey {
	return privateKeyFromParts(row.ID, row.Name, row.Type, row.Description, row.PublicKey, row.CreatedAt)
}

func privateKeyFromListRow(row sqlc.ListPrivateKeysRow) *PrivateKey {
	return privateKeyFromParts(row.ID, row.Name, row.Type, row.Description, row.PublicKey, row.CreatedAt)
}

func privateKeyFromCreateRow(row sqlc.CreatePrivateKeyRow) *PrivateKey {
	return privateKeyFromParts(row.ID, row.Name, row.Type, row.Description, row.PublicKey, row.CreatedAt)
}

func privateKeyFromUpdateRow(row sqlc.UpdatePrivateKeyProfileRow) *PrivateKey {
	return privateKeyFromParts(row.ID, row.Name, row.Type, row.Description, row.PublicKey, row.CreatedAt)
}

func privateKeyFromParts(id pgtype.UUID, name string, keyType, description, publicKey *string, createdAt pgtype.Timestamptz) *PrivateKey {
	key := &PrivateKey{
		ID:          uuidFromPG(id),
		Name:        name,
		Description: description,
		CreatedAt:   createdAt.Time,
	}
	if keyType != nil {
		key.Type = *keyType
	}
	if publicKey != nil {
		key.PublicKey = *publicKey
	}
	return key
}
