// Package templates ships the built-in one-click service templates. Each
// subdirectory is one template: template.yaml declares the metadata and the
// form fields, compose.yaml is the compose document with {{ .field }}
// placeholders. The control plane's template engine (internal/templates)
// parses and validates every directory at load; this package only carries the
// files into the binary, so a release always ships the exact templates it was
// built from.
//
// See the README in this directory for the format and the HTTP contract.
package templates

import "embed"

// FS holds every built-in template directory.
//
//go:embed wordpress nextcloud n8n uptime-kuma
var FS embed.FS
