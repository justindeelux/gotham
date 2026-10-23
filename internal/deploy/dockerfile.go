package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	MaxDockerfileBytes    = 64 << 10
	MaxBuildArgs          = 64
	MaxBuildArgValueBytes = 4 << 10
)

func ValidateDockerfileContent(content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%w: Dockerfile content is required", ErrValidation)
	}
	if err := checkTextBytes(content, "Dockerfile content"); err != nil {
		return err
	}
	if len(content) > MaxDockerfileBytes {
		return fmt.Errorf("%w: Dockerfile content exceeds %d bytes", ErrValidation, MaxDockerfileBytes)
	}
	if !hasFromInstruction(content) {
		return fmt.Errorf("%w: Dockerfile must contain a FROM instruction", ErrValidation)
	}
	return nil
}

func checkTextBytes(s, what string) error {
	if strings.ContainsRune(s, 0) {
		return fmt.Errorf("%w: %s must not contain NUL bytes", ErrValidation, what)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: %s must be valid UTF-8", ErrValidation, what)
	}
	return nil
}

func hasFromInstruction(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && strings.EqualFold(fields[0], "FROM") {
			return true
		}
	}
	return false
}

func ValidateBuildArgs(args map[string]string) error {
	if len(args) > MaxBuildArgs {
		return fmt.Errorf("%w: too many build args (max %d)", ErrValidation, MaxBuildArgs)
	}
	for key, value := range args {
		if err := validateEnvKey(strings.TrimSpace(key)); err != nil {
			return err
		}
		if err := checkTextBytes(value, fmt.Sprintf("build arg %q value", key)); err != nil {
			return err
		}
		if len(value) > MaxBuildArgValueBytes {
			return fmt.Errorf("%w: build arg %q value exceeds %d bytes", ErrValidation, key, MaxBuildArgValueBytes)
		}
	}
	return nil
}

func normalizeBuildArgs(args map[string]string) map[string]string {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]string, len(args))
	for key, value := range args {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			out[trimmed] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func writeDockerfileContext(dir, content string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0o644)
}
