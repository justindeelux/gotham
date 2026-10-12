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

// Real-browser proof for the P18 step 8 editor conflict modal: the committed
// webdist is served statically with the control-plane file API mocked, a
// session is seeded in localStorage, and real Chromium measures the real Naive
// UI DOM. The file is opened in the editor (loading the CodeMirror chunk), a
// change is typed, Ctrl/Cmd+S meets a mocked 409, and the conflict modal is
// measured. Same harness as environment-layout.spec.ts.

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

const serverId = "33333333-3333-4333-8333-333333333333";

const serverRow = {
  id: serverId,
  name: "prod-01",
  ip: "10.0.0.1",
  status: "ready",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

const root = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 21 * 1024 ** 3,
};

const entry = {
  name: "app.yaml",
  kind: "file",
  size: 24,
  mode: 0o100644,
  modified_at: "2026-10-12T09:02:00Z",
  etag: '"v1"',
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

/** mockApi seeds the session and answers the calls the files page makes. */
async function mockApi(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  // Catch-all first so the specific routes registered after it win.
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: session.user });
  });
  await page.route("**/api/v1/teams", async (route) => {
    await route.fulfill({ json: { teams: [team] } });
  });
  await page.route("**/api/v1/version", async (route) => {
    await route.fulfill({ json: { version: "v0.2.1-dev" } });
  });
  await page.route("**/api/v1/servers", async (route) => {
    await route.fulfill({ json: { servers: [serverRow] } });
  });
  await page.route(`**/api/v1/servers/${serverId}`, async (route) => {
    await route.fulfill({ json: serverRow });
  });
  await page.route(`**/api/v1/servers/${serverId}/files/roots`, async (route) => {
    await route.fulfill({ json: { roots: [root] } });
  });
  await page.route(`**/api/v1/servers/${serverId}/files/list*`, async (route) => {
    await route.fulfill({ json: { entries: [entry], truncated: false, cursor: "" } });
  });
  await page.route(`**/api/v1/servers/${serverId}/files/content*`, async (route) => {
    if (route.request().method() === "PUT") {
      // A concurrent write: the editor's If-Match no longer matches.
      await route.fulfill({ status: 409, json: { message: "conflict" } });
      return;
    }
    await route.fulfill({
      json: {
        content: "services:\n  web:\n",
        etag: '"v1"',
        sha256: "abc",
        size: 24,
        mode: 0o100644,
      },
    });
  });
}

/** openConflict opens app.yaml, edits it, and triggers the 409 conflict modal. */
async function openConflict(page: Page): Promise<Locator> {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  const label = page.locator(".fm-label", { hasText: "app.yaml" }).first();
  await expect(label).toBeVisible();
  await label.click();

  // The CodeMirror chunk loads lazily; wait for the real editor surface.
  const editor = page.locator(".cm-content");
  await expect(editor).toBeVisible({ timeout: 15_000 });
  await editor.click();
  await page.keyboard.type("# changed");

  // The dirty dot appears on the tab.
  await expect(page.locator(".ed-dot")).toHaveCount(1);

  await page.keyboard.press("ControlOrMeta+s");

  const dialog = page.locator(".n-modal.n-card");
  await expect(dialog).toBeVisible({ timeout: 10_000 });
  await expect
    .poll(async () => (await dialog.boundingBox())?.width ?? 0, { timeout: 5000 })
    .toBeGreaterThanOrEqual(400);
  return dialog;
}

test("editor save conflict opens a bounded modal with pinned header and three choices", async ({
  page,
}) => {
  const viewport = { width: 1440, height: 900 };
  const dialog = await openConflict(page);
  const dialogBox = await dialog.boundingBox();
  expect(dialogBox, "dialog has a box").not.toBeNull();
  expect(dialogBox!.width, "dialog is ~480px wide").toBeLessThanOrEqual(560);
  expect(dialogBox!.width, "dialog is ~480px wide").toBeGreaterThanOrEqual(400);
  // The modal never exceeds the viewport.
  expect(dialogBox!.y).toBeGreaterThanOrEqual(0);
  expect(dialogBox!.y + dialogBox!.height).toBeLessThanOrEqual(viewport.height);

  // Header stays fixed while the body scrolls.
  const header = dialog.locator(".n-card-header");
  const body = dialog.locator(".n-card-content");
  const before = await header.boundingBox();
  await body.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  const after = await header.boundingBox();
  expect(after!.y).toBeCloseTo(before!.y, 0);

  // The three conflict choices, left to right: Reload, Save as copy, Overwrite.
  const reload = dialog.getByRole("button", { name: "Reload", exact: true });
  const saveAs = dialog.getByRole("button", { name: "Save as copy", exact: true });
  const overwrite = dialog.getByRole("button", { name: "Overwrite", exact: true });
  await expect(reload).toBeVisible();
  await expect(saveAs).toBeVisible();
  await expect(overwrite).toBeVisible();
  const reloadBox = await reload.boundingBox();
  const saveAsBox = await saveAs.boundingBox();
  const overwriteBox = await overwrite.boundingBox();
  expect(reloadBox!.x + reloadBox!.width).toBeLessThanOrEqual(saveAsBox!.x);
  expect(saveAsBox!.x + saveAsBox!.width).toBeLessThanOrEqual(overwriteBox!.x);
  // Every action stays inside the viewport (the acceptance proof).
  for (const box of [reloadBox, saveAsBox, overwriteBox]) {
    expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height);
  }
});

test("the conflict modal stays inside a short viewport", async ({ page }) => {
  const viewport = { width: 1280, height: 640 };
  await page.setViewportSize(viewport);
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  const label = page.locator(".fm-label", { hasText: "app.yaml" }).first();
  await expect(label).toBeVisible();
  await label.click();
  const editor = page.locator(".cm-content");
  await expect(editor).toBeVisible({ timeout: 15_000 });
  await editor.click();
  await page.keyboard.type("# changed");
  await page.keyboard.press("ControlOrMeta+s");

  const dialog = page.locator(".n-modal.n-card");
  await expect(dialog).toBeVisible({ timeout: 10_000 });
  const dialogBox = await dialog.boundingBox();
  expect(dialogBox, "dialog has a box").not.toBeNull();
  expect(dialogBox!.y).toBeGreaterThanOrEqual(0);
  expect(dialogBox!.y + dialogBox!.height).toBeLessThanOrEqual(viewport.height);
  await expect(dialog.getByRole("button", { name: "Overwrite" })).toBeVisible();
});
