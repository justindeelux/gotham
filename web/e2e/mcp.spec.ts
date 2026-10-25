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

async function currentSession(request: APIRequestContext): Promise<Session> {
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

  const email = `ui-e2e-mcp-member-${uniqueSuffix()}@example.com`;
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

async function openMcpSetup(page: Page): Promise<void> {
  await page.goto("/settings/mcp");
  await expect(
    page.getByRole("heading", { name: "MCP setup", level: 1 }),
  ).toBeVisible();
  await expect(page.getByTestId("mcp-create-token")).toBeVisible();
}

async function createToken(page: Page, accessLevel: "read" | "deploy"): Promise<string> {
  if (accessLevel === "deploy") {
    await page
      .locator(".n-radio")
      .filter({ hasText: "Read and deploy" })
      .click();
  }
  await page.getByTestId("mcp-create-token").click();

  const secret = page.locator("#mcp-token-secret");
  await expect(secret).toBeVisible();
  const value = await secret.inputValue();
  expect(value).toMatch(/^gt1_/);
  return value;
}

function pageSnippet(page: Page, id: string) {
  return page.locator("#app").getByTestId(`mcp-snippet-${id}`);
}

function dialogSnippet(page: Page, id: string) {
  return page.getByRole("dialog").getByTestId(`mcp-snippet-${id}`);
}

test("sets up an MCP client as the owner", async ({ page, request, guardrails }) => {
  test.skip(!(await isOwner(request)), "the smoke account is not the instance owner");

  await page.goto("/dashboard");
  await openMcpSetup(page);

  // Plain-HTTP test instance: the warning points the owner at the instance settings.
  const warning = page.getByTestId("mcp-https-warning");
  await expect(warning).toBeVisible();
  await expect(warning).toContainText(
    "Set the control-plane URL to an HTTPS address",
  );
  await expect(
    warning.getByRole("button", { name: "Open instance settings" }),
  ).toBeVisible();

  const preview = pageSnippet(page, "claude-desktop");
  await expect(preview).toContainText("<YOUR_TOKEN>");
  await expect(preview).toContainText("/mcp");

  const secret = await createToken(page, "deploy");
  await expect(dialogSnippet(page, "claude-code")).toContainText(secret);
  await expect(dialogSnippet(page, "claude-desktop")).toContainText(
    `"Authorization": "Bearer ${secret}"`,
  );
  await expect(dialogSnippet(page, "url-token")).toContainText(`Token: ${secret}`);

  // Nothing is persisted, and closing the reveal drops the token from the page.
  const storage = await page.evaluate(() => JSON.stringify(localStorage));
  expect(storage).not.toContain(secret);
  expect(page.url()).not.toContain(secret);
  await page.getByTestId("mcp-reveal-done").click();
  await expect(page.locator("#mcp-token-secret")).toBeHidden();
  expect(await page.content()).not.toContain(secret);
  await expect(pageSnippet(page, "claude-code")).toContainText("<YOUR_TOKEN>");

  const listed = await request.get("/api/v1/tokens", { headers: authHeaders() });
  expect(listed.status()).toBe(200);
  const body = (await listed.json()) as {
    tokens: Array<{ name: string; scopes: string[] }>;
  };
  const token = body.tokens.find((item) => item.name.startsWith("MCP setup"));
  expect(token?.scopes).toContain("deploy");
  expect(JSON.stringify(body)).not.toContain(secret);

  expect(
    guardrails.apiFailures,
    `failed API requests:\n${guardrails.apiFailures.join("\n")}`,
  ).toEqual([]);
});

test("sets up an MCP client as a member without the admin scope", async ({
  page,
  request,
  guardrails,
}) => {
  const session = await createMemberSession(request);
  test.skip(session.user.role === "owner", "could not obtain a member account");

  await signInAs(page, session);
  await openMcpSetup(page);

  await expect(page.getByRole("radio", { name: /^Read only/ })).toHaveCount(1);
  await expect(page.getByRole("radio", { name: /^Read and deploy/ })).toHaveCount(1);
  await expect(page.getByRole("radio", { name: /^Admin/ })).toHaveCount(0);

  const warning = page.getByTestId("mcp-https-warning");
  await expect(warning).toBeVisible();
  await expect(warning).toContainText("Ask the instance owner");
  await expect(
    warning.getByRole("button", { name: "Open instance settings" }),
  ).toHaveCount(0);

  const secret = await createToken(page, "read");
  await expect(dialogSnippet(page, "claude-code")).toContainText(
    `--header "Authorization: Bearer ${secret}"`,
  );
  const storage = await page.evaluate(() => JSON.stringify(localStorage));
  expect(storage).not.toContain(secret);

  await page.getByTestId("mcp-reveal-done").click();
  expect(await page.content()).not.toContain(secret);

  const listed = await request.get("/api/v1/tokens", {
    headers: { Authorization: `Bearer ${session.accessToken}` },
  });
  expect(listed.status()).toBe(200);
  const body = (await listed.json()) as {
    tokens: Array<{ name: string; scopes: string[] }>;
  };
  const token = body.tokens.find((item) => item.name.startsWith("MCP setup"));
  expect(token?.scopes).toEqual(["read"]);

  expect(
    guardrails.apiFailures,
    `failed API requests:\n${guardrails.apiFailures.join("\n")}`,
  ).toEqual([]);
});
