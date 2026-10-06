import { defineStore } from "pinia";
import { computed, ref } from "vue";

import type { ApiError } from "@/shared/api/http";
import { http } from "@/shared/api/http";
import { stripErrorPrefix } from "@/features/servers";
import { activeLocale } from "@/shared/i18n/locale";
import enCatalog from "@/shared/i18n/locales/en";
import viCatalog from "@/shared/i18n/locales/vi";
import {
  clearSession as clearStoredSession,
  getSession,
  setSession as persistSession,
  subscribeSession,
  type AuthResult,
  type Session,
  type User,
} from "@/shared/api/token";
import { useApplicationsStore } from "@/features/applications";
import { useBackupsStore } from "@/features/databases";
import { useDatabasesStore } from "@/features/databases";
import { useNotificationsStore } from "@/features/notifications";
import { useProjectsStore } from "@/features/projects";
import { useProvidersStore } from "@/features/applications";
import { useProxyStore } from "@/features/domains";
import { useServersStore } from "@/features/servers";
import { useServicesStore } from "@/features/services";
import { useTeamsStore } from "@/features/teams";
import { useTemplatesStore } from "@/features/templates";

export type { AuthResult, User } from "@/shared/api/token";

/** Input accepted by {@link useAuthStore.setSession}. */
export interface SessionInput {
  user?: User | null;
  access_token: string;
  refresh_token: string;
}

export const useAuthStore = defineStore("auth", () => {
  const initial: Session = getSession();

  const user = ref<User | null>(initial.user);
  const accessToken = ref<string | null>(initial.accessToken);
  const refreshToken = ref<string | null>(initial.refreshToken);

  /**
   * userSeq orders local account writes (setSession, setUser, clearSession).
   * fetchMe captures it at start and discards a response that resolves after
   * a newer local write, so a slow read cannot overwrite it. Reads that land
   * through applySession (a token rotation or another tab) do not bump it:
   * they carry server facts, never a newer local write.
   */
  let userSeq = 0;

  /**
   * registrationOpen mirrors `GET /auth/config`: true only on a fresh instance
   * with zero accounts. Afterwards the create-account tab is hidden and
   * members join through an admin-created invite link (P-A2).
   */
  const registrationOpen = ref(false);
  /** configLoaded caches /auth/config for the session (fetchAuthConfig once). */
  let configLoaded = false;

  const isAuthenticated = computed<boolean>(() => accessToken.value !== null);

  /** applySession replaces the in-memory state from a stored snapshot. */
  function applySession(session: Session): void {
    user.value = session.user;
    accessToken.value = session.accessToken;
    refreshToken.value = session.refreshToken;
  }

  // The HTTP layer rotates tokens directly through the token module; mirror
  // those changes here so the store never writes a stale token back. When the
  // shared session is emptied elsewhere (forced logout on 401, cross-tab
  // sign-out) the user-scoped caches are dropped too: the redirect stays
  // in-app now, so no reload clears them anymore.
  subscribeSession((session) => {
    applySession(session);
    if (session.accessToken === null && session.refreshToken === null) {
      resetUserStores();
    }
  });

  /** persist writes the current state back to the shared token module. */
  function persist(): void {
    persistSession({
      user: user.value,
      accessToken: accessToken.value,
      refreshToken: refreshToken.value,
    });
  }

  /** setSession stores the token pair (and user, when provided). */
  function setSession(input: SessionInput): void {
    user.value = input.user ?? null;
    accessToken.value = input.access_token;
    refreshToken.value = input.refresh_token;
    userSeq += 1;
    persist();
  }

  /**
   * setUser replaces the stored account (a display-name save) and persists
   * it, so the sidebar follows without a reload. It bumps the write order so
   * an in-flight fetchMe cannot overwrite it with an older server read.
   */
  function setUser(next: User | null): void {
    user.value = next;
    userSeq += 1;
    persist();
  }

  /**
   * clearSession forgets the session in memory and in localStorage, and
   * drops every user-scoped Pinia cache (teams, servers, applications,
   * databases, notifications, services, backups, providers, templates,
   * proxy) so the next sign-in cannot render the previous account's data —
   * notably the sidebar role, which the teams store would otherwise keep
   * serving from its loaded cache.
   */
  function clearSession(): void {
    // The instance's registration policy does not change on sign-out; keep the
    // cached config unless a caller forces a refresh.
    user.value = null;
    accessToken.value = null;
    refreshToken.value = null;
    userSeq += 1;
    clearStoredSession();
    resetUserStores();
  }

  /**
   * resetUserStores clears every user-scoped store. Each call is guarded so
   * a store that was never instantiated (or whose reset throws) cannot break
   * sign-out; Pinia creates the store on first use, at which point there is
   * nothing stale to clear.
   */
  function resetUserStores(): void {
    const resetters: Array<() => void> = [
      () => useTeamsStore().reset(),
      () => useServersStore().reset(),
      () => useApplicationsStore().reset(),
      () => useDatabasesStore().reset(),
      () => useNotificationsStore().reset(),
      () => useProjectsStore().reset(),
      () => useServicesStore().reset(),
      () => useBackupsStore().reset(),
      () => useProvidersStore().reset(),
      () => useTemplatesStore().reset(),
      () => useProxyStore().reset(),
    ];
    for (const resetStore of resetters) {
      try {
        resetStore();
      } catch {
        // Sign-out must always complete; a store reset never blocks it.
      }
    }
  }

  /**
   * fetchMe refreshes the account from GET /auth/me, clearing on 401. A
   * response that resolves after the account changed is discarded so it
   * cannot overwrite it: a different account id (an OAuth exchange or another
   * sign-in installed a new session, or sign-out cleared it), or a newer
   * local write (a display-name save, a password change). A token rotation
   * that lands mid-flight changes the token but not the account, so the
   * retried read after a 401 still applies — that is the first open with an
   * expired access token.
   */
  async function fetchMe(): Promise<void> {
    const tokenAtStart = accessToken.value;
    const idAtStart = user.value?.id;
    const seqAtStart = userSeq;
    try {
      const response = await http.get<{ user: User }>("/auth/me");
      if (user.value?.id !== idAtStart || userSeq !== seqAtStart) {
        return;
      }
      user.value = response.data.user;
      persist();
    } catch (error) {
      if (accessToken.value !== tokenAtStart) {
        return;
      }
      if (isUnauthorized(error)) {
        clearSession();
      }
      throw error;
    }
  }

  /** login exchanges credentials for a session. */
  async function login(email: string, password: string): Promise<void> {
    const response = await http.post<AuthResult>("/auth/login", {
      email,
      password,
    });
    setSession(response.data);
  }

  /**
   * fetchAuthConfig refreshes `registrationOpen`. A failure leaves it false
   * (closed) — the safe default for an unknown instance state.
   */
  async function fetchAuthConfig(force = false): Promise<void> {
    if (configLoaded && !force) {
      return;
    }
    try {
      const response = await http.get<{ registrationOpen: boolean }>("/auth/config");
      registrationOpen.value = response.data.registrationOpen === true;
      configLoaded = true;
    } catch {
      // Leave the safe default (closed) and allow a later retry.
      registrationOpen.value = false;
    }
  }

  /**
   * validateInvite resolves an invite token to the team the invitee is
   * joining. A missing or unusable token throws, so the caller can fall back
   * to the sign-in form.
   */
  async function validateInvite(token: string): Promise<{ team: string; email: string }> {
    const response = await http.get<{ team: string; email: string }>(
      "/auth/invites/validate",
      { params: { token } },
    );
    return response.data;
  }

  /** register creates an account and starts the session. */
  async function register(
    email: string,
    password: string,
    inviteToken?: string,
  ): Promise<void> {
    const response = await http.post<AuthResult>("/auth/register", {
      email,
      password,
      ...(inviteToken ? { inviteToken } : {}),
    });
    setSession(response.data);
    // The instance now has an account (unless the test/dev override is on), so
    // the cached policy is stale: the next auth render re-probes /auth/config
    // instead of offering a create-account tab that would answer 403.
    configLoaded = false;
  }

  /** logout revokes the refresh token, then clears the session. */
  async function logout(): Promise<void> {
    const token = refreshToken.value ?? getSession().refreshToken;
    try {
      if (token) {
        await http.post("/auth/logout", { refresh_token: token });
      }
    } finally {
      clearSession();
    }
  }

  return {
    user,
    accessToken,
    refreshToken,
    isAuthenticated,
    registrationOpen,
    setSession,
    setUser,
    clearSession,
    persist,
    fetchMe,
    fetchAuthConfig,
    validateInvite,
    login,
    register,
    logout,
  };
});

