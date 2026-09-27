package builds

import "errors"

// Sentinel errors returned by the build engines and mapped to deploy status by
// the orchestration layer.
var (
	// ErrEngineNotWired reports a detected engine whose Build is not
	// implemented yet. It is never masked with a fabricated image.
	ErrEngineNotWired = errors.New("builds: engine not wired yet")
	// ErrNoEngine reports that no engine recognised the repository.
	ErrNoEngine = errors.New("builds: no build engine detected")
	// ErrValidation reports a malformed build request or unsupported input.
	ErrValidation = errors.New("builds: invalid build request")
)
