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
  created_at: string;
}

/** Token-pair body returned by register, login, and refresh. */
export interface AuthResult {
  user: User;
  access_token: string;
  token_type: string;
  expires_in: number;
  refresh_token: string;
}

/** Persisted session snapshot kept in localStorage. */
export interface Session {
  user: User | null;
  accessToken: string | null;
  refreshToken: string | null;
}

/** localStorage key holding the serialised session. */
const storageKey = "gotham.auth.session";

/** Listeners notified whenever the persisted session changes. */
const listeners = new Set<(session: Session) => void>();

/** subscribeSession registers a listener and returns an unsubscribe function. */
export function subscribeSession(listener: (session: Session) => void): () => void {
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

  try {
    storage.setItem(storageKey, JSON.stringify(session));
  } catch {
    // Ignore quota and private-mode failures: the in-memory store still works.
  }

  notifySession();
}

/** clearSession removes the persisted session. */
export function clearSession(): void {
  getStorage()?.removeItem(storageKey);
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
