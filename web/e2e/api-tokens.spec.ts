import type { APIRequestContext, Page } from "@playwright/test";

import { expect, test } from "./fixtures";
import {
  loadAccount,
  storageKey,
  storageStatePath,
  uniqueSuffix,
} from "./support";

test.use({ storageState: storageStatePath, viewport: { width: 1366, height: 900 } });

interface Session {
  user: { id: string; email: string; role?: string };
  accessToken: string;
  refreshToken: string;
}

function authHeaders(): Record<string, string> {
  return { Authorization: `Bearer ${loadAccount().accessToken}` };
}

async function isOwner(request: APIRequestContext): Promise<boolean> {
  const response = await request.get("/api/v1/auth/me", {
    headers: authHeaders(),
  });
  if (!response.ok()) {
    return false;
  }
  const body = (await response.json()) as { user?: { role?: string } };
  return body.user?.role === "owner";
}

async function currentSession(
  request: APIRequestContext,
): Promise<Session> {
  const response = await request.get("/api/v1/auth/me", {
    headers: authHeaders(),
  });
  expect(response.status(), await response.text()).toBe(200);
  const body = (await response.json()) as {
    user: { id: string; email: string; role?: string };
  };
  return {
    user: body.user,
    accessToken: loadAccount().accessToken,
    refreshToken: "",
  };
}

async function createMemberSession(
  request: APIRequestContext,
): Promise<Session> {
  if (!(await isOwner(request))) {
    return currentSession(request);
  }

  const email = `ui-e2e-member-${uniqueSuffix()}@example.com`;
  const password = "Gotham-E2E-Password1";
  const invite = await request.post("/api/v1/invites", {
    headers: authHeaders(),
    data: { email, project_ids: [] },
  });
  expect(invite.status(), await invite.text()).toBe(201);
  const inviteToken = ((await invite.json()) as { token: string }).token;

  const register = await request.post("/api/v1/auth/register", {
    data: { email, password, invite_token: inviteToken },
  });
  expect(register.ok(), await register.text()).toBe(true);
  const body = (await register.json()) as {
    user: { id: string; email: string; role?: string };
    access_token: string;
    refresh_token: string;
  };
  return {
    user: body.user,
    accessToken: body.access_token,
    refreshToken: body.refresh_token,
  };
}

async function signInAs(page: Page, session: Session): Promise<void> {
  await page.goto("/dashboard");
  await page.evaluate(
    ({ key, value }) => localStorage.setItem(key, value),
    { key: storageKey, value: JSON.stringify(session) },
  );
  await page.reload();
}

async function openApiTokens(page: Page): Promise<void> {
  await page.getByRole("link", { name: "API tokens" }).click();
  await expect(page).toHaveURL(/\/security\/api-tokens$/);
  await expect(
    page.getByRole("heading", { name: "API tokens", level: 1 }),
  ).toBeVisible();
}

async function createToken(page: Page, name: string, scope?: string): Promise<string> {
  await page.getByTestId("api-tokens-create").click();
  const dialog = page.locator(".n-modal").last();
  await dialog.locator("#api-tokens-name").fill(name);
  if (scope !== undefined) {
    await dialog.locator(".n-radio").filter({ hasText: scope }).click();
  }
  await dialog.getByRole("button", { name: "Create token" }).click();

  const secret = page.locator("#api-tokens-secret");
  await expect(secret).toBeVisible();
  const value = await secret.inputValue();
  expect(value).toMatch(/^gt1_/);
  return value;
}

async function closeReveal(page: Page): Promise<void> {
  await page.getByTestId("api-tokens-reveal-done").click();
  await expect(page.locator("#api-tokens-secret")).toBeHidden();
}

function tokenRow(page: Page, name: string) {
  return page
    .locator('[data-testid="api-tokens-table"] tbody tr')
    .filter({ hasText: name });
}

async function confirmPopover(page: Page, action: string): Promise<void> {
  const popover = page.locator(".n-popconfirm").last();
  await expect(popover).toBeVisible();
  await popover.getByRole("button", { name: action, exact: true }).click();
}

