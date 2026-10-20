/**
 * Framework-free session storage for the Gotham SPA.
 *
 * Both the Pinia auth store and the axios interceptors read and write the
 * token pair through this module, so neither has to import the other (which
 * would introduce a circular dependency between the HTTP layer and the store).
 */

/** Authenticated account as returned by the control-plane API. */
export interface User {
  id: string;
  email: string;
  avatar?: string;
  /** Platform role ("admin" for the first account, "user" otherwise). Optional: older control planes omit it. */
  role?: string;
  created_at: string;
  /** Optional display name shown in the sidebar instead of the email. Omitted when unset. */
  display_name?: string;
  /** Whether the account has a password set (false for OAuth-created accounts). Optional: older control planes omit it. */
  has_password?: boolean;
  /** Whether the account is a platform admin. Optional: older control planes omit it. */
  is_platform_admin?: boolean;
}

/** Token-pair body returned by register, login, and refresh. */
export interface AuthResult {
  user: User;
  access_token: string;
  token_type: string;
  expires_in: number;
  refresh_token: string;
  /**
   * Set by a login (password or OAuth) whose account has TOTP enabled: no
   * token pair is issued and `mfa_token` must be redeemed with a code through
   * POST /auth/login/totp. Optional: older control planes omit it.
   */
  mfa_required?: boolean;
  /** Half-authenticated token for the second login step. Omitted otherwise. */
  mfa_token?: string;
}

/** Persisted session snapshot kept in localStorage. */
export interface Session {
  user: User | null;
  accessToken: string | null;
  refreshToken: string | null;
}

/** localStorage key holding the serialised session. */
const storageKey = "gotham.auth.session";

/**
 * activeTeamStorageKey persists the teams store's active-team selection. It
 * lives in this framework-free module (rather than the teams store) so the
 * session layer owns every persisted user-scoped key and can clear them
 * together; the store imports it from here.
 */
export const activeTeamStorageKey = "gotham.teams.active";

/**
 * getActiveTeamId returns the persisted active-team selection, or "" when the
 * caller is on their personal team. The HTTP layer reads it on every
 * team-scoped request (see ./http), so a resource route does not depend on
 * each caller threading the id through — the teams store remains the only
 * writer of the key.
 */
export function getActiveTeamId(): string {
  const storage = getStorage();
  if (!storage) {
    return "";
  }
  try {
    return storage.getItem(activeTeamStorageKey) ?? "";
  } catch {
    return "";
  }
}

/**
 * editorDraftStorageKeyPrefix scopes the code editor's unsaved drafts. Drafts
 * hold plaintext file bodies (which may include secrets), so every key under
 * this prefix is purged on sign-out and on an account switch instead of being
 * left readable in a shared browser.
 */
export const editorDraftStorageKeyPrefix = "gotham.files.draft.v1.";

/**
 * userScopedStorageKeys holds every persisted user-scoped key beyond the
 * session itself. The cross-tab refresh lease (see ./http) is deliberately
 * absent: it coordinates tabs, not users, and must survive a sign-out.
 */
const userScopedStorageKeys = [activeTeamStorageKey];

/** Listeners notified whenever the persisted session changes. */
const listeners = new Set<(_session: Session) => void>();

/** subscribeSession registers a listener and returns an unsubscribe function. */
export function subscribeSession(listener: (_session: Session) => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** notifySession informs listeners of the current session. */
function notifySession(): void {
  const session = getSession();
  for (const listener of listeners) {
    listener(session);
  }
}

// Other tabs share localStorage. Mirror their session changes so this tab's
// store follows external rotations and sign-outs instead of writing a stale
// token back. A null key means storage.clear() ran.
if (typeof window !== "undefined") {
  window.addEventListener("storage", (event) => {
    if (event.key === storageKey || event.key === null) {
      notifySession();
    }
  });
}

/** getStorage returns localStorage when it is available, or null otherwise. */
function getStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

/** getSession reads the persisted session, falling back to an empty one. */
export function getSession(): Session {
  const storage = getStorage();
  if (!storage) {
    return { user: null, accessToken: null, refreshToken: null };
  }

  const raw = storage.getItem(storageKey);
  if (!raw) {
    return { user: null, accessToken: null, refreshToken: null };
  }

  try {
    const parsed = JSON.parse(raw) as Partial<Session>;
    return {
      user: parsed.user ?? null,
      accessToken: parsed.accessToken ?? null,
      refreshToken: parsed.refreshToken ?? null,
    };
  } catch {
    return { user: null, accessToken: null, refreshToken: null };
  }
}

/** setSession persists a session snapshot. */
export function setSession(session: Session): void {
  const storage = getStorage();
  if (!storage) {
    return;
  }
  // A different account on the same browser must not inherit the previous
  // account's editor drafts (they contain file bodies). Read the old snapshot
  // before overwriting it.
  const previousUserId = getSession().user?.id ?? "";
  const nextUserId = session.user?.id ?? "";

  try {
    storage.setItem(storageKey, JSON.stringify(session));
  } catch {
    // Ignore quota and private-mode failures: the in-memory store still works.
  }

  if (previousUserId !== nextUserId && nextUserId !== "") {
    clearStoragePrefix(storage, editorDraftStorageKeyPrefix);
  }

  notifySession();
}

/** clearStoragePrefix removes every key under one namespace, never throwing. */
function clearStoragePrefix(storage: Storage, prefix: string): void {
  try {
    const keys: string[] = [];
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index);
      if (key !== null && key.startsWith(prefix)) {
        keys.push(key);
      }
    }
    for (const key of keys) {
      storage.removeItem(key);
    }
  } catch {
    // Blocked storage: sign-out and the page still work.
  }
}

/**
 * clearSession removes the persisted session and every other persisted
 * user-scoped key (the teams store's active-team selection and every editor
 * draft). It is the single exit path for a dead session: the auth store and
 * expireSession (the forced logout on 401/refresh failure, which never
 * reaches the Pinia stores because it only reloads to /login) both funnel
 * through here, so the next sign-in cannot inherit the previous user's
 * persisted selection or read their unsaved file bodies.
 */
export function clearSession(): void {
  const storage = getStorage();
  storage?.removeItem(storageKey);
  for (const key of userScopedStorageKeys) {
    storage?.removeItem(key);
  }
  if (storage) {
    clearStoragePrefix(storage, editorDraftStorageKeyPrefix);
  }
  notifySession();
}

/** getAccessToken returns the persisted access token, if any. */
export function getAccessToken(): string | null {
  return getSession().accessToken;
}

/** getRefreshToken returns the persisted refresh token, if any. */
export function getRefreshToken(): string | null {
  return getSession().refreshToken;
}
