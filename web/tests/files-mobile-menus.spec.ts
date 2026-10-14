import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, document:readonly, window:readonly */
// JUS-145 proof: at 390px and 768px every row (list) and tile (grid) menu
// trigger is inside the viewport and the listing does not scroll horizontally,
// so a touch user reaches the row menu with one tap. The committed webdist is
// served statically with the control-plane file API mocked in real Chromium.
// Before the fix the row ⋮ sat at x≈975 in a 390px viewport (the .table-wrap
// scrolled horizontally) and grid tiles had no trigger at all.

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

/** viewports are the widths the issue names (a phone and a tablet). */
const viewports = [
  { width: 390, height: 844 },
  { width: 768, height: 1024 },
];

/** listing is a small folder of directories and files (every row has a menu). */
function listing() {
  return [
    { name: "app.yaml", kind: "file" as const, size: 2458, mode: 0o100644, modified_at: "2026-10-12T09:02:00Z", etag: '"a"' },
    { name: "backup.tar.gz", kind: "file" as const, size: 4096, mode: 0o100644, modified_at: "2026-10-12T09:10:00Z", etag: '"b"' },
    { name: "config", kind: "directory" as const, size: 0, mode: 0o040755, modified_at: "2026-10-12T09:14:00Z", etag: '"c"' },
    ...Array.from({ length: 9 }, (_, index) => ({
      name: `report-${String(index).padStart(4, "0")}.txt`,
      kind: "file" as const,
      size: 1024 + index,
      mode: 0o100644,
      modified_at: "2026-10-12T09:20:00Z",
      etag: `"r${index}"`,
    })),
  ];
}

/** COUNT is the large-directory size the issue names (5,000 entries). */
const COUNT = 5000;

