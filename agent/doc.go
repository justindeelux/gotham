// Package agent implements the Gotham node agent that runs on each managed
// server.
//
// The root package is the composition layer: it loads configuration, owns the
// TLS identity, registers with the control plane, streams heartbeats and hosts
// the gRPC server. Each functional implementation lives in its own subpackage:
//
//	agent/files     confined FileService (browse, write, archive, trash)
//	agent/terminal  host and container terminal sessions
//	agent/docker    Docker client, Docker/Build/Compose/volume services
//	agent/proxy     Traefik configuration and ACME storage service
//	agent/tools     host tool catalog service and tool-use tracking
//	agent/updates   signed self-update engine and backoff state
//	agent/fsroot    confined filesystem root primitives
//	agent/stats     host resource sampling for heartbeats
//
// Dependencies point inward: the root imports the subpackages, and a
// subpackage never imports the root. leaf packages are proxy, fsroot, stats and
// tools; docker imports proxy and tools; files imports proxy and fsroot.
//
// This package must not import any package under gotham/internal, which is
// reserved for the control plane; the binary is built and shipped on its own.
package agent
