package templates

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound   = errors.New("templates: not found")
	ErrValidation = errors.New("templates: validation")
	ErrDisabled   = errors.New("templates: feature disabled")
)

func notFound(slug string) error {
	return fmt.Errorf("%w: no template %q", ErrNotFound, slug)
}
