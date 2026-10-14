import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, window:readonly */
// JUS-137 windowed listing proof: the committed webdist is served statically
// with the control-plane file API mocked in real Chromium. Measured claims on a
// 5,000-entry directory:
//   - only the visible rows (plus overscan) are in the DOM — a bounded node
//     count — while the footer, the aria row count and select-all still speak
//     for the whole listing;
//   - scrolling materialises the far rows and drops the near ones, and the
//     sticky header stays pinned across the window;
//   - selection (select-all, ctrl/cmd-click, per-row checkbox), activation
//     (Enter / double-click) and the row menu (Shift+F10) work on rendered rows
//     inside a windowed list;
//   - a renamed entry that sorts to the far end is scrolled back into view even
//     though it was outside the rendered window;
//   - the grid view windows the same way at its own row height.

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

/** COUNT is the large-directory size the issue names (5,000 entries). */
const COUNT = 5000;

/** bigListing is one folder row plus 5,000 files (the folder sorts first). */
function bigListing() {
  return [
    {
      name: "aaa-dir",
      kind: "directory",
      size: 4096,
      mode: 0o040755,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"d"',
    },
    ...Array.from({ length: COUNT - 1 }, (_, index) => ({
      name: `file-${String(index).padStart(4, "0")}.txt`,
      kind: "file",
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

/** Mutable listing so a rename is visible on the next list call. */
type Listing = ReturnType<typeof bigListing>;

/** openFiles seeds a session, mocks the file API and lands on the big folder. */
async function openFiles(page: Page): Promise<{ listing: Listing }> {
  const listing: Listing = bigListing();
  const state = { listing };
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
    const path = new URL(route.request().url()).searchParams.get("path") ?? "";
    if (path === "aaa-dir") {
      await route.fulfill({
        json: {
          entries: [
            {
              name: "hosts",
              kind: "file",
              size: 12,
              mode: 0o100644,
              modified_at: "2026-10-12T09:02:00Z",
              etag: '"h"',
            },
          ],
          truncated: false,
          cursor: "",
        },
      });
      return;
    }
    await route.fulfill({
      json: { entries: state.listing, truncated: false, cursor: "" },
    });
  });
  await page.route(`**/api/v1/servers/${server1}/files/rename`, async (route) => {
    const body = route.request().postDataJSON() as {
      source: string;
      destination: string;
    };
    const from = body.source.split("/").pop() ?? "";
    const to = body.destination.split("/").pop() ?? "";
    state.listing = state.listing.map((entry) =>
      entry.name === from ? { ...entry, name: to } : entry,
    );
    const renamed = state.listing.find((entry) => entry.name === to) ?? null;
    await route.fulfill({ json: { entry: renamed } });
  });

  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)").first()).toBeVisible();
  return { listing };
}

/** rows is the rendered (windowed) list rows. */
function rows(page: Page) {
  return page.locator(".fm-table .fm-row:not(.fm-head)");
}

/** rowFor returns the browser row whose name matches. */
function rowFor(page: Page, name: string) {
  return page.locator(".fm-table .fm-row", {
    has: page.locator(`b:text-is("${name}")`),
  });
}

test.describe("windowed list", () => {
  test("a 5,000-entry folder renders a bounded DOM and keeps full-count semantics", async ({
    page,
  }) => {
    await openFiles(page);

    // Only the visible window is rendered, nowhere near 5,000 rows.
    const rendered = await rows(page).count();
    expect(rendered).toBeGreaterThan(0);
    expect(rendered).toBeLessThan(60);

    // The listing itself still reports every entry.
    await expect(page.locator(".fm-foot")).toContainText("5000 entries");
    await expect(page.locator(".fm-table")).toHaveAttribute(
      "aria-rowcount",
      String(COUNT + 1),
    );
  });

  test("scrolling materialises far rows and drops near ones, header stays pinned", async ({
    page,
  }) => {
    await openFiles(page);

    // Top of the list: the first entry is rendered, the last is not.
    await expect(rowFor(page, "aaa-dir")).toBeVisible();
    await expect(page.locator(".fm-table b", { hasText: "file-4998.txt" })).toHaveCount(0);

    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await page.waitForTimeout(100);

    // Bottom: the last entry is rendered, the first dropped from the DOM.
    await expect(page.locator(".fm-table b", { hasText: "file-4998.txt" })).toBeVisible();
    await expect(rowFor(page, "aaa-dir")).toHaveCount(0);
    expect(await rows(page).count()).toBeLessThan(60);

    // The sticky header rode the top of the scrollport instead of scrolling away.
    await wrap.evaluate((element) => {
      element.scrollTop = 4000;
    });
    await page.waitForTimeout(100);
    const wrapBox = (await wrap.boundingBox())!;
    const headBox = (await page.locator(".fm-head").boundingBox())!;
    expect(headBox.y).toBeGreaterThanOrEqual(wrapBox.y - 1);
    expect(headBox.y).toBeLessThan(wrapBox.y + 4);
  });

  test("select-all selects every entry, not just the rendered ones", async ({ page }) => {
    await openFiles(page);
    expect(await rows(page).count()).toBeLessThan(60);

    await page.locator(".fm-head .n-checkbox").click();
    // 5,000 selected though only the window is in the DOM.
    await expect(page.locator(".fm-bulk-count")).toContainText("5000 selected");
    // The reserved bulk-actions slot is now visible.
    await expect(page.locator(".fm-selection-actions")).toHaveCSS("visibility", "visible");

    await page.getByRole("button", { name: "Clear", exact: true }).click();
    await expect(page.locator(".fm-bulk-count")).toHaveText("");
  });

  test("ctrl/cmd-click and the per-row checkbox select inside the window", async ({
    page,
  }) => {
    await openFiles(page);
    const first = rowFor(page, "file-0000.txt");

    // Cmd+click (Ctrl on other platforms) toggles without a context menu.
    await first.click({ modifiers: ["Meta"] });
    await expect(page.locator(".fm-bulk-count")).toContainText("1 selected");

    const second = rowFor(page, "file-0005.txt");
    await second.hover();
    await second.locator(".n-checkbox").click();
    await expect(page.locator(".fm-bulk-count")).toContainText("2 selected");
  });

  test("Enter and double-click activate a rendered row", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").locator(".fm-label").focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".fm-foot")).toContainText("1 entries");
    await expect(page.locator(".fm-label").filter({ hasText: "hosts" })).toBeVisible();
  });

  test("double-click opens a rendered row", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").locator(".fm-label").dblclick();
    await expect(page.locator(".fm-label").filter({ hasText: "hosts" })).toBeVisible();
  });

  test("Shift+F10 opens the row menu on a focused windowed row", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "file-0000.txt").locator(".fm-label").focus();
    await page.keyboard.press("Shift+F10");
    await expect(
      page.getByRole("menu", { name: "More actions for file-0000.txt" }),
    ).toBeVisible();
  });

  test("the filter narrows the listing and the footer counts all of it", async ({
    page,
  }) => {
    await openFiles(page);
    await page.locator(".fm-search input").fill("file-4998");
    await expect(page.locator(".fm-foot")).toContainText("1 of 5000 entries");
    await expect(rowFor(page, "file-4998.txt")).toBeVisible();
  });

  test("sorting reorders the whole listing before it is windowed", async ({ page }) => {
    await openFiles(page);
    // Name asc: the first file row is the smallest name.
    await expect(rows(page).nth(1)).toContainText("file-0000.txt");
    // Size desc: the largest file sorts first, taken from the far end of 5,000.
    await page.getByRole("button", { name: "Size" }).click();
    await page.getByRole("button", { name: "Size" }).click();
    await expect(rows(page).nth(1)).toContainText("file-4998.txt");
    expect(await rows(page).count()).toBeLessThan(60);
  });

  test("a renamed row that sorts out of the window is scrolled back into view", async ({
    page,
  }) => {
    await openFiles(page);
    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await page.waitForTimeout(100);

    // Rename the bottom row to a name that now sorts to the very top.
    await rowFor(page, "file-4998.txt").getByRole("button", { name: /More actions/ }).click();
    await page.getByRole("menuitem", { name: "Rename", exact: true }).click();
    const dialog = page.locator(".n-modal").filter({ hasText: "Rename entry" });
    await dialog.locator("input").fill("0000-renamed.txt");
    await dialog.getByRole("button", { name: "Rename", exact: true }).click();

    // The browser scrolled the far-away row back into the visible band.
    await expect(page.locator(".fm-table b").filter({ hasText: "0000-renamed.txt" })).toBeVisible();
    const top = await wrap.evaluate((element) => element.scrollTop);
    expect(top).toBeLessThan(120);
  });
});

test.describe("windowed grid", () => {
  test("the grid view renders a bounded tile set and materialises the far end", async ({
    page,
  }) => {
    await openFiles(page);
    // Scroll the list far down first: a view switch must not carry that offset
    // into the grid and render a blank window.
    await page.locator(".table-wrap").evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await page.waitForTimeout(100);
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();
    expect(await page.locator(".fm-grid .fm-tile").count()).toBeGreaterThan(0);

    const rendered = await page.locator(".fm-grid .fm-tile").count();
    expect(rendered).toBeLessThan(120);

    const grid = page.locator(".fm-grid");
    await grid.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await page.waitForTimeout(100);
    await expect(
      page.locator(".fm-grid").getByText("file-4998.txt", { exact: true }),
    ).toBeVisible();
    expect(await page.locator(".fm-grid .fm-tile").count()).toBeLessThan(120);
  });
});
