// Package files is the control-plane read side of the remote file manager
// (Phase 18, step 3).
//
// The control plane never touches a node's filesystem directly. Every read
// routes through the node agent's FileService over gRPC: this package resolves
// the target server through the Phase 2 node registry, dials that node's agent,
// and maps the agent contract onto the HTTP API under
// /api/v1/servers/{id}/files. Files are addressed as (root, path) where root is
// a name from ListRoots and path is relative to that root; the agent is the
// confinement boundary (os.OpenRoot), this package is the authorization
// boundary.
//
// RBAC is stricter than the rest of the node surface: a browsable server must
// belong to a team (or the caller is on the pre-teams path with no team scope),
// list/stat/roots are open to any member, and content/download are owner/admin
// only — file bodies hold secrets such as 0600 compose environment files.
//
// The agent dialect is injected as a DialFunc (see Service.SetDial); the mTLS
// dialer lands with *servers.ServerService.DialFileClient. The whole surface is
// gated by FEATURE_FILES, which defaults off: the routes are mounted but answer
// 503 until step 10 signs the feature off. A 501 ErrAgentOutdated is returned
// when the node agent predates the FileService.
package files
