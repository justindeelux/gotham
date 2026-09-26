// Package agent implements the Gotham node agent that runs on each managed
// server.
//
// The agent registers with the control plane over gRPC, serves the DockerService
// RPCs on its own mTLS listener, and streams resource heartbeats. It must not
// import any package under gotham/internal, which is reserved for the control
// plane; the binary is built and shipped on its own.
package agent
