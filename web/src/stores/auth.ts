import { defineStore } from "pinia";
import { computed, ref } from "vue";

import type { ApiError } from "../api/http";
import { http } from "../api/http";
import {
  clearSession as clearStoredSession,
  getSession,
  setSession as persistSession,
  subscribeSession,
  type AuthResult,
  type Session,
  type User,
} from "../api/token";

export type { AuthResult, User } from "../api/token";

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
  // those changes here so the store never writes a stale token back.
  subscribeSession(applySession);

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
    persist();
  }

  /** clearSession forgets the session in memory and in localStorage. */
  function clearSession(): void {
    // The instance's registration policy does not change on sign-out; keep the
    // cached config unless a caller forces a refresh.
    user.value = null;
    accessToken.value = null;
    refreshToken.value = null;
    clearStoredSession();
  }

  /** fetchMe refreshes the account from GET /auth/me, clearing on 401. */
  async function fetchMe(): Promise<void> {
    try {
      const response = await http.get<{ user: User }>("/auth/me");
      user.value = response.data.user;
      persist();
    } catch (error) {
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

/** describeAuthError maps a thrown API error to a user-facing message. */
export function describeAuthError(error: unknown): string {
  const status = getStatus(error);
  if (status === 429) {
    return "Too many attempts, please wait";
  }

  const message =
    typeof error === "object" && error !== null
      ? (error as Partial<ApiError>).message
      : undefined;
  return message ?? "Something went wrong. Please try again.";
}

/** getStatus extracts the HTTP status from a thrown ApiError, if present. */
function getStatus(error: unknown): number | null {
  if (typeof error !== "object" || error === null) {
    return null;
  }
  const status = (error as Partial<ApiError>).status;
  return typeof status === "number" ? status : null;
}
