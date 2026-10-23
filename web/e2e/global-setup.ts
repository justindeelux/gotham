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

export default async function globalSetup(): Promise<void> {
  const email =
    process.env.GOTHAM_E2E_EMAIL ?? `ui-e2e-${uniqueSuffix()}@example.com`;
  const password = "Gotham-E2E-Password1";

  const api = await request.newContext({ baseURL, timeout: 15_000 });
  try {
    let response = await api.post("/api/v1/auth/register", {
      data: { email, password },
    });
    if (!response.ok()) {
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
