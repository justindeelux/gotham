// Package jobguard keeps a backup job and a lifecycle request from touching the
// same resource at the same time. The backup core owns the lease; the deploy
// and service services ask this package before they act. A job that is itself
// stopping or starting its resource marks the context internal, so its own
// lifecycle calls are not refused by its lease.
package jobguard

import (
	"context"

	"github.com/google/uuid"
)

// Active reports whether a job holds the resource.
type Active func(resourceID uuid.UUID) bool

type internalKey struct{}

// WithInternal marks lifecycle calls made by the job that owns the lease.
func WithInternal(ctx context.Context) context.Context {
	return context.WithValue(ctx, internalKey{}, true)
}

// IsInternal reports whether the call comes from the job that owns the lease.
func IsInternal(ctx context.Context) bool {
	internal, _ := ctx.Value(internalKey{}).(bool)
	return internal
}

// Busy reports whether a foreign job holds the resource. A nil guard never
// blocks a request.
func Busy(ctx context.Context, guard Active, resourceID uuid.UUID) bool {
	return guard != nil && !IsInternal(ctx) && guard(resourceID)
}
