package databases

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/justindeelux/gotham/internal/providers"
)

// TestGenerateCredentials pins the per-engine credential shape.
func TestGenerateCredentials(t *testing.T) {
	tests := []struct {
		name     string
		engine   string
		dbName   string
		username string
		database string
		wantRoot bool
	}{
		{
			name:     "postgres derives one identifier",
			engine:   EnginePostgres,
			dbName:   "orders",
			username: "orders",
			database: "orders",
		},
		{
			name:     "mysql keeps two accounts",
			engine:   EngineMySQL,
			dbName:   "orders",
			username: "orders",
			database: "orders",
			wantRoot: true,
		},
		{
			name:     "mysql never creates root",
			engine:   EngineMySQL,
			dbName:   "root",
			username: "gotham_root",
			database: "root",
			wantRoot: true,
		},
		{
			name:     "mariadb keeps two accounts",
			engine:   EngineMariaDB,
			dbName:   "analytics",
			username: "analytics",
			database: "analytics",
			wantRoot: true,
		},
		{
			name:     "postgres never derives a pg_ username",
			engine:   EnginePostgres,
			dbName:   "pg-orders",
			username: "gotham_pg_orders",
			database: "gotham_pg_orders",
		},
		{
			name:     "mongodb roots the init database",
			engine:   EngineMongoDB,
			dbName:   "catalog",
			username: "catalog",
			database: "catalog",
		},
		{
			name:     "redis authenticates the built-in user",
			engine:   EngineRedis,
			dbName:   "cache",
			username: "default",
			database: "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			credentials, err := generateCredentials(tt.engine, tt.dbName)
			if err != nil {
				t.Fatalf("generateCredentials: %v", err)
			}
			if credentials.Username != tt.username {
				t.Errorf("username = %q, want %q", credentials.Username, tt.username)
			}
			if credentials.Database != tt.database {
				t.Errorf("database = %q, want %q", credentials.Database, tt.database)
			}
			if len(credentials.Password) < 16 {
				t.Errorf("password is too short: %q", credentials.Password)
			}
			if strings.ContainsAny(credentials.Password, " \t\"'\\") {
				t.Errorf("password %q carries unsafe characters", credentials.Password)
			}
			if tt.wantRoot {
				if credentials.RootPassword == "" {
					t.Fatal("root password is empty")
				}
				if credentials.RootPassword == credentials.Password {
					t.Error("root password must differ from the user password")
				}
			} else if credentials.RootPassword != "" {
				t.Errorf("root password = %q, want none for this engine", credentials.RootPassword)
			}
		})
	}
}

// TestGenerateCredentialsIsRandom guards against a fixed or seeded password.
func TestGenerateCredentialsIsRandom(t *testing.T) {
	first, err := generateCredentials(EnginePostgres, "orders")
	if err != nil {
		t.Fatalf("generateCredentials: %v", err)
	}
	second, err := generateCredentials(EnginePostgres, "orders")
	if err != nil {
		t.Fatalf("generateCredentials: %v", err)
	}
	if first.Password == second.Password {
		t.Error("two databases generated the same password")
	}
}

