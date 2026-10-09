import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, window:readonly */
// URL/window run inside the page and server callbacks (browser/Node URL
// globals); the repo lint envs do not cover tests/, so they are declared.

// Real-browser proof for JUS-74 fix round 1: the committed webdist is served
// statically with the control-plane API mocked, a session is seeded in
// localStorage, and real Chromium measures the real Naive UI DOM bounding
// boxes. No backend is involved. Same harness as environment-layout.spec.ts.
//
// The previous round passed review on source-text assertions yet rendered
// wrongly (scoped :deep rules never reach NModal's teleported card), so every
// claim here is a measured box: modal width, input width, hint below input,
// footer order.

const testsDir = dirname(fileURLToPath(import.meta.url));
const webdistRoot = resolve(testsDir, "../../internal/server/webdist");

const mimeTypes: Record<string, string> = {
  ".css": "text/css",
  ".html": "text/html",
  ".ico": "image/x-icon",
  ".js": "text/javascript",
  ".json": "application/json",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".woff2": "font/woff2",
};

const session = {
  user: {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-03-04T12:00:00Z",
    display_name: "Ada",
    has_password: true,
    is_platform_admin: false,
  },
  accessToken: "test-access",
  refreshToken: "test-refresh",
};

const team = {
  id: "team-1",
  name: "Acme",
  is_personal: false,
  role: "owner",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

let server: Server;
let baseURL = "";

test.beforeAll(async () => {
  server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url ?? "/", "http://127.0.0.1");
      let path = decodeURIComponent(url.pathname);
      if (path === "/" || !extname(path)) {
        // SPA fallback: client-side routes serve index.html.
        path = "/index.html";
      }
      const file = resolve(webdistRoot, `.${path}`);
      if (!file.startsWith(webdistRoot)) {
        response.writeHead(403);
        response.end();
        return;
      }
      const body = await readFile(file);
      response.writeHead(200, {
        "content-type": mimeTypes[extname(file)] ?? "application/octet-stream",
      });
      response.end(body);
    } catch {
      response.writeHead(404);
      response.end();
    }
  });
  await new Promise<void>((done) => {
    server.listen(0, "127.0.0.1", () => done());
  });
  const address = server.address() as AddressInfo;
  baseURL = `http://127.0.0.1:${address.port}`;
});

test.afterAll(async () => {
  await new Promise<void>((done) => {
    server.close(() => done());
  });
});

/** mockApi seeds the session and answers the calls the Git sources page makes. */
async function mockApi(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: session.user });
  });
  await page.route("**/api/v1/teams", async (route) => {
    await route.fulfill({ json: { teams: [team] } });
  });
  await page.route("**/api/v1/version", async (route) => {
    await route.fulfill({ json: { version: "v0.2.1-dev" } });
  });
  await page.route("**/api/v1/providers/github-app", async (route) => {
    await route.fulfill({ json: { apps: [] } });
  });
  await page.route("**/api/v1/providers", async (route) => {
    await route.fulfill({ json: { providers: [] } });
  });
  await page.route("**/api/v1/applications*", async (route) => {
    await route.fulfill({ json: { applications: [] } });
  });
}

/** openDialog opens one connect modal from the empty Git sources page. */
async function openDialog(page: Page, name: string): Promise<Locator> {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/settings/git-sources`);
  await page.getByRole("button", { name, exact: true }).first().click();
  const dialog = page.locator(".n-modal.n-card");
  await expect(dialog).toBeVisible();
  // NModal opens with a scale transition: wait until the card settles.
  await expect
    .poll(async () => (await dialog.boundingBox())?.width ?? 0, { timeout: 5000 })
    .toBeGreaterThanOrEqual(480);
  return dialog;
}

/** expectStacked asserts a full-width input with its hint rendered below it. */
async function expectStacked(
  dialog: Locator,
  inputId: string,
  hintText: string,
): Promise<void> {
  const input = dialog.locator(`#${inputId}`);
  await expect(input).toBeVisible();
  const hint = dialog.locator(".field-hint", { hasText: hintText });
  await expect(hint).toBeVisible();
  const inputBox = await input.boundingBox();
  const hintBox = await hint.boundingBox();
  expect(inputBox, `${inputId} has a box`).not.toBeNull();
  expect(hintBox, `${inputId} hint has a box`).not.toBeNull();
  expect(inputBox!.width, `${inputId} spans the modal body`).toBeGreaterThanOrEqual(420);
  expect(hintBox!.y, `${inputId} hint sits below its input`).toBeGreaterThanOrEqual(
    inputBox!.y + inputBox!.height - 1,
  );
}