/** bigListing is one folder plus 5,000 files so the list/grid window. */
function bigListing() {
  return [
    { name: "aaa-dir", kind: "directory" as const, size: 4096, mode: 0o040755, modified_at: "2026-10-12T09:02:00Z", etag: '"d"' },
    ...Array.from({ length: COUNT - 1 }, (_, index) => ({
      name: `file-${String(index).padStart(4, "0")}.txt`,
      kind: "file" as const,
      size: 1024 + index,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: `"e${index}"`,
    })),
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

interface OpenOptions {
  entries?: ReturnType<typeof listing>;
  viewport?: { width: number; height: number };
}

/** openFiles seeds a session, mocks the file API and lands on the browser. */
async function openFiles(page: Page, options: OpenOptions = {}): Promise<void> {
  const entries = options.entries ?? listing();
  const viewport = options.viewport ?? viewports[0];
  await page.setViewportSize(viewport);
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

/** moreFor is the row/tile ⋮ trigger (list and grid share the label). */
function moreFor(page: Page, name: string): Locator {
  return page.getByRole("button", { name: `More actions for ${name}` });
}

/** expectNoHorizontalOverflow proves the page (and the list wrap) never scrolls
 * sideways — the trigger must be reachable without a sideways scroll. */
async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  const doc = await page.evaluate(() => {
    const el = document.documentElement;
    return el.scrollWidth - el.clientWidth;
  });
  expect(doc, "page horizontal overflow").toBeLessThanOrEqual(1);

  const wrap = page.locator(".table-wrap");
  if ((await wrap.count()) > 0) {
    const overflow = await wrap.evaluate((el) => el.scrollWidth - el.clientWidth);
    expect(overflow, ".table-wrap horizontal overflow").toBeLessThanOrEqual(1);
  }
}

/** expectInsideViewport asserts the trigger's rectangle is fully on screen. */
async function expectInsideViewport(
  trigger: Locator,
  viewport: { width: number },
): Promise<void> {
  await expect(trigger).toBeVisible();
  const box = await trigger.boundingBox();
  expect(box, "trigger has no box").not.toBeNull();
  expect(box!.x, "trigger left edge").toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width, "trigger right edge").toBeLessThanOrEqual(
    viewport.width + 1,
  );
}

test.describe("row and tile menus are reachable without scrolling (JUS-145)", () => {
  for (const viewport of viewports) {
    test(`list at ${viewport.width}px: every row ⋮ is on screen`, async ({ page }) => {
      await openFiles(page, { viewport });
      await expectNoHorizontalOverflow(page);

      const rows = page.locator(".fm-table .fm-row:not(.fm-head)");
      const count = await rows.count();
      expect(count).toBe(12);
      for (let index = 0; index < count; index += 1) {
        const name = (await rows.nth(index).getAttribute("data-entry")) ?? "";
        await expectInsideViewport(moreFor(page, name), viewport);
      }
    });

    test(`grid at ${viewport.width}px: every tile ⋮ is on screen`, async ({ page }) => {
      await openFiles(page, { viewport });
      await page.getByRole("button", { name: "Grid view" }).click();
      await expectNoHorizontalOverflow(page);

      const tiles = page.locator(".fm-grid .fm-tile-wrap");
      const count = await tiles.count();
      expect(count).toBe(12);
      for (let index = 0; index < count; index += 1) {
        const name = (await tiles.nth(index).getAttribute("data-entry")) ?? "";
        await expectInsideViewport(moreFor(page, name), viewport);
      }
    });
  }

  test("a 5,000-entry folder stays windowed and keeps its menus on screen", async ({
    page,
  }) => {
    const entries = bigListing();
    for (const viewport of viewports) {
      await openFiles(page, { entries, viewport });
      await expectNoHorizontalOverflow(page);

      // Windowed: only a fraction of the 5,000 rows are in the DOM.
      const rows = page.locator(".fm-table .fm-row:not(.fm-head)");
      const rendered = await rows.count();
      expect(rendered).toBeGreaterThan(0);
      expect(rendered).toBeLessThan(500);
      for (let index = 0; index < Math.min(rendered, 5); index += 1) {
        const name = (await rows.nth(index).getAttribute("data-entry")) ?? "";
        await expectInsideViewport(moreFor(page, name), viewport);
      }

      await page.getByRole("button", { name: "Grid view" }).click();
      const tiles = page.locator(".fm-grid .fm-tile-wrap");
      const tileCount = await tiles.count();
      expect(tileCount).toBeGreaterThan(0);
      expect(tileCount).toBeLessThan(500);
      for (let index = 0; index < Math.min(tileCount, 5); index += 1) {
        const name = (await tiles.nth(index).getAttribute("data-entry")) ?? "";
        await expectInsideViewport(moreFor(page, name), viewport);
      }
    }
  });

  test("the grid ⋮ follows the same popup/ARIA contract as the list", async ({
    page,
  }) => {
    await openFiles(page, { viewport: { width: 1280, height: 900 } });
    await page.getByRole("button", { name: "Grid view" }).click();

    const more = moreFor(page, "app.yaml");
    const menu = page.getByRole("menu", { name: "More actions for app.yaml" });
    await expect(more).toHaveAttribute("aria-haspopup", "menu");
    await expect(more).toHaveAttribute("aria-expanded", "false");

    await more.click();
    await expect(menu).toBeVisible();
    await expect(more).toHaveAttribute("aria-expanded", "true");

    // Escape closes popup and state together.
    await page.keyboard.press("Escape");
    await expect(menu).toBeHidden();
    await expect(more).toHaveAttribute("aria-expanded", "false");

    // One menu at a time: opening a second tile closes the first.
    await more.click();
    await expect(menu).toBeVisible();
    await moreFor(page, "config").click();
    await expect(menu).toBeHidden();
    await expect(
      page.getByRole("menu", { name: "More actions for config" }),
    ).toBeVisible();
    await expect(moreFor(page, "config")).toHaveAttribute("aria-expanded", "true");
  });
});

test.describe("touch targets (JUS-145)", () => {
  // hasTouch makes Chromium report `(hover: none)` / `(pointer: coarse)`.
  test.use({ hasTouch: true });

  test("list and grid ⋮ are >= 44px and open with one tap at 390px", async ({
    page,
  }) => {
    await openFiles(page, { viewport: { width: 390, height: 844 } });

    const more = moreFor(page, "app.yaml");
    const box = await more.boundingBox();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.height).toBeGreaterThanOrEqual(44);

    await more.tap();
    await expect(
      page.getByRole("menu", { name: "More actions for app.yaml" }),
    ).toBeVisible();
    await expect(more).toHaveAttribute("aria-expanded", "true");
    await page.keyboard.press("Escape");

    await page.getByRole("button", { name: "Grid view" }).click();
    const tileMore = moreFor(page, "app.yaml");
    const tileBox = await tileMore.boundingBox();
    expect(tileBox!.width).toBeGreaterThanOrEqual(44);
    expect(tileBox!.height).toBeGreaterThanOrEqual(44);

    await tileMore.tap();
    await expect(
      page.getByRole("menu", { name: "More actions for app.yaml" }),
    ).toBeVisible();
    await expect(tileMore).toHaveAttribute("aria-expanded", "true");
  });
});
