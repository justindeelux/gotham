// Package files is the control-plane file manager: the read side (Phase 18,
// step 3) and the write side (step 4).
//
// The control plane never touches a node's filesystem directly. Every operation
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
// list/stat/roots are open to any member, and content/download (reads and
// writes) are owner/admin only — file bodies hold secrets such as 0600 compose
// environment files. Every mutation and every content/download read is
// audit-logged (actor user/team, server, root, relpath, op, result) without ever
// recording a file body, a token or a download ticket.
//
// The write path streams to the agent: PUT /content streams the request body
// directly (its Content-Length is the declared size) and a request-body error
// cancels the agent stream so a partial upload is never committed. POST /upload
// reads one multipart part at a time (never ParseMultipartForm), spooling each
// part just long enough to learn its exact size, and reports a per-file result.
//
// The agent dialect is injected as a DialFunc (see Service.SetDial); the mTLS
// dialer lands with *servers.ServerService.DialFileClient. The whole surface is
// gated by FEATURE_FILES, which defaults off: the routes are mounted but answer
// 503 until step 10 signs the feature off. A 501 ErrAgentOutdated is returned
// when the node agent predates the FileService.
//
// Step 9a adds POST /chmod (owner/admin, deploy scope for a token): it changes
// only the low 0o777 permission bits — setuid, setgid and sticky are refused —
// and never follows a symlink. The agent's ChmodResponse is empty, so the
// service re-stats the entry after the call and returns the fresh entry/etag.
package files
