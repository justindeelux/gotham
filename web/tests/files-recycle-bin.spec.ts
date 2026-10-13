import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, window:readonly */
// JUS-122 recycle-bin proof: the committed webdist is served statically with
// the control-plane file API mocked in real Chromium. Measured claims:
//   - the toolbar's Recycle bin button opens the bin dialog for the root.
//   - the dialog lists entries (name, original path, size, kind) and says so
//     when the listing is truncated, offering Restore / Delete / Empty bin.
//   - an empty bin shows the empty state, and the folder-level context menu
//     also opens the bin.

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

/** binEntry builds one recycle-bin row the mocked API returns. */
function binEntry(overrides: Record<string, unknown> = {}) {
  return {
    id: "trash-1",
    name: "app.yaml",
    original_path: "storefront-8f2c1ab/app.yaml",
    deleted_at: "2026-10-13T10:00:00Z",
    size: 2458,
    kind: "file",
    ...overrides,
  };
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
async function openFiles(
  page: Page,
  trash: { entries: Array<Record<string, unknown>>; truncated: boolean },
): Promise<void> {
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
    await route.fulfill({
      json: {
        entries: [
          {
            name: "app.yaml",
            kind: "file",
            size: 2458,
            mode: 0o100644,
            modified_at: "2026-10-12T09:02:00Z",
            etag: '"a"',
          },
        ],
        truncated: false,
        cursor: "",
      },
    });
  });
  await page.route(`**/api/v1/servers/${server1}/files/trash*`, async (route) => {
    await route.fulfill({ json: trash });
  });

  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label b").first()).toBeVisible();
}

test("the toolbar opens the recycle bin with entries and a truncation note", async ({
  page,
}) => {
  await openFiles(page, {
    entries: [binEntry()],
    truncated: true,
  });

  await page.getByRole("button", { name: "Recycle bin" }).click();

  const dialog = page.locator(".n-modal.app-modal");
  await expect(dialog).toBeVisible();
  // Column headers and the entry's name + recorded original path.
  await expect(dialog.getByText("Original path")).toBeVisible();
  await expect(dialog.getByRole("cell", { name: "app.yaml", exact: true })).toBeVisible();
  await expect(
    dialog.getByRole("cell", { name: "storefront-8f2c1ab/app.yaml" }),
  ).toBeVisible();
  // The node's cap is stated, never hidden.
  await expect(dialog.getByText(/more entries than shown/)).toBeVisible();
  // Row actions and the bin-wide Empty action.
  await expect(
    dialog.getByRole("button", { name: "Restore app.yaml" }),
  ).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Delete app.yaml permanently" }),
  ).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Empty bin" })).toBeVisible();
});

test("an empty bin shows the empty state", async ({ page }) => {
  await openFiles(page, { entries: [], truncated: false });

  await page.getByRole("button", { name: "Recycle bin" }).click();

  const dialog = page.locator(".n-modal.app-modal");
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("The recycle bin is empty")).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Empty bin" })).toHaveCount(0);
});

test("the folder-level context menu also opens the recycle bin", async ({
  page,
}) => {
  await openFiles(page, { entries: [binEntry()], truncated: false });

  const wrap = page.locator(".table-wrap");
  const box = await wrap.boundingBox();
  expect(box).not.toBeNull();
  await wrap.click({
    button: "right",
    position: { x: box!.width / 2, y: box!.height - 24 },
  });

  const menu = page.getByRole("menu", { name: "Folder actions" });
  await expect(menu).toBeVisible();
  await menu.getByRole("menuitem", { name: "Recycle bin…" }).click();

  await expect(page.locator(".n-modal.app-modal")).toBeVisible();
  await expect(
    page.locator(".n-modal.app-modal").getByRole("cell", { name: "app.yaml", exact: true }),
  ).toBeVisible();
});
