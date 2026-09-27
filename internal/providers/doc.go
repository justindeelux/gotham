// Package providers is the control-plane side of source-provider integration.
//
// A source provider is a Git host (GitHub, GitLab, Gitea) that the control
// plane can read repositories and branches from, and — from BE-4.4 onward —
// install webhooks on. Each provider connection carries its own OAuth
// application, persisted in the providers table; the account access token is
// sealed with AES-256-GCM before it reaches storage and is opened only when a
// provider API call is made.
//
// The SourceProvider interface is the Phase-1 OAuthProvider pattern promoted to
// source access: every implementation owns an oauth2.Config and exchanges the
// authorization code exactly like the login flow, then speaks the provider's
// REST API for repos, branches and webhooks. Adding a provider means adding one
// implementation and one factory entry — the deploy flow never changes.
package providers
