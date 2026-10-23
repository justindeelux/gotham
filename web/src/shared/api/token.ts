
export interface User {
  id: string;
  email: string;
  avatar?: string;
  role?: string;
  created_at: string;
  display_name?: string;
  has_password?: boolean;
  is_platform_admin?: boolean;
}

export interface AuthResult {
  user: User;
  access_token: string;
  token_type: string;
  expires_in: number;
  refresh_token: string;
  mfa_required?: boolean;
  mfa_token?: string;
}

export interface Session {
  user: User | null;
  accessToken: string | null;
  refreshToken: string | null;
}

const storageKey = "gotham.auth.session";

export const activeTeamStorageKey = "gotham.teams.active";

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

export const editorDraftStorageKeyPrefix = "gotham.files.draft.v1.";

const userScopedStorageKeys = [activeTeamStorageKey];

const listeners = new Set<(_session: Session) => void>();

export function subscribeSession(listener: (_session: Session) => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

function notifySession(): void {
  const session = getSession();
  for (const listener of listeners) {
    listener(session);
  }
}

if (typeof window !== "undefined") {
  window.addEventListener("storage", (event) => {
    if (event.key === storageKey || event.key === null) {
      notifySession();
    }
  });
}

function getStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

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

export function setSession(session: Session): void {
  const storage = getStorage();
  if (!storage) {
    return;
  }
  const previousUserId = getSession().user?.id ?? "";
  const nextUserId = session.user?.id ?? "";

  try {
    storage.setItem(storageKey, JSON.stringify(session));
  } catch {
  }

  if (previousUserId !== nextUserId && nextUserId !== "") {
    clearStoragePrefix(storage, editorDraftStorageKeyPrefix);
  }

  notifySession();
}

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
  }
}

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

export function getAccessToken(): string | null {
  return getSession().accessToken;
}

export function getRefreshToken(): string | null {
  return getSession().refreshToken;
}
