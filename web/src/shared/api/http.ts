import axios from "axios";
import type {
  AxiosError,
  AxiosInstance,
  AxiosResponse,
  InternalAxiosRequestConfig,
} from "axios";

import {
  clearSession,
  getAccessToken,
  getRefreshToken,
  getSession,
  setSession,
} from "./token";
import type { AuthResult } from "./token";

/** Axios error payloads returned by the Gotham API. */
interface ApiErrorBody {
  message?: string;
}

/** Normalised failure surfaced to callers of the Gotham API. */
export interface ApiError {
  status: number | null;
  message: string;
  cause: unknown;
}

/** Request config extended with a marker set when a 401 has been retried. */
interface RetryableRequestConfig extends InternalAxiosRequestConfig {
  _retry?: boolean;
}

/** Request timeout in milliseconds. */
const requestTimeout = 15_000;

/** Refresh endpoint, addressed relative to the current origin. */
const refreshPath = "/api/v1/auth/refresh";

/**
 * StaleRefreshError marks a refresh whose session was replaced while it was in
 * flight (for example by the OAuth exchange). Callers must not expire the newer
 * session on it.
 */
class StaleRefreshError extends Error {
  constructor() {
    super("session changed during refresh");
    this.name = "StaleRefreshError";
  }
}

/**
 * isStaleRefreshError reports whether error is a stale-refresh rejection, i.e.
 * a newer session replaced the one a refresh or retry was working on. Callers
 * must not expire the replacement session for it.
 */
export function isStaleRefreshError(error: unknown): boolean {
  return error instanceof StaleRefreshError;
}

/** Endpoints that must never trigger a refresh-and-retry on 401. */
const noRefreshPaths = [
  "/auth/login",
  "/auth/register",
  "/auth/refresh",
  "/auth/oauth/exchange",
];

/** Shared axios instance for the `/api/v1` control-plane API. */
export const http: AxiosInstance = axios.create({
  baseURL: "/api/v1",
  timeout: requestTimeout,
  headers: { Accept: "application/json" },
});

http.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const accessToken = getAccessToken();
  if (accessToken) {
    config.headers.set("Authorization", `Bearer ${accessToken}`);
  } else {
    config.headers.delete("Authorization");
  }
  return config;
});

/** In-flight refresh promise shared by all concurrent 401 responses. */
let refreshPromise: Promise<string> | null = null;

http.interceptors.response.use(
  (response: AxiosResponse) => response,
  async (error: AxiosError<ApiErrorBody>) => {
    const config = error.config as RetryableRequestConfig | undefined;

    if (shouldRefresh(error, config)) {
      config._retry = true;
      // Identity of the session that starts the refresh. A replacement installed
      // at any point before this handler decides to expire must survive.
      const tokenBeforeRefresh = getRefreshToken();
      // Identity after a successful refresh (the rotated token), used to tell
      // whether a replacement landed before a failed retry.
      let tokenBeforeRetry: string | null | undefined;
      try {
        const accessToken = await refreshAccessToken();
        config.headers.set("Authorization", `Bearer ${accessToken}`);
        tokenBeforeRetry = getRefreshToken();
        return await http.request(config);
      } catch (refreshError) {
        if (!(refreshError instanceof StaleRefreshError)) {
          // The refresh (or a retry after it) failed. Expire only if the
          // session that started it is still current: a replacement installed
          // meanwhile is preserved. After a successful refresh the rotated
          // token is that session's identity, otherwise the pre-refresh one is.
          const sessionAtFailure =
            tokenBeforeRetry !== undefined
              ? tokenBeforeRetry
              : tokenBeforeRefresh;
          if (getRefreshToken() === sessionAtFailure) {
            expireSession();
          }
        }
      }
    }

    return Promise.reject(toApiError(error));
  },
);

/** shouldRefresh reports whether a 401 warrants a single refresh-and-retry. */
function shouldRefresh(
  error: AxiosError<ApiErrorBody>,
  config: RetryableRequestConfig | undefined,
): config is RetryableRequestConfig {
  if (!config || config._retry || error.response?.status !== 401) {
    return false;
  }

  const url = config.url ?? "";
  return !noRefreshPaths.some((path) => url.includes(path));
}

/** Web Lock name serialising refresh across tabs. */
const refreshLockName = "gotham-refresh";

/** localStorage key holding the best-effort refresh lease (insecure contexts). */
const refreshLeaseKey = "gotham-refresh-lock";

/**
 * Lease lifetime in ms. It is longer than the refresh request timeout so a slow
 * refresh does not let another tab acquire the lease and replay the consumed
 * token; the TTL is the only self-heal for a tab that crashed mid-refresh.
 */
const refreshLeaseTtl = requestTimeout + 5_000;

/** How long the lease fallback waits between acquisition attempts, in ms. */
const refreshLeaseRetryMs = 250;