test("creates, rotates and revokes a token as the owner", async ({
  page,
  request,
  guardrails,
}) => {
  test.skip(!(await isOwner(request)), "the smoke account is not the instance owner");

  const name = `ui-e2e-owner-${uniqueSuffix()}`;
  await page.goto("/dashboard");
  await openApiTokens(page);

  const firstSecret = await createToken(page, name, "Deploy");
  await expect(page).toHaveURL(/\/security\/api-tokens$/);
  expect(page.url()).not.toContain(firstSecret);
  const storage = await page.evaluate(() => JSON.stringify(localStorage));
  expect(storage).not.toContain(firstSecret);
  await closeReveal(page);
  expect(await page.content()).not.toContain(firstSecret);

  const row = tokenRow(page, name);
  await expect(row).toBeVisible();
  await expect(row).toContainText("Deploy");

  await row.getByRole("button", { name: "Rotate" }).click();
  await confirmPopover(page, "Rotate");
  const rotatedSecret = page.locator("#api-tokens-secret");
  await expect(rotatedSecret).toBeVisible();
  const secondSecret = await rotatedSecret.inputValue();
  expect(secondSecret).toMatch(/^gt1_/);
  expect(secondSecret).not.toBe(firstSecret);
  await closeReveal(page);

  await row.getByRole("button", { name: "Revoke" }).click();
  await confirmPopover(page, "Revoke");
  const revokedRow = page
    .locator('[data-testid="api-tokens-table"] tbody tr.is-revoked')
    .filter({ hasText: name });
  await expect(revokedRow).toContainText("Revoked");
  await expect(revokedRow.getByRole("button", { name: "Revoke" })).toHaveCount(0);

  const listed = await request.get("/api/v1/tokens", { headers: authHeaders() });
  expect(listed.status()).toBe(200);
  const body = (await listed.json()) as {
    tokens: Array<{ name: string; revoked_at: string | null }>;
  };
  const token = body.tokens.find((item) => item.name === name);
  expect(token?.revoked_at).not.toBeNull();
  expect(JSON.stringify(body)).not.toContain(secondSecret);

  expect(
    guardrails.apiFailures,
    `failed API requests:\n${guardrails.apiFailures.join("\n")}`,
  ).toEqual([]);
});

test("creates and revokes a token as a member without the admin scope", async ({
  page,
  request,
  guardrails,
}) => {
  const session = await createMemberSession(request);
  test.skip(session.user.role === "owner", "could not obtain a member account");

  const name = `ui-e2e-member-${uniqueSuffix()}`;
  await signInAs(page, session);
  await page.goto("/settings/api-tokens");
  await expect(page).toHaveURL(/\/security\/api-tokens$/);
  await expect(
    page.getByRole("heading", { name: "API tokens", level: 1 }),
  ).toBeVisible();

  await page.getByTestId("api-tokens-create").click();
  const dialog = page.locator(".n-modal").last();
  await expect(dialog.getByRole("radio", { name: /^Read / })).toHaveCount(1);
  await expect(dialog.getByRole("radio", { name: /^Deploy / })).toHaveCount(1);
  await expect(dialog.getByRole("radio", { name: /^Admin / })).toHaveCount(0);
  await expect(
    dialog.getByText("View projects, servers and applications. Cannot change anything."),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "Cancel" }).click();

  const secret = await createToken(page, name);
  expect(secret).toMatch(/^gt1_/);
  await closeReveal(page);

  const row = tokenRow(page, name);
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "Revoke" }).click();
  await confirmPopover(page, "Revoke");
  await expect(
    page
      .locator('[data-testid="api-tokens-table"] tbody tr.is-revoked')
      .filter({ hasText: name }),
  ).toContainText("Revoked");

  expect(
    guardrails.apiFailures,
    `failed API requests:\n${guardrails.apiFailures.join("\n")}`,
  ).toEqual([]);
});