test("Connect GitLab modal is 520px with full-width inputs and hints below", async ({
  page,
}) => {
  const dialog = await openDialog(page, "Connect GitLab");
  const dialogBox = await dialog.boundingBox();
  expect(dialogBox, "dialog has a box").not.toBeNull();
  expect(dialogBox!.width, "dialog is ~520px wide").toBeLessThanOrEqual(600);
  expect(dialogBox!.width, "dialog is ~520px wide").toBeGreaterThanOrEqual(480);

  await expectStacked(dialog, "gitlab-instance-input", "self-hosted instance");
  const token = dialog.locator("#gitlab-admin-token-input");
  await expect(token).toHaveAttribute("placeholder", "glpat-…");
  await expectStacked(dialog, "gitlab-admin-token-input", "never stored");

  // Footer: the quiet manual toggle sits left, Cancel + primary right.
  const toggle = dialog.getByRole("button", { name: "Use manual app fields instead" });
  const cancel = dialog.getByRole("button", { name: "Cancel", exact: true });
  const primary = dialog.getByRole("button", { name: "Provision & connect" });
  const toggleBox = await toggle.boundingBox();
  const cancelBox = await cancel.boundingBox();
  const primaryBox = await primary.boundingBox();
  expect(toggleBox, "toggle has a box").not.toBeNull();
  expect(cancelBox, "cancel has a box").not.toBeNull();
  expect(primaryBox, "primary has a box").not.toBeNull();
  expect(
    toggleBox!.x + toggleBox!.width,
    "manual toggle is left of Cancel",
  ).toBeLessThanOrEqual(cancelBox!.x);
  expect(
    cancelBox!.x + cancelBox!.width,
    "primary is right of Cancel",
  ).toBeLessThanOrEqual(primaryBox!.x);
  expect(
    dialogBox!.x + dialogBox!.width - (primaryBox!.x + primaryBox!.width),
    "primary hugs the dialog right edge",
  ).toBeLessThanOrEqual(40);

  // Manual mode fields stack the same way.
  await toggle.click();
  await expectStacked(dialog, "gitlab-redirect-input", "exactly this URI");
  await expectStacked(dialog, "gitlab-client-secret-input", "never shown again");
});

test("Connect GitHub modal is 520px with a full-width name field", async ({
  page,
}) => {
  const dialog = await openDialog(page, "Connect GitHub");
  const dialogBox = await dialog.boundingBox();
  expect(dialogBox, "dialog has a box").not.toBeNull();
  expect(dialogBox!.width, "dialog is ~520px wide").toBeLessThanOrEqual(600);
  expect(dialogBox!.width, "dialog is ~520px wide").toBeGreaterThanOrEqual(480);

  await expectStacked(dialog, "github-app-name-input", "Letters, digits and dashes");

  const cancel = dialog.getByRole("button", { name: "Cancel", exact: true });
  const primary = dialog.getByRole("button", { name: "Connect GitHub" });
  const cancelBox = await cancel.boundingBox();
  const primaryBox = await primary.boundingBox();
  expect(cancelBox, "cancel has a box").not.toBeNull();
  expect(primaryBox, "primary has a box").not.toBeNull();
  expect(
    cancelBox!.x + cancelBox!.width,
    "primary is right of Cancel",
  ).toBeLessThanOrEqual(primaryBox!.x);
});
