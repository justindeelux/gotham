import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

/** Directory holding this file (used to anchor generated artifacts). */
const e2eDir = fileURLToPath(new URL(".", import.meta.url));

/** Base URL of the control plane under test. */
export const baseURL = process.env.GOTHAM_E2E_BASE_URL ?? "http://127.0.0.1:8099";

/** localStorage key the SPA persists its session under. */
export const storageKey = "gotham.auth.session";

/** Playwright storage state written by global setup (authenticated context). */
export const storageStatePath = join(e2eDir, ".auth", "state.json");

/** Credentials + token for the account created in global setup. */
export const accountPath = join(e2eDir, ".auth", "account.json");

/**
 * Public clone URL used by the seeded application. It is only stored as a
 * string — the smoke never triggers a deployment, so nothing is cloned.
 */
export const cloneURL = "https://github.com/docker/welcome-to-docker.git";

/** Account created once per run and shared by every scenario. */
export interface E2EAccount {
  email: string;
  password: string;
  accessToken: string;
  userId: string;
}

/** uniqueSuffix namespaces every run's email and application name. */
export function uniqueSuffix(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

/** accountDir is the directory the generated artifacts live in. */
export function accountDir(): string {
  return dirname(storageStatePath);
}

/** loadAccount reads the account written by global setup. */
export function loadAccount(): E2EAccount {
  return JSON.parse(readFileSync(accountPath, "utf8")) as E2EAccount;
}
