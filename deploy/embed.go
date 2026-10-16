// Package deploy exposes the fixed deploy artifacts the gotham binary embeds
// so it can install and refresh the root-owned host helper and tool installer
// itself (see internal/hostbootstrap). The embedded bytes must stay byte-equal
// to the files on disk; TestEmbeddedScriptsMatchDeployFiles enforces it.
package deploy

import _ "embed"

// HostctlScript is the fixed, root-owned host helper for instance settings
// (deploy/gotham-hostctl.sh).
//
//go:embed gotham-hostctl.sh
var HostctlScript []byte

// ToolctlScript is the fixed, root-owned host-tool installer
// (deploy/gotham-toolctl.sh).
//
//go:embed gotham-toolctl.sh
var ToolctlScript []byte
