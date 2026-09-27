// Package containers is the control-plane side of container management.
//
// The control plane never talks to Docker directly. Every operation routes
// through the node agent over gRPC: this package resolves the target server
// via the Phase 2 node registry, dials that node's agent, and maps the agent
// contract onto the shared Container DTO served by the HTTP API.
//
// The agent dial is injected as a DialFunc. The mTLS dialer lands with the
// sibling P3-CONN package; until it is plugged in (see Service.SetDial) all
// operations report ErrAgentUnavailable. List results are cached in Redis with
// a short TTL; a cache miss or an unreachable Redis simply falls through to
// the agent, so container management keeps working without Redis.
package containers
