import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, URL, window:readonly */
// JUS-118/119/120 layout proof: the committed webdist is served statically with
// the control-plane file API mocked in real Chromium. Measured claims:
//   - JUS-118 the browser panel fills the viewport and only the entry list
//     scrolls (no page-level scroll) at desktop and mobile widths.
//   - JUS-119 the page-top breadcrumb is gone and a bottom bar names the node
//     and switches to another node's files.
//   - JUS-120 the toolbar groups New file/New folder under one dropdown, keeps
//     bulk tools on the right, and collapses to labelled icon-only buttons on
//     small screens.

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
const server2 = "22222222-2222-4222-8222-222222222222";

function serverRow(id: string, name: string, ip: string) {
  return {
    id,
    name,
    ip,
    status: "ready",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

const row1 = serverRow(server1, "prod-01", "10.0.0.1");
const row2 = serverRow(server2, "prod-02", "10.0.0.2");
const rows = [row1, row2];

const root = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 21 * 1024 ** 3,
};

/** manyEntries produces a listing tall enough to force the list to scroll. */
function manyEntries() {
  return Array.from({ length: 60 }, (_, index) => ({
    name: `file-${String(index).padStart(2, "0")}.txt`,
    kind: "file",
    size: 1024 + index,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: `"e${index}"`,
  }));
}

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

/** mockApi seeds a session and answers the files page calls for both nodes. */
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
    await route.fulfill({ json: { servers: rows } });
  });
  for (const row of rows) {
    await page.route(`**/api/v1/servers/${row.id}`, async (route) => {
      await route.fulfill({ json: row });
    });
    await page.route(`**/api/v1/servers/${row.id}/files/roots`, async (route) => {
      await route.fulfill({ json: { roots: [root] } });
    });
    await page.route(`**/api/v1/servers/${row.id}/files/list*`, async (route) => {
      await route.fulfill({
        json: { entries: manyEntries(), truncated: false, cursor: "" },
      });
    });
  }
}

test("the browser fills the viewport and only the entry list scrolls", async ({
  page,
}) => {
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${server1}/files`);

    const card = page.locator(".files-browser");
    await expect(card).toBeVisible();
    await expect(page.locator(".fm-label").first()).toBeVisible();

    // No page-level scroll: neither the window nor the .view container scrolls.
    const scroll = await page.evaluate(() => {
      const view = document.querySelector(".view") as HTMLElement | null;
      const doc = document.scrollingElement as HTMLElement;
      return {
        docScroll: doc.scrollHeight - window.innerHeight,
        viewScroll: view ? view.scrollHeight - view.clientHeight : 0,
      };
    });
    expect(scroll.docScroll).toBeLessThanOrEqual(1);
    expect(scroll.viewScroll).toBeLessThanOrEqual(1);

    // The panel reaches the bottom of the content column (bar + padding).
    const box = await card.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.y + box!.height).toBeGreaterThan(viewport.height - 120);

    // The list owns the overflow: it is taller than its scrollport.
    const list = await page.evaluate(() => {
      const wrap = document.querySelector(".table-wrap") as HTMLElement | null;
      return wrap ? { scroll: wrap.scrollHeight, client: wrap.clientHeight } : null;
    });
    expect(list).not.toBeNull();
    expect(list!.scroll).toBeGreaterThan(list!.client);
  }
});

test("the top breadcrumb is gone and the bottom node bar switches nodes", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);

  // JUS-119: the page-top breadcrumb (Servers / node / Files) is removed.
  await expect(page.locator(".breadcrumb")).toHaveCount(0);

  const bar = page.locator(".node-bar");
  await expect(bar).toBeVisible();
  await expect(bar).toContainText("prod-01");

  // Switching nodes reloads that node's files.
  await page.locator(".node-bar__select").click();
  await page.locator(".n-base-select-option", { hasText: "prod-02" }).click();
  await expect(page.locator(".node-bar__select")).toContainText("prod-02");
  expect(new URL(page.url()).pathname).toContain(server2);
  await expect(page.locator(".fm-label").first()).toBeVisible();
});

test("the toolbar groups New, keeps bulk tools right and is icon-only on small screens", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);

  // JUS-120: New file / New folder live inside one dropdown.
  await expect(page.getByRole("button", { name: "New file", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "New folder", exact: true })).toHaveCount(0);
  const newButton = page.getByRole("button", { name: "New", exact: true });
  await expect(newButton).toBeVisible();
  await newButton.click();
  await expect(page.getByRole("menuitem", { name: "New file", exact: true })).toBeVisible();
  await expect(page.getByRole("menuitem", { name: "New folder", exact: true })).toBeVisible();
  await page.keyboard.press("Escape");

  // Bulk tools sit to the right of the create/upload group.
  const upload = await page.getByRole("button", { name: "Upload", exact: true }).boundingBox();
  const compress = await page
    .getByRole("button", { name: "Compress…", exact: true })
    .boundingBox();
  expect(upload).not.toBeNull();
  expect(compress).not.toBeNull();
  expect(compress!.x).toBeGreaterThan(upload!.x + upload!.width);

  // Small screens: labels are hidden and every toolbar button keeps its name.
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".fm-btn-label").first()).toBeHidden();
  for (const name of ["New", "Upload", "Compress…"]) {
    await expect(page.getByRole("button", { name, exact: true })).toHaveAttribute(
      "aria-label",
      name,
    );
  }
});
