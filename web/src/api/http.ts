import axios from "axios";
import type {
  AxiosError,
  AxiosInstance,
  AxiosResponse,
  InternalAxiosRequestConfig,
} from "axios";

import { clearSession, getAccessToken, getRefreshToken, setSession } from "./token";
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
      // Set once the refresh succeeds; used to tell whether a newer session
      // replaced this one between the refresh and a failed retry.
      let tokenBeforeRetry: string | null | undefined;
      try {
        const accessToken = await refreshAccessToken();
        config.headers.set("Authorization", `Bearer ${accessToken}`);
        tokenBeforeRetry = getRefreshToken();
        return await http.request(config);
      } catch (refreshError) {
        // A stale refresh, or a retry that failed after a newer session
        // replaced this one, must not clear the newer session.
        const stale = refreshError instanceof StaleRefreshError;
        const replacedAfterRefresh =
          tokenBeforeRetry !== undefined &&
          getRefreshToken() !== tokenBeforeRetry;
        if (!stale && !replacedAfterRefresh) {
          expireSession();
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

/** refreshAccessToken rotates the refresh token, reusing one shared request. */
function refreshAccessToken(): Promise<string> {
  if (refreshPromise) {
    return refreshPromise;
  }

  const refreshToken = getRefreshToken();
  if (!refreshToken) {
    return Promise.reject(new Error("no refresh token available"));
  }

  refreshPromise = axios
    .post<AuthResult>(
      refreshPath,
      { refresh_token: refreshToken },
      { timeout: requestTimeout, headers: { Accept: "application/json" } },
    )
    .then((response) => {
      // A newer session landed while this refresh was in flight: keep it rather
      // than overwriting it with the stale rotation.
      if (getRefreshToken() !== refreshToken) {
        throw new StaleRefreshError();
      }
      setSession({
        user: response.data.user ?? null,
        accessToken: response.data.access_token,
        refreshToken: response.data.refresh_token,
      });
      return response.data.access_token;
    })
    .catch((error) => {
      // A failed refresh for a session that is already gone must not clear the
      // newer session.
      if (getRefreshToken() !== refreshToken) {
        throw new StaleRefreshError();
      }
      throw error;
    })
    .finally(() => {
      refreshPromise = null;
    });

  return refreshPromise;
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
 * expireSession drops the stored session and sends the browser to the login
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
