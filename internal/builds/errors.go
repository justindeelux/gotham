package builds

import "errors"

var (
	ErrEngineNotWired = errors.New("builds: engine not wired yet")
	ErrNoEngine       = errors.New("builds: no build engine detected")
	ErrValidation     = errors.New("builds: invalid build request")
)