/** isUnauthorized reports whether an error is a 401 ApiError. */
export function isUnauthorized(error: unknown): boolean {
  return getStatus(error) === 401;
}

/**
 * describeAuthError maps a thrown API error to a user-facing message. Known
 * rate-limit refusals and the generic fallback resolve from the shared
 * common catalog for the active locale at call time (so a computed banner
 * refreshes on language switch); any other message keeps the raw
 * server text with only the backend prefix stripped, never translated, so
 * classification and diagnostics stay language-independent.
 */
export function describeAuthError(error: unknown): string {
  const status = getStatus(error);
  if (status === 429) {
    return commonErrors().rateLimited;
  }

  const message =
    typeof error === "object" && error !== null
      ? (error as Partial<ApiError>).message
      : undefined;
  if (typeof message === "string" && message.trim() !== "") {
    return stripErrorPrefix(message);
  }
  return commonErrors().unexpected;
}

/** commonErrors reads the shared error summaries for the active locale. */
function commonErrors(): { rateLimited: string; unexpected: string } {
  return (activeLocale.value === "vi" ? viCatalog : enCatalog).common.errors;
}

/** getStatus extracts the HTTP status from a thrown ApiError, if present. */
function getStatus(error: unknown): number | null {
  if (typeof error !== "object" || error === null) {
    return null;
  }
  const status = (error as Partial<ApiError>).status;
  return typeof status === "number" ? status : null;
}
