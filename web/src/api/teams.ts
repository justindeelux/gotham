import { http } from "./http";
import { isApiError } from "./servers";

/**
 * Typed client for the team-management routes served by `internal/teams`
 * (see Mount in routes.go for the contract):
 *
 *   GET    /teams                      POST   /teams
 *   GET    /teams/{id}                 PATCH  /teams/{id}
 *   DELETE /teams/{id}
 *   GET    /teams/{id}/members
 *   PATCH  /teams/{id}/members/{userID}
 *   DELETE /teams/{id}/members/{userID}
 *   GET    /teams/{id}/invites         POST   /teams/{id}/invites
 *   DELETE /teams/{id}/invites/{inviteID}
 *   POST   /invites/accept
 *
 * The routes carry no admin scope: authorization comes from the caller's role
 * in the team each path names. `read_only` may read but never mutate, so the
 * UI gates mutations on the role `GET /teams` reports. Disabled
 * (FEATURE_TEAMS=false) every route is unmounted and answers 404.
 *
 * Paths are relative to the shared axios instance (`baseURL: /api/v1`), so the
 * auth header and refresh-on-401 behaviour come from `./http` unchanged.
 */

/** A member's permission level inside one team (Role in internal/teams/role.go). */
export type TeamRole = "owner" | "admin" | "read_only";

/** One team; `role` is the requesting caller's role in it. */
export interface Team {
  id: string;
  name: string;
  is_personal: boolean;
  role: TeamRole;
  created_at: string;
  updated_at: string;
}

/** One membership, joined with the account email for display. */
export interface TeamMember {
  user_id: string;
  email: string;
  role: TeamRole;
  created_at: string;
}

/** One invite. The token is never part of a stored invite read. */
export interface TeamInvite {
  id: string;
  team_id: string;
  email: string;
  role: TeamRole;
  expires_at: string;
  accepted_at?: string;
  created_at: string;
}

/**
 * The create answer: the invite plus the raw token and the relative accept
 * endpoint, returned exactly once. Callers must show it and never persist it.
 */
export interface CreatedTeamInvite extends TeamInvite {
  token: string;
  accept_url: string;
}

/** Wire envelopes. */
interface TeamEnvelope {
  team: Team;
}
interface TeamListEnvelope {
  teams: Team[];
}
interface MemberListEnvelope {
  members: TeamMember[];
}
interface InviteListEnvelope {
  invites: TeamInvite[];
}
interface CreatedInviteEnvelope {
  invite: CreatedTeamInvite;
}

/** listTeams returns every team the caller is a member of. */
export async function listTeams(): Promise<Team[]> {
  const response = await http.get<TeamListEnvelope>("/teams");
  return response.data.teams ?? [];
}

/** createTeam stores a team; the caller becomes its owner. */
export async function createTeam(name: string): Promise<Team> {
  const response = await http.post<TeamEnvelope>("/teams", { name });
  return response.data.team;
}

/** renameTeam renames a team (owner/admin). */
export async function renameTeam(id: string, name: string): Promise<Team> {
  const response = await http.patch<TeamEnvelope>(`/teams/${id}`, { name });
  return response.data.team;
}

/**
 * deleteTeam deletes a team (owner only, never a personal team). A team that
 * still owns resources answers 409 with the backend's message.
 */
export async function deleteTeam(id: string): Promise<void> {
  await http.delete(`/teams/${id}`);
}

/** listMembers returns a team's memberships (any member may read). */
export async function listMembers(teamId: string): Promise<TeamMember[]> {
  const response = await http.get<MemberListEnvelope>(
    `/teams/${teamId}/members`,
  );
  return response.data.members ?? [];
}

/** updateMemberRole changes one membership's role (owner/admin). */
export async function updateMemberRole(
  teamId: string,
  userId: string,
  role: TeamRole,
): Promise<TeamMember> {
  const response = await http.patch<{ member: TeamMember }>(
    `/teams/${teamId}/members/${userId}`,
    { role },
  );
  return response.data.member;
}

/**
 * removeMember drops one membership (owner/admin, or the member themselves).
 * The last owner and a personal team's owner membership are refused with 409.
 */
export async function removeMember(
  teamId: string,
  userId: string,
): Promise<void> {
  await http.delete(`/teams/${teamId}/members/${userId}`);
}

/** listInvites returns a team's invites (owner/admin). */
export async function listInvites(teamId: string): Promise<TeamInvite[]> {
  const response = await http.get<InviteListEnvelope>(
    `/teams/${teamId}/invites`,
  );
  return response.data.invites ?? [];
}

/**
 * createInvite issues an invite and returns its one-time token. Invites may
 * grant `admin` or `read_only` only — never ownership.
 */
export async function createInvite(
  teamId: string,
  email: string,
  role: TeamRole,
): Promise<CreatedTeamInvite> {
  const response = await http.post<CreatedInviteEnvelope>(
    `/teams/${teamId}/invites`,
    { email, role },
  );
  return response.data.invite;
}

/** revokeInvite invalidates a pending invite (owner/admin). */
export async function revokeInvite(
  teamId: string,
  inviteId: string,
): Promise<void> {
  await http.delete(`/teams/${teamId}/invites/${inviteId}`);
}

/**
 * acceptInvite consumes an invite token as the authenticated account whose
 * email it names, and returns the joined team.
 */
export async function acceptInvite(token: string): Promise<Team> {
  const response = await http.post<TeamEnvelope>("/invites/accept", { token });
  return response.data.team;
}

/** roleLabel renders a role as display text. */
export function roleLabel(role: TeamRole): string {
  return role === "read_only" ? "read-only" : role;
}

/** roleTagType maps a role onto a tag style. */
export function roleTagType(
  role: TeamRole,
): "success" | "warning" | "default" {
  switch (role) {
    case "owner":
      return "success";
    case "admin":
      return "warning";
    default:
      return "default";
  }
}

/** canManageMembers reports whether a role may invite, remove and re-role. */
export function canManageMembers(role: TeamRole | null): boolean {
  return role === "owner" || role === "admin";
}

/** isFeatureDisabled reports whether an error is the teams feature 404. */
export function isFeatureDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 404;
}

/**
 * describeTeamError maps a thrown error to a user-facing message. The backend
 * answers 400 for validation, 403 for an insufficient role, 409 for the
 * last-owner / personal-team / non-empty-team protections and 410 for an
 * expired invite, and its message is the actionable part — the "teams: "
 * package prefix is stripped for display.
 */
export function describeTeamError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return "Your session expired. Please sign in again.";
    }
    if (error.status === 403) {
      return stripPrefix(error.message) || "Your team role does not allow this action.";
    }
    if (error.status === 404) {
      return stripPrefix(error.message) || "Not found. It may have been removed already.";
    }
    if (error.status === 409) {
      return (
        stripPrefix(error.message) ||
        "The team changed while you were editing it. Reload and retry."
      );
    }
    if (error.status === 410) {
      return stripPrefix(error.message) || "This invite expired. Issue a new one.";
    }
    if (error.status === 400) {
      return stripPrefix(error.message) || "Invalid request.";
    }
    return error.message || "Request failed";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong. Please try again.";
}

/** stripPrefix drops a domain package prefix from a backend message. */
function stripPrefix(message: string): string {
  return (message ?? "").replace(/^(teams|notifications):\s*/i, "").trim();
}
