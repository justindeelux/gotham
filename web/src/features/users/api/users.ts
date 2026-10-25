import { http } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";
import { parseWith } from "@/shared/validation/parse";

import {
  accountEnvelopeSchema,
  createdInviteEnvelopeSchema,
  inviteListEnvelopeSchema,
  userListEnvelopeSchema,
} from "@/features/users/schemas/users";
import type {
  Account,
  CreatedInvite,
  Invite,
} from "@/features/users/schemas/users";

export async function listUsers(): Promise<Account[]> {
  const response = await http.get<unknown>("/users");
  return parseWith(userListEnvelopeSchema, response.data, {
    context: "UserList",
  }).users;
}

export async function listInvites(): Promise<Invite[]> {
  const response = await http.get<unknown>("/invites");
  return parseWith(inviteListEnvelopeSchema, response.data, {
    context: "InviteList",
  }).invites;
}

export async function inviteUser(
  email: string,
  projectIds: string[],
): Promise<CreatedInvite> {
  const response = await http.post<unknown>("/invites", {
    email: email.trim(),
    project_ids: projectIds,
  });
  return parseWith(createdInviteEnvelopeSchema, response.data, {
    context: "InviteCreate",
  });
}

export async function revokeInvite(id: string): Promise<void> {
  await http.delete(`/invites/${id}`);
}

export async function setUserProjects(
  userId: string,
  projectIds: string[],
): Promise<Account> {
  const response = await http.put<unknown>(`/users/${userId}/projects`, {
    project_ids: projectIds,
  });
  return parseWith(accountEnvelopeSchema, response.data, {
    context: "MemberProjects",
  }).user;
}

export async function removeUser(userId: string): Promise<void> {
  await http.delete(`/users/${userId}`);
}

export async function resetUser(userId: string): Promise<void> {
  await http.post(`/users/${userId}/reset`, {});
}

export function describeUserError(error: unknown): string {
  const text = (key: string): string => String(i18n.global.t(key));
  if (isApiError(error)) {
    if (error.status === 401) {
      return text("users.errors.sessionExpired");
    }
    if (error.status === 403) {
      return stripErrorPrefix(error.message) || text("users.errors.forbidden");
    }
    if (error.status === 404) {
      return stripErrorPrefix(error.message) || text("users.errors.notFound");
    }
    if (error.status === 409) {
      return stripErrorPrefix(error.message) || text("users.errors.conflict");
    }
    return stripErrorPrefix(error.message) || text("common.errors.requestFailed");
  }
  if (error instanceof Error) {
    return stripErrorPrefix(error.message) || text("common.errors.unexpected");
  }
  return text("common.errors.unexpected");
}

export function inviteLink(token: string): string {
  const origin =
    typeof window === "undefined" ? "" : window.location.origin;
  return `${origin}/register?invite=${encodeURIComponent(token)}`;
}