/**
 * How long to wait after writing the lease before the confirming re-read, so a
 * concurrent cross-process writer can be observed. The write/read pair is still
 * not atomic: this remains best-effort.
 */
const refreshLeaseConfirmMs = 75;

/** sleep resolves after ms milliseconds. */
function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** Best-effort cross-tab refresh lease persisted in localStorage. */
interface RefreshLease {
  id: string;
  expiresAt: number;
}

/** Outcome of a lease acquisition attempt. */
type LeaseAttempt =
  | { status: "acquired"; release: () => void }
  | { status: "held" }
  | { status: "unusable" };

/** getLockStorage returns localStorage when it is available, or null otherwise. */
function getLockStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

/** readRefreshLease returns the unexpired lease held in storage, if any. */
function readRefreshLease(storage: Storage): RefreshLease | null {
  try {
    const raw = storage.getItem(refreshLeaseKey);
    if (!raw) {
      return null;
    }
    const parsed = JSON.parse(raw) as Partial<RefreshLease>;
    if (typeof parsed.id !== "string" || typeof parsed.expiresAt !== "number") {
      return null;
    }
    return parsed.expiresAt > Date.now()
      ? { id: parsed.id, expiresAt: parsed.expiresAt }
      : null;
  } catch {
    return null;
  }
}

/**
 * tryAcquireRefreshLease takes the best-effort lease.
 *
 * The result distinguishes a live foreign lease ("held", so the caller waits)
 * from storage that cannot be written at all ("unusable", so the caller runs
 * unlocked rather than hanging forever). This fallback is weaker than
 * navigator.locks: there is no queue and no crash-safe release, only a TTL, and
 * two simultaneous writers resolve by last writer wins (confirmed by a delayed
 * re-read). It exists so the A2-12 mitigation also applies on plain HTTP, where
 * the Web Locks API is unavailable because it requires a secure context.
 */
async function tryAcquireRefreshLease(): Promise<LeaseAttempt> {
  const storage = getLockStorage();
  if (!storage) {
    return { status: "unusable" };
  }
  if (readRefreshLease(storage)) {
    return { status: "held" };
  }

  // crypto.randomUUID() is secure-context only, so build the id from a
  // timestamp and random suffix available to insecure contexts too.
  const id = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  const lease: RefreshLease = { id, expiresAt: Date.now() + refreshLeaseTtl };
  try {
    storage.setItem(refreshLeaseKey, JSON.stringify(lease));
  } catch {
    // Quota full or a write-blocked profile: a lease cannot be coordinated.
    return { status: "unusable" };
  }

  // Give a concurrent writer time to land, then confirm ownership. The last
  // writer wins; only the id that survived the re-read owns the lease.
  await sleep(refreshLeaseConfirmMs);
  if (readRefreshLease(storage)?.id !== id) {
    return { status: "held" };
  }

  return {
    status: "acquired",
    release: () => {
      if (readRefreshLease(storage)?.id === id) {
        try {
          storage.removeItem(refreshLeaseKey);
        } catch {
          // Ignore private-mode failures; the TTL expires the lease anyway.
        }
      }
    },
  };
}

/** withRefreshLease waits for the lease, then runs task while holding it. */
async function withRefreshLease<T>(task: () => Promise<T>): Promise<T> {
  for (;;) {
    const attempt = await tryAcquireRefreshLease();
    if (attempt.status === "unusable") {
      // Storage cannot coordinate a lease: run unlocked rather than hang.
      return task();
    }
    if (attempt.status === "acquired") {
      try {
        return await task();
      } finally {
        attempt.release();
      }
    }
    await sleep(refreshLeaseRetryMs);
  }
}

/** Lock errors that mean the Web Locks API could not run the task at all. */
const lockEnvironmentErrorNames = new Set([
  "InvalidStateError",
  "SecurityError",
  "NotSupportedError",
]);

/** errorName extracts a DOMException/Error name for environment-error checks. */
function errorName(error: unknown): string {
  return typeof error === "object" && error !== null && "name" in error
    ? String((error as { name: unknown }).name)
    : "";
}

/**
 * withRefreshLock runs task while holding a cross-tab lock, so two tabs cannot
 * present the same single-use refresh token at once. The Web Locks API is the
 * primary path (secure contexts); on plain HTTP it is unavailable, so the
 * best-effort localStorage lease above is used instead. An environment error
 * from the lock call itself (not from the task) also falls back to the lease.
 */
async function withRefreshLock<T>(task: () => Promise<T>): Promise<T> {
  const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
  if (locks) {
    try {
      return (await locks.request(refreshLockName, task)) as T;
    } catch (error) {
      // Only lock-environment failures fall back; a rejected task (a refresh
      // failure) must propagate unchanged.
      if (!lockEnvironmentErrorNames.has(errorName(error))) {
        throw error;
      }
    }
  }
  return withRefreshLease(task);
}

