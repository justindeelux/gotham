import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, MouseEvent:readonly, URL, window:readonly */
// JUS-124 context-menu proof: the committed webdist is served statically with
// the control-plane file API mocked in real Chromium. Measured claims:
//   - right-click on a file, a folder and empty space opens the custom menu,
//     never the browser's native one (the handler cancels the event).
//   - right-click applies the selection rule: a lone unselected row becomes the
//     selection, a selected row keeps its multi-selection.
//   - Shift+F10 opens the same menu on the focused row.

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

/** listing builds a mixed file/folder listing the menu tests act on. */
function listing() {
  return [
    { name: "app.yaml", kind: "file", size: 2458, mode: 0o100644, modified_at: "2026-10-12T09:02:00Z", etag: '"a"' },
    { name: "config", kind: "directory", size: 0, mode: 0o040755, modified_at: "2026-10-12T09:14:00Z", etag: '"b"' },
    { name: "data", kind: "directory", size: 0, mode: 0o040755, modified_at: "2026-10-12T08:51:00Z", etag: '"c"' },
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
function rowFor(page: Page, name: string) {
  return page.locator(".fm-table .fm-row", { has: page.locator(`b:text-is("${name}")`) });
}

test("right-click on a file opens the custom menu, not the native one", async ({
  page,
}) => {
  await openFiles(page);
  await rowFor(page, "app.yaml").click({ button: "right" });

  const menu = page.getByRole("menu", { name: "More actions for app.yaml" });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: "Edit in editor" })).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: "Cut" })).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: "Copy" })).toBeVisible();
  // No duplicate Move: cut + paste is the move now.
  await expect(menu.getByRole("menuitem", { name: "Move", exact: true })).toHaveCount(0);

  // Esc closes it.
  await page.keyboard.press("Escape");
  await expect(menu).toBeHidden();

  // Outside click closes it too.
  await rowFor(page, "app.yaml").click({ button: "right" });
  await expect(menu).toBeVisible();
  await page.locator(".fm-card-title").click();
  await expect(menu).toBeHidden();
});

test("right-click on a special entry opens no menu", async ({ page }) => {
  await openFiles(page);
  await rowFor(page, "run.sock").click({ button: "right" });
  await expect(page.getByRole("menu")).toHaveCount(0);
});

test("the custom menu cancels the native context menu", async ({ page }) => {
  await openFiles(page);

  // dispatchEvent returns false when a cancelable event was preventDefault-ed,
  // which is exactly how the browser's native menu is suppressed.
  const prevented = await page.evaluate(() => {
    const row = document.querySelector(
      ".fm-table .fm-row:not(.fm-head)",
    ) as HTMLElement | null;
    if (row === null) {
      return null;
    }
    const event = new MouseEvent("contextmenu", {
      bubbles: true,
      cancelable: true,
      button: 2,
      clientX: 48,
      clientY: 96,
    });
    return !row.dispatchEvent(event);
  });

  expect(prevented).toBe(true);
  await expect(page.getByRole("menu")).toBeVisible();
});

test("right-click on a folder opens its row menu", async ({ page }) => {
  await openFiles(page);
  await rowFor(page, "config").click({ button: "right" });

  const menu = page.getByRole("menu", { name: "More actions for config" });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: "Open", exact: true })).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: "Move", exact: true })).toHaveCount(0);
});

test("folder-row Paste copies into that folder", async ({ page }) => {
  await openFiles(page);
  const copies: Array<Record<string, unknown>> = [];
  await page.route(`**/api/v1/servers/${server1}/files/copy`, async (route) => {
    copies.push(route.request().postDataJSON() as Record<string, unknown>);
    await route.fulfill({
      json: {
        entry: {
          name: "app.yaml",
          kind: "file",
          size: 2458,
          mode: 0o100644,
          modified_at: "2026-10-12T09:02:00Z",
          etag: '"x"',
        },
      },
    });
  });

  // Stage a copy from a row menu, then paste from the folder's row menu. Wait
  // for each menu to settle so the click lands on the item, not the row behind.
  await rowFor(page, "app.yaml").click({ button: "right" });
  const fileMenu = page.getByRole("menu", { name: "More actions for app.yaml" });
  await expect(fileMenu).toBeVisible();
  await fileMenu.getByRole("menuitem", { name: "Copy" }).click();

  await rowFor(page, "config").click({ button: "right" });
  const folderMenu = page.getByRole("menu", { name: "More actions for config" });
  await expect(folderMenu).toBeVisible();
  await folderMenu.getByRole("menuitem", { name: "Paste" }).click();

  await expect.poll(() => copies.length).toBe(1);
  expect(copies[0]).toMatchObject({
    root: "volumes",
    source: "app.yaml",
    destination: "config/app.yaml",
    overwrite: false,
  });
});

test("right-click on empty space opens the folder-level menu", async ({ page }) => {
  await openFiles(page);
  const wrap = page.locator(".table-wrap");
  const box = await wrap.boundingBox();
  expect(box).not.toBeNull();
  // A point below the last row: still inside the list, not on any row.
  await wrap.click({
    button: "right",
    position: { x: box!.width / 2, y: box!.height - 24 },
  });

  const menu = page.getByRole("menu", { name: "Folder actions" });
  await expect(menu).toBeVisible();
  for (const name of ["New file", "New folder", "Paste", "Upload", "Refresh"]) {
    await expect(menu.getByRole("menuitem", { name, exact: true })).toBeVisible();
  }
});

test("Shift+F10 opens the row menu on the focused row", async ({ page }) => {
  await openFiles(page);
  await rowFor(page, "app.yaml").locator(".fm-label").focus();
  await page.keyboard.press("Shift+F10");

  const menu = page.getByRole("menu", { name: "More actions for app.yaml" });
  await expect(menu).toBeVisible();
  await expect(menu.getByRole("menuitem", { name: "Edit in editor" })).toBeVisible();
});

test("right-click keeps a multi-selection but selects a lone unselected row", async ({
  page,
}) => {
  await openFiles(page);
  const selected = page.locator(".fm-table .fm-row.is-selected");

  // Folders sort first, so pick by name. Select config and data.
  await rowFor(page, "config").hover();
  await rowFor(page, "config").locator(".n-checkbox").click();
  await rowFor(page, "data").hover();
  await rowFor(page, "data").locator(".n-checkbox").click();
  await expect(selected).toHaveCount(2);

  // Right-clicking a selected row keeps the whole selection.
  await rowFor(page, "config").click({ button: "right" });
  await expect(page.getByRole("menu")).toBeVisible();
  await expect(selected).toHaveCount(2);
  await page.keyboard.press("Escape");

  // Right-clicking an unselected row makes it the sole selection.
  await rowFor(page, "app.yaml").click({ button: "right" });
  await expect(page.getByRole("menu")).toBeVisible();
  await expect(selected).toHaveCount(1);
  await expect(selected.first()).toContainText("app.yaml");
});
