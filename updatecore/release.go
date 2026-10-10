// Package updatecore holds the transport-agnostic self-update engine shared by
// the control plane (internal/updates) and the node agent (agent/). It is
// deliberately free of any HTTP-server or control-plane dependency so agent/
// can import it without importing internal/ (AGENTS.md invariant).
//
// It owns the trust chain — Ed25519 signature and manifest verification, the
// semver comparison, the bounded safe download client, and the atomic
// gap-free binary swap with rollback and regular-file guards — but not the
// release API that resolves a candidate (internal/updates) nor the HTTP/gRPC
// surfaces.
package updatecore

import "time"

// Release is a resolved update candidate: the version to install together with
// the URLs of its platform asset, signed manifest and manifest signature. It is
// transport-agnostic: the control plane fills it from the GitHub Releases API
// and offers it to agents over mTLS.
type Release struct {
	Version              string
	Tag                  string
	Channel              string
	Prerelease           bool
	Notes                string
	PublishedAt          time.Time
	Arch                 string
	AssetName            string
	AssetURL             string
	ManifestName         string
	ManifestURL          string
	ManifestSignatureURL string
	AssetSize            int64
	// HTMLURL is the human release page (the Releases API html_url), used for
	// the "View on GitHub" link. It is display-only: never fetched, and only
	// kept when it is a valid https URL.
	HTMLURL string
	// SHA256 is the artifact digest carried by the signed manifest. It is
	// optional in a locally built Release but always set by the CP offer, so the
	// agent can bind the offer to the manifest it verifies.
	SHA256 string
}
