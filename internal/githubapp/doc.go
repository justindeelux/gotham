// Package githubapp connects Gotham to GitHub through a GitHub App created
// with the manifest flow, so no client id/secret is ever hand-copied.
//
// Flow: the API serves a manifest that the browser posts to
// github.com/settings/apps/new (or a GitHub Enterprise base URL) with a
// single-use signed state; the callback exchanges the code for the app
// credentials, which are AES-GCM sealed with providers.SealSecret like other
// provider secrets. An install step then records the installation, and repo
// and branch listing use short-lived installation access tokens (JWT RS256,
// cached until expiry).
//
// The GitHub REST surface sits behind the GitHubAPI interface, so tests use a
// fake over httptest and never touch the network. Tokens and private keys are
// never logged and never appear in API responses.
package githubapp
