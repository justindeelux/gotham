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

// Sealed credential keys stored in database_secrets. Every engine stores the
// three base values; MySQL and MariaDB add a dedicated root login.
const (
	secretKeyUsername     = "username"
	secretKeyPassword     = "password"
	secretKeyDatabase     = "database"
	secretKeyRootPassword = "root_password"
)

// passwordBytes is the entropy behind a generated password. base64url keeps it
// URL-, shell- and DSN-safe without reducing strength.
const passwordBytes = 24

// identifierInvalid matches everything outside the [a-z0-9_] alphabet of a
// SQL identifier derived from the database name.
var identifierInvalid = regexp.MustCompile(`[^a-z0-9_]+`)

// reservedUsernames are logins a server image claims for itself: MySQL refuses
// MYSQL_USER=root outright, and creating a second "root" elsewhere only invites
// confusion, so a name that derives to one of these is prefixed instead.
var reservedUsernames = map[string]bool{"root": true, "mysql": true}

// redisUsername is the built-in ACL user Redis authenticates under requirepass.
const redisUsername = "default"

// generateCredentials builds the login of a new database: an identifier
// derived from its name, a fresh random password, and — for MySQL/MariaDB — a
// separate root password because those images keep two accounts.
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
		// Redis has no user or database name to create; the built-in user and
		// database 0 are what a client connects with.
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

// randomPassword returns a base64url password of passwordBytes entropy.
func randomPassword() (string, error) {
	buf := make([]byte, passwordBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("databases: generate password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// deriveIdentifier turns a database name into a lower-case SQL identifier:
// invalid characters collapse to "_", leading digits and separators are
// trimmed, and an unrecognisable name falls back to "gotham". The result is
// truncated to fit the shortest username limit of the supported engines.
func deriveIdentifier(name string) string {
	identifier := strings.ToLower(strings.TrimSpace(name))
	identifier = strings.Trim(identifierInvalid.ReplaceAllString(identifier, "_"), "_")
	identifier = strings.TrimLeft(identifier, "0123456789")
	if identifier == "" {
		identifier = "gotham"
	}
	if len(identifier) > 32 {
		identifier = strings.Trim(identifier[:32], "_")
	}
	return identifier
}

// rootPassword returns the dedicated root login when one was generated,
// falling back to the regular password so an engine never boots with an empty
// credential.
func rootPassword(c Credentials) string {
	if c.RootPassword != "" {
		return c.RootPassword
	}
	return c.Password
}

// sealCredentials seals every non-empty credential with the AES-256-GCM helper
// (providers.SealSecret) for storage in database_secrets. The database row
// keeps only the reference — the database_id foreign key — never plaintext.
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

// openCredentials reverses sealCredentials. It is called when the runtime
// payload is assembled for the agent and when the owner asks to see the
// credentials; unknown keys are ignored so a future credential can travel
// through the same table.
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
