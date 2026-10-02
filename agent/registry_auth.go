package agent

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	// registryAuthUser is the single user the node-local registry accepts.
	registryAuthUser = "gotham"
	// registryCredentialFilename stores the generated username/password in the
	// agent state directory, mode 0600.
	registryCredentialFilename = "registry.auth.json"
	// registryHtpasswdFilename is the bcrypt htpasswd file the registry reads.
	registryHtpasswdFilename = "registry.htpasswd"
	// registryHtpasswdPath is where the htpasswd file is mounted inside the
	// registry container.
	registryHtpasswdPath = "/etc/gotham/registry.htpasswd"
	// registryAuthRealm is the realm the registry advertises.
	registryAuthRealm = "Gotham Registry"
	// registryAuthFileMode is 0600: the credential must not be group/world
	// readable.
	registryAuthFileMode = 0o600
	// registryPasswordBytes is the entropy of a generated registry password.
	registryPasswordBytes = 32
)

// registryAuth is one node-local registry credential. Address is the registry
// host:port the credential belongs to; it is held in memory only and never
// persisted, because the published port can change across recreations.
type registryAuth struct {
	Address  string `json:"-"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// header renders the Docker X-Registry-Auth value: base64-encoded JSON with the
// username, password and the registry server address. An empty credential is
// the anonymous config.
func (a registryAuth) header() (string, error) {
	if a.Username == "" {
		return anonymousRegistryAuth, nil
	}
	encoded, err := json.Marshal(map[string]string{
		"username":      a.Username,
		"password":      a.Password,
		"serveraddress": a.Address,
	})
	if err != nil {
		return "", fmt.Errorf("docker: encode registry auth: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encoded), nil
}

// prepareRegistryAuth writes (or reloads) the node-local registry credential in
// stateDir and returns it alongside the host path of the htpasswd file. It is
// idempotent: a stored credential is reused so a restart keeps the same
// password, and the htpasswd file is rewritten every time so a deleted file
// self-heals. The returned credential has an empty Address; the caller sets it
// once the published port is known.
func prepareRegistryAuth(stateDir string) (registryAuth, string, error) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return registryAuth{}, "", errors.New("docker: registry state dir is required to authenticate the node registry")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return registryAuth{}, "", fmt.Errorf("docker: create registry state dir: %w", err)
	}

	credentialPath := filepath.Join(stateDir, registryCredentialFilename)
	htpasswdPath := filepath.Join(stateDir, registryHtpasswdFilename)

	auth, err := readRegistryAuth(credentialPath)
	if err != nil {
		return registryAuth{}, "", err
	}
	if auth.Password == "" || auth.Username == "" {
		password, err := randomRegistryPassword()
		if err != nil {
			return registryAuth{}, "", err
		}
		auth = registryAuth{Username: registryAuthUser, Password: password}
		if err := writeRegistryCredential(credentialPath, auth); err != nil {
			return registryAuth{}, "", err
		}
	}
	if err := writeRegistryHtpasswd(htpasswdPath, auth); err != nil {
		return registryAuth{}, "", err
	}
	return auth, htpasswdPath, nil
}

// readRegistryAuth loads a persisted credential, returning the zero value when
// the file does not exist or is unreadable as JSON (a corrupt file is
// regenerated rather than failing the whole agent).
func readRegistryAuth(path string) (registryAuth, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return registryAuth{}, nil
	}
	if err != nil {
		return registryAuth{}, fmt.Errorf("docker: read registry credential: %w", err)
	}
	var auth registryAuth
	if err := json.Unmarshal(data, &auth); err != nil {
		return registryAuth{}, nil
	}
	return auth, nil
}

// writeRegistryCredential persists the credential mode 0600.
func writeRegistryCredential(path string, auth registryAuth) error {
	data, err := json.Marshal(auth)
	if err != nil {
		return fmt.Errorf("docker: encode registry credential: %w", err)
	}
	return writeSecretFile(path, data)
}

// writeRegistryHtpasswd writes the bcrypt htpasswd entry the registry:2
// distribution expects. The registry supports only bcrypt entries, so the hash
// is generated in-process rather than by an external htpasswd tool.
func writeRegistryHtpasswd(path string, auth registryAuth) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(auth.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("docker: hash registry password: %w", err)
	}
	line := auth.Username + ":" + string(hash) + "\n"
	return writeSecretFile(path, []byte(line))
}

// writeSecretFile writes data mode 0600, truncating any existing file.
func writeSecretFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, registryAuthFileMode)
	if err != nil {
		return fmt.Errorf("docker: write registry credential file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("docker: write registry credential file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("docker: close registry credential file: %w", err)
	}
	// OpenFile honours umask, so enforce 0600 explicitly.
	if err := os.Chmod(path, registryAuthFileMode); err != nil {
		return fmt.Errorf("docker: chmod registry credential file: %w", err)
	}
	return nil
}

// randomRegistryPassword returns a URL-safe random password.
func randomRegistryPassword() (string, error) {
	buf := make([]byte, registryPasswordBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("docker: generate registry password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
