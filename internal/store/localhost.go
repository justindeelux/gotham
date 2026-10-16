package store

import _ "embed"

// ReconcileLocalhostSQL preserves custom local node identities before startup
// creates the mandatory localhost entry. Execute it inside a transaction.
//
//go:embed migrations/00051_legacy_local_node.sql
var ReconcileLocalhostSQL string
