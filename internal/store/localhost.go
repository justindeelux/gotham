package store

import _ "embed"

//go:embed migrations/00051_legacy_local_node.sql
var ReconcileLocalhostSQL string
