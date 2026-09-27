// Package builds implements the application build engines. An engine turns a
// cloned source tree into a container image with the standardized tag
// gotham/{appID}:{deployID}, delegating the actual build to an ImageBuilder:
// the node agent in production, a local Docker daemon in development.
//
// The Dockerfile and static engines are fully implemented. Railpack and
// Buildpacks implement detection only for now; their Build returns
// ErrEngineNotWired so a caller never receives a fabricated image.
package builds
