// Package deploy runs the application deployment pipeline (Phase 4, BE-4.3):
// the control plane clones the repository, builds the image through the build
// engines (the node agent performs the Docker build and pushes to the node's
// internal registry), then starts the container with the application's
// environment variables, decrypted secrets, volume map and port mapping, and
// gates success on a post-start healthcheck. The runtime payload defaults
// PORT to the application's container port whenever the application declares
// one and neither an env var nor a secret defines PORT — an explicit value
// (including a sealed secret reference) always wins and port 0 injects
// nothing — so images built without a Dockerfile listen where the host port
// mapping points, the same way Heroku/Railway/Coolify inject PORT.
//
// Every deployment is a persisted state machine:
//
//	queued → cloning → building → pushing → starting → running | failed
//
// A rollback skips cloning and building and redeploys the image tag recorded on
// an earlier deployment (queued → pushing → starting → running). Each
// transition is written to the deployments table and mirrored to Redis on the
// logs:{serverID}:{deploymentID} channel — the same logs:* pattern the
// realtime bridge already pattern-subscribes — so the UI streams deploy steps
// live without any bridge change.
//
// Secrets are stored AES-256-GCM sealed (providers.SealSecret, the single
// crypto helper in this codebase) and decrypted only when the runtime payload
// is assembled for the agent; plaintext never reaches the database or the logs.
//
// The whole surface is disable-able with FEATURE_APPLICATIONS=false: the routes
// do not mount and no worker starts, so Phases 0–3 stay unaffected.
package deploy
