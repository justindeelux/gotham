import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, window:readonly */
// JUS-133 discoverability proof: the committed webdist is served statically
// with the control-plane file API mocked in real Chromium. Measured claims:
//   - the ⋮ row-menu button is visible with no hover (touch reachable), while
//     the other inline row actions stay hover/focus-revealed on pointer devices;
//   - the inline actions reveal on keyboard focus, so a keyboard user reaches
//     them without a pointer;
//   - on a touch device (hover: none) the whole action set is visible and the
//     ⋮ opens the row menu;
//   - every icon-only control (toolbar + row actions) has an accessible name
//     (aria-label) and a tooltip (title).

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
    is_platform_admin: true,
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

const server1 = "11111111-1111-4111-8111-111111111111";

const row1 = {
  id: server1,
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

/** listing builds a folder, an editable file, an archive and a special row. */
function listing() {
  return [
    { name: "app.yaml", kind: "file", size: 2458, mode: 0o100644, modified_at: "2026-10-12T09:02:00Z", etag: '"a"' },
    { name: "backup.tar.gz", kind: "file", size: 4096, mode: 0o100644, modified_at: "2026-10-12T09:10:00Z", etag: '"b"' },
    { name: "config", kind: "directory", size: 0, mode: 0o040755, modified_at: "2026-10-12T09:14:00Z", etag: '"c"' },
    { name: "run.sock", kind: "special", size: 0, mode: 0o140777, modified_at: "2026-10-12T09:19:00Z", etag: '"d"' },
  ];
}

let server: Server;
let baseURL = "";

test.beforeAll(async () => {
  server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url ?? "/", "http://127.0.0.1");
      let path = decodeURIComponent(url.pathname);
      if (path === "/" || !extname(path)) {
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

/** openFiles seeds a session, mocks the file API and lands on the browser. */
async function openFiles(page: Page, entries = listing()): Promise<void> {
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
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
    await route.fulfill({ json: { servers: [row1] } });
  });
  await page.route(`**/api/v1/servers/${server1}`, async (route) => {
    await route.fulfill({ json: row1 });
  });
  await page.route(`**/api/v1/servers/${server1}/files/roots`, async (route) => {
    await route.fulfill({ json: { roots: [root] } });
  });
  await page.route(`**/api/v1/servers/${server1}/files/list*`, async (route) => {
    await route.fulfill({ json: { entries, truncated: false, cursor: "" } });
  });

  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label b").first()).toBeVisible();
}

/** rowFor returns the browser row whose name matches. */
function rowFor(page: Page, name: string): Locator {
  return page
    .locator(".fm-table .fm-row", { has: page.locator(`b:text-is("${name}")`) })
    .first();
}

test.describe("icon-only controls are accessible", () => {
  test("the ⋮ is visible at rest; inline actions reveal on hover, then focus", async ({
    page,
  }) => {
    await openFiles(page);
    const row = rowFor(page, "app.yaml");
    const inline = row.locator(".fm-action");
    const more = page.getByRole("button", { name: "More actions for app.yaml" });

    // No hover: the menu button is already visible (touch reachable), while the
    // inline actions are hidden.
    await expect(more).toHaveCSS("opacity", "1");
    await expect(inline.first()).toHaveCSS("opacity", "0");

    // Hovering the row reveals the inline actions.
    await row.hover();
    await expect(inline.first()).toHaveCSS("opacity", "1");

    // Moving the pointer away hides them again.
    await page.locator(".fm-card-title").hover();
    await expect(inline.first()).toHaveCSS("opacity", "0");

    // Keyboard focus reveals them too, with no pointer involved.
    await row.locator(".fm-label").focus();
    await expect(inline.first()).toHaveCSS("opacity", "1");

    // The ⋮ button is the last focusable control and stays visible once focused.
    await more.focus();
    await expect(more).toHaveCSS("opacity", "1");
  });

  test("every toolbar and row icon-only control has an aria-label and a title", async ({
    page,
  }) => {
    await openFiles(page);
    // Select the whole listing so every bulk action renders (enabled), then
    // shrink to the icon-only toolbar layout.
    await page.locator(".fm-head .n-checkbox").click();
    const toolbar = page.locator(".fm-toolbar");

    // Row actions are always icon-only; each pairs an accessible name with a
    // tooltip.
    const fileRow = rowFor(page, "app.yaml");
    for (const label of [
      "Download app.yaml",
      "Rename",
      "Delete app.yaml",
      "More actions for app.yaml",
    ]) {
      const button = fileRow.getByRole("button", { name: label, exact: true });
      await expect(button, label).toHaveAttribute("aria-label", label);
      await expect(button, label).toHaveAttribute("title", label);
    }
    const archiveRow = rowFor(page, "backup.tar.gz");
    for (const label of ["Extract here…", "More actions for backup.tar.gz"]) {
      const button = archiveRow.getByRole("button", { name: label, exact: true });
      await expect(button, label).toHaveAttribute("aria-label", label);
      await expect(button, label).toHaveAttribute("title", label);
    }
    const folderRow = rowFor(page, "config");
    for (const label of [
      "Open config",
      "Rename",
      "Delete config",
      "More actions for config",
    ]) {
      const button = folderRow.getByRole("button", { name: label, exact: true });
      await expect(button, label).toHaveAttribute("aria-label", label);
      await expect(button, label).toHaveAttribute("title", label);
    }

    // The toolbar drops its labels on small screens; each icon-only button must
    // keep both an accessible name and a tooltip.
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator(".fm-btn-label").first()).toBeHidden();
    for (const name of [
      "New",
      "Upload",
      "Download",
      "Copy",
      "Cut",
      "Compress…",
      "Delete",
      "Clear",
    ]) {
      const button = toolbar.getByRole("button", { name, exact: true });
      await expect(button, name).toHaveAttribute("aria-label", name);
      await expect(button, name).toHaveAttribute("title", name);
    }
  });

  test("a special row offers no row menu", async ({ page }) => {
    await openFiles(page);
    await expect(
      page.getByRole("button", { name: "More actions for run.sock" }),
    ).toHaveCount(0);
  });
});

test.describe("touch devices", () => {
  // hasTouch makes Chromium report `(hover: none)` / `(pointer: coarse)`.
  test.use({ hasTouch: true });

  test("the whole action set is visible and the ⋮ opens the row menu", async ({
    page,
  }) => {
    await openFiles(page);
    expect(await page.evaluate(() => window.matchMedia("(hover: none)").matches)).toBe(true);

    const row = rowFor(page, "app.yaml");
    await expect(row.locator(".fm-action").first()).toHaveCSS("opacity", "1");

    const more = page.getByRole("button", { name: "More actions for app.yaml" });
    await expect(more).toHaveCSS("opacity", "1");
    await more.click();
    await expect(
      page.getByRole("menu", { name: "More actions for app.yaml" }),
    ).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "Edit in editor" })).toBeVisible();
  });
});
