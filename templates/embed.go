package templates

import "embed"

//go:embed wordpress nextcloud n8n uptime-kuma bugsink changedetection checkmate elasticsearch embystat glitchtip goatcounter grafana healthchecks meilisearch metabase openobserve openpanel postiz redis-insight searxng sequin sessy statusnook typesense umami wakapi
var FS embed.FS
