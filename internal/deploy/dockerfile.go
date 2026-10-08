package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Dockerfile source limits (GS-7). The content travels as the build context
// to the node, so it is capped well under the context budget; build args ride
// the same BuildImage meta as git builds.
const (
	// MaxDockerfileBytes caps pasted Dockerfile text at 64 KiB.
	MaxDockerfileBytes = 64 << 10
	// MaxBuildArgs caps the number of --build-arg pairs per application.
	MaxBuildArgs = 64
	// MaxBuildArgValueBytes caps one build-arg value at 4 KiB.
	MaxBuildArgValueBytes = 4 << 10
)

// ValidateDockerfileContent checks pasted Dockerfile text without executing
// anything: it must be non-empty, fit the size limit, and contain a FROM
// instruction (comments and blank lines ignored, parser directives honoured).
// Anything deeper (valid base image, buildable stages) is the node builder's
// job at build time.
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

// checkTextBytes rejects what Postgres text/jsonb columns reject: NUL bytes
// and invalid UTF-8. Without it a pasted value would travel to the database
// and surface as a 500 instead of a 400.
func checkTextBytes(s, what string) error {
	if strings.ContainsRune(s, 0) {
		return fmt.Errorf("%w: %s must not contain NUL bytes", ErrValidation, what)
	}
	if !utf8.ValidString(s) {
		return fmt.Errorf("%w: %s must be valid UTF-8", ErrValidation, what)
	}
	return nil
}

// hasFromInstruction reports whether the text contains a FROM line. Only
// leading blank lines, comments and `#`-style parser directives may precede
// the first instruction search; matching is case-insensitive on the first
// word, which is all the orchestrator gate needs.
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

// ValidateBuildArgs checks the optional --build-arg pairs: keys follow the
// same container-variable shape as env keys, values are bounded, and the
// collection stays small enough for the BuildImage meta.
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

// normalizeBuildArgs trims keys, drops empty keys with empty values left over
// from form drafts, and returns nil for an empty collection so the stored
// JSON stays a stable '{}'.
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

// writeDockerfileContext materializes pasted Dockerfile text as the build
// tree: the file is the whole context (Dockerfile only, no repo checkout).
// The caller owns dir; a retried step re-enters with the same directory, so a
// stale repo checkout is cleared first like the git cloner does.
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
