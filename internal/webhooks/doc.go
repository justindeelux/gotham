// Package webhooks turns Git-host push notifications into deployments.
//
// It owns three things:
//
//   - Signature verification. Every delivery from GitHub (HMAC SHA-256 in
//     X-Hub-Signature-256), GitLab (shared token in X-Gitlab-Token) and Gitea
//     (HMAC SHA-256 hex in X-Gitea-Signature) is checked in constant time
//     against the secret the control plane generated when it installed the
//     hook. An unverifiable delivery never reaches the deploy service.
//   - Anti-spam. A per-client-IP token bucket bounds how many deliveries are
//     examined at all, and every delivery that is acted on is claimed in
//     webhook_events keyed by commit SHA, so a provider re-delivering the same
//     push (retries, redelivers, parallel hooks) cannot start a second
//     deployment.
//   - The hook lifecycle: installing the hook when an application is created
//     and removing it when the application is deleted, exposed to the HTTP
//     layer as authenticated management routes.
//   - Preview deployments (BE-8.1, FEATURE_PREVIEWS): a verified
//     pull_request delivery claims the delivery in the preview-specific
//     ledger (an atomic, expiring in-flight lease deduped against the live
//     binding's current head or an in-flight attempt for that head; never a
//     permanent handled-SHA set), creates (or refreshes) a sibling
//     application cloned from the base application (plain env vars and the
//     shared deploy key only; secrets, storages, unverifiable head identities
//     and forks are excluded), deploys the PR head branch through the same
//     deploy service, and records the binding in preview_deploys. A close
//     persists its intent before tearing the sibling down, completes
//     atomically (binding deleted + ledger cleared) and is re-attempted by an
//     hourly orphan-only sweep when it fails. The surface is disabled by
//     FEATURE_PREVIEWS=false without touching push handling.
//
// The package depends on narrow seams (Repository, Installer, Deployer,
// PreviewProvisioner, Commenter) rather than on the HTTP server, so the route
// tests drive a real Service with fakes behind it.
package webhooks