// TestDeriveIdentifier covers the SQL-identifier normalisation.
func TestDeriveIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "orders", want: "orders"},
		{input: "My Orders!", want: "my_orders"},
		{input: "  spaced  ", want: "spaced"},
		{input: "123db", want: "db"},
		{input: "___", want: "gotham"},
		{input: "", want: "gotham"},
		{input: "!!!", want: "gotham"},
		{input: strings.Repeat("a", 60), want: strings.Repeat("a", 32)},
		{input: "pg", want: "pg"},
		{input: "pg-orders", want: "gotham_pg_orders"},
		{input: "pg_orders", want: "gotham_pg_orders"},
		{input: "PG-Orders", want: "gotham_pg_orders"},
		{
			input: "pg-" + strings.Repeat("a", 60),
			want:  "gotham_pg_" + strings.Repeat("a", 22),
		},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := deriveIdentifier(tt.input); got != tt.want {
				t.Errorf("deriveIdentifier(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestGenerateCredentialsNeverDerivesPgPrefix guards the live failure where
// a database named pg-orders derived the login pg_orders and PostgreSQL's
// initdb refused it ("role names cannot begin with pg_"), leaving the
// create in error after the full healthcheck window. Every engine's derived
// identifiers must stay clear of the pg_ prefix (and the [a-z0-9_] alphabet
// keeps them valid for MySQL/MariaDB/MongoDB too; Redis uses fixed values).
func TestGenerateCredentialsNeverDerivesPgPrefix(t *testing.T) {
	names := []string{"pg-orders", "pg_orders", "PG-ORDERS", "Pg.Reports"}
	for _, engine := range EngineNames() {
		for _, name := range names {
			credentials, err := generateCredentials(engine, name)
			if err != nil {
				t.Fatalf("generateCredentials(%s, %q): %v", engine, name, err)
			}
			if strings.HasPrefix(credentials.Username, "pg_") {
				t.Errorf("generateCredentials(%s, %q) username = %q, must not start with pg_",
					engine, name, credentials.Username)
			}
			if strings.HasPrefix(credentials.Database, "pg_") {
				t.Errorf("generateCredentials(%s, %q) database = %q, must not start with pg_",
					engine, name, credentials.Database)
			}
		}
	}
}

// TestSealAndOpenCredentials proves the plaintext never reaches the stored
// row: the ciphertext carries neither the password nor its prefix, opens back
// to the original values, and refuses a wrong key.
func TestSealAndOpenCredentials(t *testing.T) {
	credentials := Credentials{
		Username:     "orders",
		Password:     "sup3r-s3cret-value",
		Database:     "orders",
		RootPassword: "root-value",
	}
	databaseID := uuid.New()

	secrets, err := sealCredentials(testSecret, databaseID, credentials)
	if err != nil {
		t.Fatalf("sealCredentials: %v", err)
	}
	if len(secrets) != 4 {
		t.Fatalf("sealed %d credentials, want 4", len(secrets))
	}
	for _, secret := range secrets {
		if secret.DatabaseID != databaseID {
			t.Errorf("secret %q references %s, want %s", secret.Key, secret.DatabaseID, databaseID)
		}
		if secret.Ciphertext == "" {
			t.Errorf("secret %q has no ciphertext", secret.Key)
		}
		if strings.Contains(secret.Ciphertext, credentials.Password) ||
			strings.Contains(secret.Ciphertext, credentials.RootPassword) {
			t.Errorf("secret %q leaks plaintext", secret.Key)
		}
	}

	opened, err := openCredentials(testSecret, secrets)
	if err != nil {
		t.Fatalf("openCredentials: %v", err)
	}
	if opened != credentials {
		t.Errorf("opened = %+v, want %+v", opened, credentials)
	}

	if _, err := openCredentials("wrong-key", secrets); err == nil {
		t.Error("openCredentials accepted a wrong secret")
	}
}

// TestSealSkipsAbsentCredentials: engines without a root login store three
// values, not four empty ones.
func TestSealSkipsAbsentCredentials(t *testing.T) {
	credentials, err := generateCredentials(EnginePostgres, "orders")
	if err != nil {
		t.Fatalf("generateCredentials: %v", err)
	}
	secrets, err := sealCredentials(testSecret, uuid.New(), credentials)
	if err != nil {
		t.Fatalf("sealCredentials: %v", err)
	}
	if len(secrets) != 3 {
		t.Fatalf("sealed %d credentials, want 3 (no root password)", len(secrets))
	}
	for _, secret := range secrets {
		if secret.Key == secretKeyRootPassword {
			t.Error("postgres must not store a root_password secret")
		}
	}
}

// TestOpenCredentialsIgnoresUnknownKeys keeps a future credential from
// breaking older readers.
func TestOpenCredentialsIgnoresUnknownKeys(t *testing.T) {
	password, err := providers.SealSecret(testSecret, "pw")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	opened, err := openCredentials(testSecret, []Secret{
		{Key: "password", Ciphertext: password},
		{Key: "future_key", Ciphertext: password},
	})
	if err != nil {
		t.Fatalf("openCredentials: %v", err)
	}
	if opened.Password != "pw" {
		t.Errorf("password = %q, want pw", opened.Password)
	}
}

// TestOpenCredentialsRejectsCorruptedValue fails loudly instead of booting a
// database with an empty password.
func TestOpenCredentialsRejectsCorruptedValue(t *testing.T) {
	_, err := openCredentials(testSecret, []Secret{{Key: "password", Ciphertext: "not-base64!"}})
	if err == nil {
		t.Fatal("openCredentials accepted a corrupted ciphertext")
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("error %q should name the credential", err)
	}
}
