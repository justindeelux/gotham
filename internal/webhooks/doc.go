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
//
// The package depends on narrow seams (Repository, Installer, Deployer) rather
// than on the HTTP server, so the route tests drive a real Service with fakes
// behind it.
package webhooks
