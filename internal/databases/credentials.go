package databases

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

const (
	secretKeyUsername     = "username"
	secretKeyPassword     = "password"
	secretKeyDatabase     = "database"
	secretKeyRootPassword = "root_password"
)

const passwordBytes = 24

var identifierInvalid = regexp.MustCompile(`[^a-z0-9_]+`)

var reservedUsernames = map[string]bool{"root": true, "mysql": true}

const redisUsername = "default"

func generateCredentials(engine, name string) (Credentials, error) {
	password, err := randomPassword()
	if err != nil {
		return Credentials{}, err
	}
	credentials := Credentials{
		Username: deriveIdentifier(name),
		Password: password,
		Database: deriveIdentifier(name),
	}
	switch engine {
	case EngineRedis:
		credentials.Username = redisUsername
		credentials.Database = "0"
	case EngineMySQL, EngineMariaDB:
		if reservedUsernames[credentials.Username] {
			credentials.Username = "gotham_" + credentials.Username
		}
		if credentials.RootPassword, err = randomPassword(); err != nil {
			return Credentials{}, err
		}
	}
	return credentials, nil
}

func randomPassword() (string, error) {
	buf := make([]byte, passwordBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("databases: generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func deriveIdentifier(name string) string {
	identifier := strings.ToLower(strings.TrimSpace(name))
	identifier = strings.Trim(identifierInvalid.ReplaceAllString(identifier, "_"), "_")
	identifier = strings.TrimLeft(identifier, "0123456789")
	if identifier == "" {
		identifier = "gotham"
	}
	if strings.HasPrefix(identifier, "pg_") {
		identifier = "gotham_" + identifier
	}
	if len(identifier) > 32 {
		identifier = strings.Trim(identifier[:32], "_")
	}
	return identifier
}

func rootPassword(c Credentials) string {
	if c.RootPassword != "" {
		return c.RootPassword
	}
	return c.Password
}

func sealCredentials(secret string, databaseID uuid.UUID, c Credentials) ([]Secret, error) {
	pairs := [][2]string{
		{secretKeyUsername, c.Username},
		{secretKeyPassword, c.Password},
		{secretKeyDatabase, c.Database},
		{secretKeyRootPassword, c.RootPassword},
	}
	secrets := make([]Secret, 0, len(pairs))
	for _, pair := range pairs {
		if pair[1] == "" {
			continue
		}
		sealed, err := providers.SealSecret(secret, pair[1])
		if err != nil {
			return nil, fmt.Errorf("databases: seal %s: %w", pair[0], err)
		}
		secrets = append(secrets, Secret{
			DatabaseID: databaseID,
			Key:        pair[0],
			Ciphertext: sealed,
		})
	}
	return secrets, nil
}

func openCredentials(secret string, secrets []Secret) (Credentials, error) {
	var credentials Credentials
	for _, item := range secrets {
		value, err := providers.OpenSecret(secret, item.Ciphertext)
		if err != nil {
			return Credentials{}, fmt.Errorf("databases: open %s: %w", item.Key, err)
		}
		switch item.Key {
		case secretKeyUsername:
			credentials.Username = value
		case secretKeyPassword:
			credentials.Password = value
		case secretKeyDatabase:
			credentials.Database = value
		case secretKeyRootPassword:
			credentials.RootPassword = value
		}
	}
	return credentials, nil
}
