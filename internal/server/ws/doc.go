// Package ws is the shared realtime channel for the control plane.
//
// Architecture: the node agent streams container logs over gRPC, the control
// plane publishes each chunk to the Redis channel
// logs:{serverID}:{containerID}, the bridge below re-publishes Redis messages
// into the in-memory hub, and the hub fans them out to subscribed WebSocket
// clients connected at WS /api/v1/ws?token=....
//
// Realtime owns the mounted lifecycle (Hub, Redis client, supervised Bridge and
// the active log streams); the control plane closes it on shutdown. A container
// log stream is started through Realtime.StartLogStream, normally driven by the
// POST /api/v1/servers/{id}/containers/{containerID}/logs/stream endpoint when a
// log viewer mounts. Streams are idempotent per channel and are cancelled once
// the channel has had no hub subscribers for a grace period or on shutdown.
//
// When realtime is disabled (REALTIME_ENABLED=false) no Redis client is
// created; publishers write straight into the hub and HTTP clients are
// expected to fall back to polling (5s interval).
package ws
