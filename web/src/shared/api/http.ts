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
  getActiveTeamId,
  getRefreshToken,
  getSession,
  setSession,
} from "./token";
import type { AuthResult } from "./token";

interface ApiErrorBody {
  message?: string;
  code?: string;
}

export interface ApiError {
  status: number | null;
  message: string;
  code?: string;
  cause: unknown;
}

interface RetryableRequestConfig extends InternalAxiosRequestConfig {
  _retry?: boolean;
}

const requestTimeout = 15_000;

const refreshPath = "/api/v1/auth/refresh";

class StaleRefreshError extends Error {
  constructor() {
    super("session changed during refresh");
    this.name = "StaleRefreshError";
  }
}

export function isStaleRefreshError(error: unknown): boolean {
  return error instanceof StaleRefreshError;
}

const noRefreshPaths = [
  "/auth/login",
  "/auth/register",
  "/auth/refresh",
  "/auth/oauth/exchange",
];

export const http: AxiosInstance = axios.create({
  baseURL: "/api/v1",
  timeout: requestTimeout,
  headers: { Accept: "application/json" },
});

const teamScopedPathPrefixes = ["/applications", "/deployments"];

function isTeamScopedPath(url: string | undefined): boolean {
  if (!url) {
    return false;
  }
  const path = url.startsWith("/") ? url : `/${url}`;
  return teamScopedPathPrefixes.some(
    (prefix) =>
      path === prefix ||
      path.startsWith(`${prefix}/`) ||
      path.startsWith(`${prefix}?`),
  );
}

http.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const accessToken = getAccessToken();
  if (accessToken) {
    config.headers.set("Authorization", `Bearer ${accessToken}`);
  } else {
    config.headers.delete("Authorization");
  }
  if (!config.headers.has("X-Team-Id") && isTeamScopedPath(config.url)) {
    const teamId = getActiveTeamId();
    if (teamId) {
      config.headers.set("X-Team-Id", teamId);
    }
  }
  return config;
});

let refreshPromise: Promise<string> | null = null;

http.interceptors.response.use(
  (response: AxiosResponse) => response,
  async (error: AxiosError<ApiErrorBody>) => {
    const config = error.config as RetryableRequestConfig | undefined;

    if (shouldRefresh(error, config)) {
      config._retry = true;
      const tokenBeforeRefresh = getRefreshToken();
      let tokenBeforeRetry: string | null | undefined;
      try {
        const accessToken = await refreshAccessToken();
        config.headers.set("Authorization", `Bearer ${accessToken}`);
        tokenBeforeRetry = getRefreshToken();
        return await http.request(config);
      } catch (refreshError) {
        if (!(refreshError instanceof StaleRefreshError)) {
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

const refreshLockName = "gotham-refresh";

const refreshLeaseKey = "gotham-refresh-lock";

const refreshLeaseTtl = requestTimeout + 5_000;

const refreshLeaseRetryMs = 250;

const refreshLeaseConfirmMs = 75;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

interface RefreshLease {
  id: string;
  expiresAt: number;
}

type LeaseAttempt =
  | { status: "acquired"; release: () => void }
  | { status: "held" }
  | { status: "unusable" };

function getLockStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

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

async function tryAcquireRefreshLease(): Promise<LeaseAttempt> {
  const storage = getLockStorage();
  if (!storage) {
    return { status: "unusable" };
  }
  if (readRefreshLease(storage)) {
    return { status: "held" };
  }

  const id = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  const lease: RefreshLease = { id, expiresAt: Date.now() + refreshLeaseTtl };
  try {
    storage.setItem(refreshLeaseKey, JSON.stringify(lease));
  } catch {
    return { status: "unusable" };
  }

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
        }
      }
    },
  };
}

async function withRefreshLease<T>(task: () => Promise<T>): Promise<T> {
  for (;;) {
    const attempt = await tryAcquireRefreshLease();
    if (attempt.status === "unusable") {
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

const lockEnvironmentErrorNames = new Set([
  "InvalidStateError",
  "SecurityError",
  "NotSupportedError",
]);

function errorName(error: unknown): string {
  return typeof error === "object" && error !== null && "name" in error
    ? String((error as { name: unknown }).name)
    : "";
}

async function withRefreshLock<T>(task: () => Promise<T>): Promise<T> {
  const locks = typeof navigator === "undefined" ? undefined : navigator.locks;
  if (locks) {
    try {
      return (await locks.request(refreshLockName, task)) as T;
    } catch (error) {
      if (!lockEnvironmentErrorNames.has(errorName(error))) {
        throw error;
      }
    }
  }
  return withRefreshLease(task);
}

function refreshAccessToken(): Promise<string> {
  if (refreshPromise) {
    return refreshPromise;
  }

  const refreshToken = getRefreshToken();
  if (!refreshToken) {
    return Promise.reject(new Error("no refresh token available"));
  }
  const userId = getSession().user?.id ?? null;

  refreshPromise = withRefreshLock(() =>
    rotateRefreshToken(refreshToken, userId),
  ).finally(() => {
    refreshPromise = null;
  });

  return refreshPromise;
}

async function rotateRefreshToken(
  refreshToken: string,
  userId: string | null,
): Promise<string> {
  if (getRefreshToken() !== refreshToken) {
    const replacementUserId = getSession().user?.id ?? null;
    if (userId && replacementUserId && userId !== replacementUserId) {
      throw new StaleRefreshError();
    }
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
    if (getRefreshToken() !== refreshToken) {
      throw new StaleRefreshError();
    }
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
    if (getRefreshToken() !== refreshToken) {
      throw new StaleRefreshError();
    }
    throw error;
  }
}

export function refreshSession(): Promise<string> {
  return refreshAccessToken();
}

export function expireSession(): void {
  clearSession();
  redirectToLogin();
}

function redirectToLogin(): void {
  if (typeof window === "undefined" || window.location.pathname === "/login") {
    return;
  }
  try {
    const expired = new CustomEvent("gotham:session-expired", {
      cancelable: true,
    });
    if (!window.dispatchEvent(expired)) {
      return;
    }
  } catch {
  }
  window.location.assign("/login");
}

export function teamHeaders(teamId: string): Record<string, string> {
  return teamId ? { "X-Team-Id": teamId } : {};
}

function toApiError(error: AxiosError<ApiErrorBody>): ApiError {
  const message =
    error.response?.data?.message ?? error.message ?? "Unexpected request error";

  return {
    status: error.response?.status ?? null,
    message,
    code: error.response?.data?.code,
    cause: error,
  };
}
