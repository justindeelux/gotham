// Package ws is the shared realtime channel for the control plane.
//
// Architecture: the node agent streams container logs over gRPC, the control
// plane publishes each chunk to the Redis channel
// logs:{serverID}:{containerID}, the bridge below re-publishes Redis messages
// into the in-memory hub, and the hub fans them out to subscribed WebSocket
// clients connected at WS /api/v1/ws?token=....
//
// When realtime is disabled (REALTIME_ENABLED=false) no Redis client is
// created; publishers write straight into the hub and HTTP clients are
// expected to fall back to polling (5s interval).
package ws
