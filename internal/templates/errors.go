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
)

// notFound reports an absent template slug.
func notFound(slug string) error {
	return fmt.Errorf("%w: no template %q", ErrNotFound, slug)
}
