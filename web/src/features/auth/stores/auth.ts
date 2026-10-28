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
import { useApplicationsStore, useGitHubAppStore, useProvidersStore } from "@/features/applications";
import { useRecentDeploymentsStore } from "@/features/dashboard";
import { useBackupsStore } from "@/features/databases";
import { useDatabasesStore } from "@/features/databases";
import { useFilesStore } from "@/features/files";
import { useEditorStore } from "@/features/files/stores/editor";
import { useEditorWindowStore } from "@/features/files/stores/editorWindow";
import { useExplorerStore } from "@/features/files/stores/explorer";
import {
  FILES_PANE_IDS,
  useFilesPaneSession,
} from "@/features/files/stores/paneSession";
import { useTrashStore } from "@/features/files/stores/trash";
import { useIncidentsStore } from "@/features/incidents";
import {
  useNotificationInboxStore,
  useNotificationsStore,
} from "@/features/notifications";
import { useProjectsStore } from "@/features/projects";
import { useProxyStore } from "@/features/domains";
import { useServersStore } from "@/features/servers";
import { useServicesStore } from "@/features/services";
import { useTemplatesStore } from "@/features/templates";
import { useTerminalStore, useTerminalWindowStore } from "@/features/terminal";
import { useToolchainStore } from "@/features/toolchain";

export type { AuthResult, User } from "@/shared/api/token";

