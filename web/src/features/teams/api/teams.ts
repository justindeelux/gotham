import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";

export type TeamRole = "owner" | "admin" | "read_only";

export interface Team {
  id: string;
  name: string;
  is_personal: boolean;
  role: TeamRole;
  created_at: string;
  updated_at: string;
}

export interface TeamMember {
  user_id: string;
  email: string;
  role: TeamRole;
  created_at: string;
}

export interface TeamInvite {
  id: string;
  team_id: string;
  email: string;
  role: TeamRole;
  expires_at: string;
  accepted_at?: string;
  created_at: string;
}

export interface CreatedTeamInvite extends TeamInvite {
  token: string;
  accept_url: string;
}

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

export async function listTeams(): Promise<Team[]> {
  const response = await http.get<TeamListEnvelope>("/teams");
  return response.data.teams ?? [];
}

export async function createTeam(name: string): Promise<Team> {
  const response = await http.post<TeamEnvelope>("/teams", { name });
  return response.data.team;
}

export async function renameTeam(id: string, name: string): Promise<Team> {
  const response = await http.patch<TeamEnvelope>(`/teams/${id}`, { name });
  return response.data.team;
}

export async function deleteTeam(id: string): Promise<void> {
  await http.delete(`/teams/${id}`);
}

export async function listMembers(teamId: string): Promise<TeamMember[]> {
  const response = await http.get<MemberListEnvelope>(
    `/teams/${teamId}/members`,
  );
  return response.data.members ?? [];
}

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

export async function removeMember(
  teamId: string,
  userId: string,
): Promise<void> {
  await http.delete(`/teams/${teamId}/members/${userId}`);
}

export async function listInvites(teamId: string): Promise<TeamInvite[]> {
  const response = await http.get<InviteListEnvelope>(
    `/teams/${teamId}/invites`,
  );
  return response.data.invites ?? [];
}

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

export async function revokeInvite(
  teamId: string,
  inviteId: string,
): Promise<void> {
  await http.delete(`/teams/${teamId}/invites/${inviteId}`);
}

export async function acceptInvite(token: string): Promise<Team> {
  const response = await http.post<TeamEnvelope>("/invites/accept", { token });
  return response.data.team;
}

export type TextParams = Record<string, string | number>;

export function teamText(
  key: string,
  fallback: string,
  params?: TextParams,
): string {
  const composer = i18n.global;
  if (composer.te(key)) {
    return String(composer.t(key, params ?? {}));
  }
  let out = fallback;
  for (const [name, value] of Object.entries(params ?? {})) {
    out = out.replaceAll(`{${name}}`, String(value));
  }
  return out;
}

export function roleLabel(role: TeamRole): string {
  const key = roleKey(role);
  if (key !== null) {
    return teamText(key, englishRoleLabel(role));
  }
  if (role === null || role === undefined) {
    return teamText("common.roles.member", "Team member");
  }
  return role;
}

function roleKey(role: TeamRole): string | null {
  switch (role) {
    case "owner":
      return "common.roles.owner";
    case "admin":
      return "common.roles.admin";
    case "read_only":
      return "common.roles.readOnly";
    default:
      return null;
  }
}

function englishRoleLabel(role: TeamRole): string {
  switch (role) {
    case "owner":
      return "owner";
    case "admin":
      return "admin";
    default:
      return "read-only";
  }
}

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

export function meRoleLabel(role: TeamRole | null): string {
  return role === null
    ? teamText("common.roles.member", "Team member")
    : roleLabel(role);
}

export const roleReadMaxRetries = 2;

export const roleReadRetryMs = 5_000;

export interface RoleReadStatus {
  loaded: boolean;
  loading: boolean;
  retries: number;
}

export function shouldRetryRoleRead(status: RoleReadStatus): boolean {
  if (status.loaded || status.loading) {
    return false;
  }
  return status.retries < roleReadMaxRetries;
}

export function canManageMembers(role: TeamRole | null): boolean {
  return role === "owner" || role === "admin";
}

export function isFeatureDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 404;
}

export function describeTeamError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return teamText(
        "teams.errors.sessionExpired",
        "Your session expired. Please sign in again.",
      );
    }
    if (error.status === 403) {
      return (
        stripErrorPrefix(error.message) ||
        teamText(
          "teams.errors.forbiddenRole",
          "Your team role does not allow this action.",
        )
      );
    }
    if (error.status === 404) {
      return (
        stripErrorPrefix(error.message) ||
        teamText(
          "teams.errors.notFound",
          "Not found. It may have been removed already.",
        )
      );
    }
    if (error.status === 409) {
      return (
        stripErrorPrefix(error.message) ||
        teamText(
          "teams.errors.conflictChanged",
          "The team changed while you were editing it. Reload and retry.",
        )
      );
    }
    if (error.status === 410) {
      return (
        stripErrorPrefix(error.message) ||
        teamText(
          "teams.errors.inviteExpired",
          "This invite expired. Issue a new one.",
        )
      );
    }
    if (error.status === 400) {
      return (
        stripErrorPrefix(error.message) ||
        teamText("teams.errors.invalidRequest", "Invalid request.")
      );
    }
    return withTeamStatusDiagnostic(stripErrorPrefix(error.message));
  }
  if (error instanceof Error) {
    return withTeamDiagnostic(stripErrorPrefix(error.message));
  }
  return teamText(
    "teams.errors.unexpected",
    "Something went wrong. Please try again.",
  );
}

function withTeamStatusDiagnostic(raw: string): string {
  const summary = teamText("teams.errors.requestFailed", "Request failed");
  if (raw === "" || raw === summary) {
    return summary;
  }
  const lead = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return `${lead}: ${raw}`;
}

function withTeamDiagnostic(raw: string): string {
  const summary = teamText(
    "teams.errors.unexpected",
    "Something went wrong. Please try again.",
  );
  if (raw === "" || raw === summary) {
    return summary;
  }
  const lead = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return `${lead}: ${raw}`;
}

