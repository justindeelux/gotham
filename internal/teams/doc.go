// Package teams implements first-class teams with roles (Phase 8, BE-8.2): the
// team and membership model, the invite lifecycle, the active-team request
// scope every resource package filters by, and the HTTP surface for team
// management.
//
// A request's active team is resolved once by the server's RequireTeam
// middleware from the optional X-Team-Id header, falling back to the caller's
// personal team, and is carried in the request context (see Scope). Domain
// packages read it through ScopeFor; they never import the HTTP server.
package teams
