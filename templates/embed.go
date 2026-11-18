package templates

import "embed"

//go:embed */template.yaml */compose.yaml
var FS embed.FS
