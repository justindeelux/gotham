package templates

import (
	"errors"
	"fmt"
)

// Sentinel errors mapped to HTTP statuses by the routes layer.
var (
	// ErrNotFound — no template carries the requested slug (404).
	ErrNotFound = errors.New("templates: not found")
	// ErrValidation — invalid template metadata, an invalid field value or
	// a document that does not render (400).
	ErrValidation = errors.New("templates: validation")
	// ErrDisabled — FEATURE_SERVICES=false disables the template surface at
	// runtime, exactly like the service operations (503). Set before startup,
	// Mount is a no-op instead and the routes are absent (404).
	ErrDisabled = errors.New("templates: feature disabled")
)

// notFound reports an absent template slug.
func notFound(slug string) error {
	return fmt.Errorf("%w: no template %q", ErrNotFound, slug)
}
