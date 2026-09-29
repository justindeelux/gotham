import { mkdirSync, writeFileSync } from "node:fs";
import { request } from "@playwright/test";

import {
  accountDir,
  accountPath,
  baseURL,
  storageKey,
  storageStatePath,
  uniqueSuffix,
  type E2EAccount,
} from "./support";

interface RegisterResponse {
  user: { id: string };
  access_token: string;
  refresh_token: string;
}

/**
 * global setup registers one unique account through the real API and persists
 * two artifacts:
 *
 *   · .auth/state.json   — Playwright storage state holding the token pair,
 *                          so navigation/application specs start signed in.
 *   · .auth/account.json — the credentials, so the auth spec can exercise the
 *                          real login form with the same account.
 *
 * Creating the account once keeps the run inside the register/login per-IP
 * rate limit (5/minute burst).
 */
export default async function globalSetup(): Promise<void> {
  // A fixed email lets the control plane list this account as a platform
  // operator (PLATFORM_ADMINS) so the DNS-provider surface — platform-global
  // since BE-8.2 — is reachable by the smoke. Otherwise a unique per-run email
  // keeps parallel runs isolated.
  const email =
    process.env.GOTHAM_E2E_EMAIL ?? `ui-e2e-${uniqueSuffix()}@example.com`;
  const password = "Gotham-E2E-Password1";

  const api = await request.newContext({ baseURL, timeout: 15_000 });
  try {
    let response = await api.post("/api/v1/auth/register", {
      data: { email, password },
    });
    if (response.status() === 409) {
      // The account survives across runs on a persistent database; sign in.
      response = await api.post("/api/v1/auth/login", {
        data: { email, password },
      });
    }
    if (!response.ok()) {
      throw new Error(
        `global setup: register ${email} failed: ${response.status()} ${await response.text()}`,
      );
    }

    const body = (await response.json()) as RegisterResponse;
    const session = {
      user: body.user,
      accessToken: body.access_token,
      refreshToken: body.refresh_token,
    };
    const state = {
      cookies: [],
      origins: [
        {
          origin: baseURL,
          localStorage: [{ name: storageKey, value: JSON.stringify(session) }],
        },
      ],
    };
    const account: E2EAccount = {
      email,
      password,
      accessToken: body.access_token,
      userId: body.user.id,
    };

    mkdirSync(accountDir(), { recursive: true });
    writeFileSync(storageStatePath, JSON.stringify(state, null, 2));
    writeFileSync(accountPath, JSON.stringify(account, null, 2));
  } finally {
    await api.dispose();
  }
}