const MFA_CHALLENGE_TTL_MS = 5 * 60 * 1000;

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

  const mfaToken = ref<string | null>(null);
  const mfaExpiresAt = ref<number | null>(null);
  const mfaPending = computed<boolean>(() => mfaToken.value !== null);

  let userSeq = 0;

  const registrationOpen = ref(false);
  let configLoaded = false;

  const isAuthenticated = computed<boolean>(() => accessToken.value !== null);
  const isOwner = computed<boolean>(() => user.value?.role === "owner");

  function applySession(session: Session): void {
    user.value = session.user;
    accessToken.value = session.accessToken;
    refreshToken.value = session.refreshToken;
  }

  let lastAccountId = initial.user?.id ?? "";
  subscribeSession((session) => {
    applySession(session);
    const nextAccountId = session.user?.id ?? "";
    if (nextAccountId !== lastAccountId) {
      lastAccountId = nextAccountId;
      if (nextAccountId !== "") {
        try {
          useTerminalStore().reset();
          useEditorWindowStore().reloadForUser(nextAccountId);
          useFilesPaneSession().reloadForUser(nextAccountId);
          useTerminalWindowStore().reloadForUser(nextAccountId);
        } catch {
        }
      }
    }
    if (session.accessToken === null && session.refreshToken === null) {
      resetUserStores();
    }
  });

  function persist(): void {
    persistSession({
      user: user.value,
      accessToken: accessToken.value,
      refreshToken: refreshToken.value,
    });
  }

  function setSession(input: SessionInput): void {
    user.value = input.user ?? null;
    accessToken.value = input.access_token;
    refreshToken.value = input.refresh_token;
    userSeq += 1;
    persist();
  }

  function setUser(next: User | null): void {
    user.value = next;
    userSeq += 1;
    persist();
  }

  function clearSession(): void {
    user.value = null;
    accessToken.value = null;
    refreshToken.value = null;
    clearMfaChallenge();
    userSeq += 1;
    clearStoredSession();
    resetUserStores();
  }

  function resetUserStores(): void {
    const resetters: Array<() => void> = [
      () => useServersStore().reset(),
      () => useApplicationsStore().reset(),
      () => useDatabasesStore().reset(),
      () => {
        for (const pane of FILES_PANE_IDS) {
          useFilesStore(pane).reset();
        }
        useFilesPaneSession().reset();
      },
      () => {
        for (const pane of FILES_PANE_IDS) {
          useEditorStore(pane).reset();
        }
      },
      () => useEditorWindowStore().reset(),
      () => useTerminalStore().reset(),
      () => useTerminalWindowStore().reset(),
      () => {
        for (const pane of FILES_PANE_IDS) {
          useTrashStore(pane).reset();
          useExplorerStore(pane).reset();
        }
      },
      () => useNotificationsStore().reset(),
      () => useNotificationInboxStore().reset(),
      () => useIncidentsStore().reset(),
      () => useProjectsStore().reset(),
      () => useServicesStore().reset(),
      () => useBackupsStore().reset(),
      () => useProvidersStore().reset(),
      () => useGitHubAppStore().reset(),
      () => useTemplatesStore().reset(),
      () => useToolchainStore().reset(),
      () => useProxyStore().reset(),
      () => useRecentDeploymentsStore().reset(),
    ];
    for (const resetStore of resetters) {
      try {
        resetStore();
      } catch {
      }
    }
  }

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

  async function login(
    email: string,
    password: string,
  ): Promise<{ mfaRequired: boolean }> {
    const response = await http.post<AuthResult>("/auth/login", {
      email,
      password,
    });
    if (response.data.mfa_required) {
      setMfaChallenge(response.data.mfa_token ?? null);
      return { mfaRequired: true };
    }
    clearMfaChallenge();
    setSession(response.data);
    return { mfaRequired: false };
  }

  function setMfaChallenge(token: string | null): void {
    mfaToken.value = token;
    mfaExpiresAt.value = token === null ? null : Date.now() + MFA_CHALLENGE_TTL_MS;
  }

  function clearMfaChallenge(): void {
    mfaToken.value = null;
    mfaExpiresAt.value = null;
  }

  function isMfaChallengeLive(): boolean {
    return (
      mfaToken.value !== null &&
      mfaExpiresAt.value !== null &&
      Date.now() < mfaExpiresAt.value
    );
  }

  async function completeMfaLogin(code: string): Promise<void> {
    const token = mfaToken.value;
    if (token === null) {
      throw new Error("auth: no pending two-factor challenge");
    }
    const response = await http.post<AuthResult>("/auth/login/totp", {
      mfa_token: token,
      code,
    });
    clearMfaChallenge();
    setSession(response.data);
  }

  async function fetchAuthConfig(force = false): Promise<void> {
    if (configLoaded && !force) {
      return;
    }
    try {
      const response = await http.get<{ registrationOpen: boolean }>("/auth/config");
      registrationOpen.value = response.data.registrationOpen === true;
      configLoaded = true;
    } catch {
      registrationOpen.value = false;
    }
  }

  async function validateInvite(token: string): Promise<{ email: string }> {
    const response = await http.get<{ email: string }>(
      "/auth/invites/validate",
      { params: { token } },
    );
    return response.data;
  }

  async function register(
    email: string,
    password: string,
    inviteToken?: string,
  ): Promise<void> {
    const response = await http.post<AuthResult>("/auth/register", {
      email,
      password,
      ...(inviteToken ? { invite_token: inviteToken } : {}),
    });
    setSession(response.data);
    configLoaded = false;
  }

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
    isOwner,
    registrationOpen,
    mfaPending,
    setSession,
    setUser,
    clearSession,
    persist,
    fetchMe,
    fetchAuthConfig,
    validateInvite,
    login,
    setMfaChallenge,
    clearMfaChallenge,
    isMfaChallengeLive,
    completeMfaLogin,
    register,
    logout,
  };
});

export function isUnauthorized(error: unknown): boolean {
  return getStatus(error) === 401;
}

export function isRateLimited(error: unknown): boolean {
  return getStatus(error) === 429;
}

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
    return `${commonErrors().requestFailed}: ${stripErrorPrefix(message)}`;
  }
  return commonErrors().unexpected;
}

function commonErrors(): { rateLimited: string; unexpected: string; requestFailed: string } {
  return (activeLocale.value === "vi" ? viCatalog : enCatalog).common.errors;
}

function getStatus(error: unknown): number | null {
  if (typeof error !== "object" || error === null) {
    return null;
  }
  const status = (error as Partial<ApiError>).status;
  return typeof status === "number" ? status : null;
}
