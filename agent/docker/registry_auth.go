package docker

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
	registryAuthUser           = "gotham"
	registryCredentialFilename = "registry.auth.json"
	registryHtpasswdFilename   = "registry.htpasswd"
	registryHtpasswdPath       = "/etc/gotham/registry.htpasswd"
	registryAuthRealm          = "Gotham Registry"
	registryAuthFileMode       = 0o600
	registryPasswordBytes      = 32
)

type registryAuth struct {
	Address  string `json:"-"`
	Username string `json:"username"`
	Password string `json:"password"`
}

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

func prepareRegistryAuth(stateDir string) (registryAuth, string, bool, error) {
	resolved, err := resolveRegistryStateDir(stateDir)
	if err != nil {
		return registryAuth{}, "", false, err
	}
	if err := os.MkdirAll(resolved, 0o700); err != nil {
		return registryAuth{}, "", false, fmt.Errorf("docker: create registry state dir: %w", err)
	}

	credentialPath := filepath.Join(resolved, registryCredentialFilename)
	htpasswdPath := filepath.Join(resolved, registryHtpasswdFilename)

	auth, err := readRegistryAuth(credentialPath)
	if err != nil {
		return registryAuth{}, "", false, err
	}

	reused := auth.Username != "" && auth.Password != "" && registryHtpasswdMatches(htpasswdPath, auth)
	if !reused {
		if auth.Username == "" || auth.Password == "" {
			password, err := randomRegistryPassword()
			if err != nil {
				return registryAuth{}, "", false, err
			}
			auth = registryAuth{Username: registryAuthUser, Password: password}
			if err := writeRegistryCredential(credentialPath, auth); err != nil {
				return registryAuth{}, "", false, err
			}
		}
		if err := writeRegistryHtpasswd(htpasswdPath, auth); err != nil {
			return registryAuth{}, "", false, err
		}
	}
	return auth, htpasswdPath, reused, nil
}

func resolveRegistryStateDir(stateDir string) (string, error) {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return "", errors.New("docker: registry state dir is required to authenticate the node registry")
	}
	absolute, err := filepath.Abs(stateDir)
	if err != nil {
		return "", fmt.Errorf("docker: resolve registry state dir: %w", err)
	}
	return absolute, nil
}

func persistedRegistryAuth(stateDir string) (registryAuth, bool, error) {
	resolved, err := resolveRegistryStateDir(stateDir)
	if err != nil {
		return registryAuth{}, false, err
	}
	auth, err := readRegistryAuth(filepath.Join(resolved, registryCredentialFilename))
	if err != nil {
		return registryAuth{}, false, err
	}
	if auth.Username == "" || auth.Password == "" {
		return registryAuth{}, false, nil
	}
	if !registryHtpasswdMatches(filepath.Join(resolved, registryHtpasswdFilename), auth) {
		return registryAuth{}, false, nil
	}
	return auth, true, nil
}

func registryHtpasswdMatches(path string, auth registryAuth) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	user, hash, found := strings.Cut(strings.TrimSpace(string(data)), ":")
	if !found || user != auth.Username || hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(auth.Password)) == nil
}

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

func writeRegistryCredential(path string, auth registryAuth) error {
	data, err := json.Marshal(auth)
	if err != nil {
		return fmt.Errorf("docker: encode registry credential: %w", err)
	}
	return writeSecretFile(path, data)
}

func writeRegistryHtpasswd(path string, auth registryAuth) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(auth.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("docker: hash registry password: %w", err)
	}
	line := auth.Username + ":" + string(hash) + "\n"
	return writeSecretFile(path, []byte(line))
}

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
	if err := os.Chmod(path, registryAuthFileMode); err != nil {
		return fmt.Errorf("docker: chmod registry credential file: %w", err)
	}
	return nil
}

func randomRegistryPassword() (string, error) {
	buf := make([]byte, registryPasswordBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("docker: generate registry password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