/** refreshAccessToken rotates the refresh token, reusing one shared request. */
function refreshAccessToken(): Promise<string> {
  if (refreshPromise) {
    return refreshPromise;
  }

  const refreshToken = getRefreshToken();
  if (!refreshToken) {
    return Promise.reject(new Error("no refresh token available"));
  }
  // Identity of the account that starts the refresh. A replacement belonging
  // to a different account must never be replayed under.
  const userId = getSession().user?.id ?? null;

  // Per-tab single-flight: concurrent local callers share this promise, so the
  // cross-tab lock is only acquired once per tab.
  refreshPromise = withRefreshLock(() =>
    rotateRefreshToken(refreshToken, userId),
  ).finally(() => {
    refreshPromise = null;
  });

  return refreshPromise;
}

/**
 * rotateRefreshToken consumes refreshToken under the cross-tab lock. It re-reads
 * the stored token first: when another tab already rotated the session while
 * this call waited for the lock, it returns that session's access token instead
 * of replaying the now single-use token. A replacement that switched accounts is
 * treated as stale instead.
 */
async function rotateRefreshToken(
  refreshToken: string,
  userId: string | null,
): Promise<string> {
  if (getRefreshToken() !== refreshToken) {
    const replacementUserId = getSession().user?.id ?? null;
    // A different account's session is not this session's rotation: do not
    // replay the request under the wrong user.
    if (userId && replacementUserId && userId !== replacementUserId) {
      throw new StaleRefreshError();
    }
    // Another tab rotated the session. Re-persist it through this tab's store
    // (idempotent write + notify) so a later persist() here cannot write the
    // old, now-revoked token back over the rotated one.
    setSession(getSession());
    const accessToken = getAccessToken();
    if (!accessToken) {
      throw new StaleRefreshError();
    }
    return accessToken;
  }

  try {
    const response = await axios.post<AuthResult>(
      refreshPath,
      { refresh_token: refreshToken },
      { timeout: requestTimeout, headers: { Accept: "application/json" } },
    );
    // A newer session landed while this refresh was in flight: keep it rather
    // than overwriting it with the stale rotation.
    if (getRefreshToken() !== refreshToken) {
      throw new StaleRefreshError();
    }
    // A non-JSON answer (a static fallback page, a captive portal) must never
    // become the session: without a usable access token the rotation failed
    // and the stored session stays untouched.
    if (typeof response.data?.access_token !== "string" || response.data.access_token === "") {
      throw new Error("refresh did not return an access token");
    }
    setSession({
      user: response.data.user ?? null,
      accessToken: response.data.access_token,
      refreshToken: response.data.refresh_token,
    });
    return response.data.access_token;
  } catch (error) {
    // A failed refresh for a session that is already gone must not clear the
    // newer session.
    if (getRefreshToken() !== refreshToken) {
      throw new StaleRefreshError();
    }
    throw error;
  }
}

/**
 * refreshSession rotates the session through the same single-flight refresh
 * the 401 interceptor uses. It exists for callers that cannot go through the
 * axios instance — the log stream reads a chunked response with `fetch` — so
 * they can honour the same refresh-once contract before surfacing a failure.
 */
export function refreshSession(): Promise<string> {
  return refreshAccessToken();
}

/**
 * expireSession drops the stored session (plus every other persisted
 * user-scoped key, via clearSession) and sends the browser to the login
 * page. It is the single exit path for a dead session, shared by the axios
 * interceptor and the fetch-based log reader (both call it after a failed
 * refresh).
 */
export function expireSession(): void {
  clearSession();
  redirectToLogin();
}

/** redirectToLogin sends the browser to the login page after a dead session. */
function redirectToLogin(): void {
  if (typeof window === "undefined" || window.location.pathname === "/login") {
    return;
  }
  // The SPA router handles the redirect in-app when it is listening: a hard
  // assign reloads the document, which wipes SPA state (a failed logout that
  // already navigates itself must never degrade into a reload). dispatchEvent
  // is synchronous, so a canceled event means the app took over; otherwise
  // fall back to the hard navigation for contexts without the router.
  try {
    const expired = new CustomEvent("gotham:session-expired", {
      cancelable: true,
    });
    if (!window.dispatchEvent(expired)) {
      return;
    }
  } catch {
    // No DOM event support here; fall through to the hard navigation below.
  }
  window.location.assign("/login");
}

/**
 * teamHeaders scopes a request to one team (`X-Team-Id`). An empty id means
 * the caller's personal team, which the control plane resolves without the
 * header.
 */
export function teamHeaders(teamId: string): Record<string, string> {
  return teamId ? { "X-Team-Id": teamId } : {};
}

/** toApiError maps an axios failure to a typed ApiError. */
function toApiError(error: AxiosError<ApiErrorBody>): ApiError {
  const message =
    error.response?.data?.message ?? error.message ?? "Unexpected request error";

  return {
    status: error.response?.status ?? null,
    message,
    cause: error,
  };
}
