import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, getComputedStyle:readonly, HTMLElement:readonly, URL, window:readonly */

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
    role: "owner",
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

const COUNT = 5000;

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

function folderListing(): Listing {
  return [
    ...Array.from({ length: 149 }, (_, index) => ({
      name: `aaa-${String(index).padStart(4, "0")}.txt`,
      kind: "file" as const,
      size: 100 + index,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: `"f${index}"`,
    })),
    {
      name: "file-0000.txt",
      kind: "file" as const,
      size: 7,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"fg"',
    },
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

type Listing = ReturnType<typeof bigListing>;

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
        json: { entries: folderListing(), truncated: false, cursor: "" },
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
  await page.route(`**/api/v1/servers/${server1}/files/mkdir`, async (route) => {
    const body = route.request().postDataJSON() as { path: string };
    const name = body.path.split("/").pop() ?? "";
    const created = {
      name,
      kind: "directory" as const,
      size: 4096,
      mode: 0o040755,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"new"',
    };
    state.listing = [...state.listing, created];
    await route.fulfill({ json: { entry: created } });
  });

  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)").first()).toBeVisible();
  return { listing };
}

function rows(page: Page) {
  return page.locator(".fm-table .fm-row:not(.fm-head)");
}

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

    const rendered = await rows(page).count();
    expect(rendered).toBeGreaterThan(0);
    expect(rendered).toBeLessThan(60);

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

    await expect(rowFor(page, "aaa-dir")).toBeVisible();
    await expect(page.locator(".fm-table b", { hasText: "file-4998.txt" })).toHaveCount(0);

    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });

    await expect(page.locator(".fm-table b", { hasText: "file-4998.txt" })).toBeVisible();
    await expect(rowFor(page, "aaa-dir")).toHaveCount(0);
    expect(await rows(page).count()).toBeLessThan(60);

    await wrap.evaluate((element) => {
      element.scrollTop = 4000;
    });
    await expect.poll(() => wrap.evaluate((element) => element.scrollTop)).toBeGreaterThan(3000);
    const wrapBox = (await wrap.boundingBox())!;
    const headBox = (await page.locator(".fm-head").boundingBox())!;
    expect(headBox.y).toBeGreaterThanOrEqual(wrapBox.y - 1);
    expect(headBox.y).toBeLessThan(wrapBox.y + 4);
  });

  test("select-all selects every entry, not just the rendered ones", async ({ page }) => {
    await openFiles(page);
    expect(await rows(page).count()).toBeLessThan(60);

    await page.locator(".fm-head .n-checkbox").click();
    await expect(page.locator(".fm-bulk-count")).toContainText("5000 selected");
    await expect(page.locator(".fm-selection-actions")).toHaveCSS("visibility", "visible");

    await page.getByRole("button", { name: "Clear", exact: true }).click();
    await expect(page.locator(".fm-bulk-count-live")).toHaveText("");
    await expect(page.locator(".fm-bulk-count-live")).toHaveCSS("visibility", "hidden");
  });

  test("ctrl/cmd-click and the per-row checkbox select inside the window", async ({
    page,
  }) => {
    await openFiles(page);
    const first = rowFor(page, "file-0000.txt");

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
    await expect(page.locator(".fm-foot")).toContainText("150 entries");
    await expect(
      page.locator(".fm-label").filter({ hasText: "aaa-0000.txt" }),
    ).toBeVisible();
  });

  test("double-click opens a rendered row", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").locator(".fm-label").dblclick();
    await expect(
      page.locator(".fm-label").filter({ hasText: "aaa-0000.txt" }),
    ).toBeVisible();
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

  test("filtering while scrolled deep clamps the offset and still renders rows", async ({
    page,
  }) => {
    await openFiles(page);
    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect.poll(() => wrap.evaluate((element) => element.scrollTop)).toBeGreaterThan(1000);

    await page.locator(".fm-search input").fill("file-0");
    await expect(page.locator(".fm-foot")).toContainText("1000 of 5000 entries");
    await expect.poll(() => rows(page).count()).toBeGreaterThan(0);
    await expect(rows(page).first()).toBeVisible();
  });

  test("sorting reorders the whole listing before it is windowed", async ({ page }) => {
    await openFiles(page);
    await expect(rows(page).nth(1)).toContainText("file-0000.txt");
    await page.getByRole("button", { name: "Size", exact: true }).click();
    await page.getByRole("button", { name: "Size", exact: true }).click();
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

    await rowFor(page, "file-4998.txt").getByRole("button", { name: /More actions/ }).click();
    await page.getByRole("menuitem", { name: "Rename", exact: true }).click();
    const dialog = page.locator(".n-modal").filter({ hasText: "Rename entry" });
    await dialog.locator("input").fill("0000-renamed.txt");
    await dialog.getByRole("button", { name: "Rename", exact: true }).click();

    await expect(page.locator(".fm-table b").filter({ hasText: "0000-renamed.txt" })).toBeVisible();
    const top = await wrap.evaluate((element) => element.scrollTop);
    expect(top).toBeLessThan(120);
  });

  test("a created row that sorts out of the window is scrolled into view", async ({
    page,
  }) => {
    await openFiles(page);
    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect(rowFor(page, "file-4998.txt")).toBeVisible();

    await page.getByRole("button", { name: "New", exact: true }).click();
    await page.getByRole("menuitem", { name: "New folder", exact: true }).click();
    const dialog = page.locator(".n-modal").filter({ hasText: "New folder" });
    await dialog.locator("input").fill("0000-created");
    await dialog.getByRole("button", { name: "Create folder", exact: true }).click();

    await expect(
      page.locator(".fm-table b").filter({ hasText: "0000-created" }),
    ).toBeVisible();
    await expect.poll(() => wrap.evaluate((element) => element.scrollTop)).toBeLessThan(120);
  });
});

test.describe("keyboard focus retention across the window", () => {
  async function renderedNames(page: Page): Promise<string[]> {
    return rows(page).evaluateAll((nodes) =>
      nodes.map((node) => (node as HTMLElement).dataset.entry ?? ""),
    );
  }

  async function focusedEntry(page: Page): Promise<string | null> {
    return page.evaluate(() => {
      const active = document.activeElement as HTMLElement | null;
      return active?.closest<HTMLElement>("[data-entry]")?.dataset.entry ?? null;
    });
  }

  test("a focused row keeps focus and its outline when scrolled out of the window", async ({
    page,
  }) => {
    await openFiles(page);
    const label = rowFor(page, "file-0000.txt").locator(".fm-label");
    await label.focus();
    await expect(label).toBeFocused();

    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect.poll(() => wrap.evaluate((element) => element.scrollTop)).toBeGreaterThan(1000);
    await expect(page.locator(".fm-row.is-pinned")).toHaveCount(1);
    await expect(label).toBeFocused();
    await expect(label).toBeAttached();

    await wrap.evaluate((element) => {
      element.scrollTop = 0;
    });
    await expect(label).toBeFocused();
    await expect(label).toBeVisible();
  });

  test("Tab from the last rendered row reaches the next entry and scrolls it in", async ({
    page,
  }) => {
    await openFiles(page);
    const initial = await renderedNames(page);
    expect(initial.length).toBeGreaterThan(0);

    await rows(page).last().getByRole("button", { name: /More actions/ }).focus();
    await expect
      .poll(async () => (await renderedNames(page)).some((name) => !initial.includes(name)))
      .toBe(true);

    await page.keyboard.press("Tab");

    const next = await focusedEntry(page);
    expect(next).not.toBeNull();
    expect(initial).not.toContain(next);
    await expect(
      rows(page).filter({ has: page.locator(`b:text-is("${next}")`) }),
    ).toBeVisible();
  });

  test("the row menu stays open on its row while the list scrolls", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "file-0000.txt").locator(".fm-label").focus();
    await page.keyboard.press("Shift+F10");
    const menu = page.getByRole("menu", { name: "More actions for file-0000.txt" });
    await expect(menu).toBeVisible();

    await page.locator(".table-wrap").evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect(menu).toBeVisible();
    await expect(rowFor(page, "file-0000.txt")).toHaveCount(1);
  });

  test("a click on another row's non-focusable cell does not steal focus back", async ({
    page,
  }) => {
    await openFiles(page);
    const label = rowFor(page, "aaa-dir").locator(".fm-label");
    await label.focus();
    await expect(label).toBeFocused();

    await rowFor(page, "file-0000.txt").locator(".fm-cell--size").click();
    await expect.poll(() => focusedEntry(page)).not.toBe("aaa-dir");
    await expect(page.locator(".fm-row.is-pinned")).toHaveCount(0);

    await page.keyboard.press("Enter");
    await expect(page.locator(".fm-foot")).toContainText("5000 entries");
    await expect(rowFor(page, "aaa-dir")).toBeVisible();
  });

  test("a click on blank space does not steal focus back", async ({ page }) => {
    await openFiles(page);
    const label = rowFor(page, "file-0000.txt").locator(".fm-label");
    await label.focus();
    await expect(label).toBeFocused();

    await page.locator(".fm-foot .grow").click();
    await expect.poll(() => focusedEntry(page)).not.toBe("file-0000.txt");
    await expect(page.locator(".fm-row.is-pinned")).toHaveCount(0);
  });

  test("dismissing the row menu with Escape releases the pin", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").click({ button: "right" });
    const menu = page.getByRole("menu", { name: "More actions for aaa-dir" });
    await expect(menu).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(menu).toHaveCount(0);

    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect.poll(() => wrap.evaluate((element) => element.scrollTop)).toBeGreaterThan(1000);
    await expect(page.locator(".fm-row.is-pinned")).toHaveCount(0);
    await expect(rowFor(page, "aaa-dir")).toHaveCount(0);
  });

  test("a folder change resets the focus pin", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "file-0000.txt").locator(".fm-label").focus();
    await expect(rowFor(page, "file-0000.txt").locator(".fm-label")).toBeFocused();

    await rowFor(page, "aaa-dir").click({ button: "right" });
    const menu = page.getByRole("menu", { name: "More actions for aaa-dir" });
    await expect(menu).toBeVisible();
    await menu.getByRole("menuitem", { name: "Open", exact: true }).click();

    await expect(page.locator(".fm-foot")).toContainText("150 entries");
    await expect(page.locator(".fm-row.is-pinned")).toHaveCount(0);
    await expect(
      page.locator(".fm-table b").filter({ hasText: "file-0000.txt" }),
    ).toHaveCount(0);
  });

  test("a pointer focus spends the no-scroll flag so a later programmatic focus reveals", async ({
    page,
  }) => {
    await openFiles(page);
    const wrap = page.locator(".table-wrap");

    await rowFor(page, "file-0000.txt").locator(".fm-label").click();
    expect(await wrap.evaluate((el) => el.scrollTop)).toBe(0);

    await page.evaluate(() => {
      const rows = document.querySelectorAll<HTMLElement>(
        ".fm-table .fm-row:not(.fm-head)",
      );
      const last = rows[rows.length - 1];
      (last.querySelector(".fm-label") as HTMLElement).focus({
        preventScroll: true,
      });
    });
    await expect.poll(() => wrap.evaluate((el) => el.scrollTop)).toBeGreaterThan(0);
  });

  test("focusing an in-scroller header control clears the stale pin", async ({
    page,
  }) => {
    await openFiles(page);
    const label = rowFor(page, "file-0000.txt").locator(".fm-label");
    await label.focus();
    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect.poll(() => wrap.evaluate((el) => el.scrollTop)).toBeGreaterThan(1000);
    await expect(page.locator(".fm-row.is-pinned")).toHaveCount(1);

    await page.getByRole("button", { name: "Name", exact: true }).focus();
    await expect(page.locator(".fm-row.is-pinned")).toHaveCount(0);

    await wrap.evaluate((element) => {
      element.scrollTop = 0;
    });
    await expect(page.locator(".fm-row.is-pinned")).toHaveCount(0);
  });
});

test.describe("windowed grid", () => {
  test("the grid view renders a bounded tile set and materialises the far end", async ({
    page,
  }) => {
    await openFiles(page);
    await page.locator(".table-wrap").evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();

    const rendered = await page.locator(".fm-grid .fm-tile").count();
    expect(rendered).toBeGreaterThan(0);
    expect(rendered).toBeLessThan(80);

    const grid = page.locator(".fm-grid");
    const metrics = await grid.evaluate((element) => ({
      scrollHeight: element.scrollHeight,
      clientHeight: element.clientHeight,
    }));
    expect(metrics.scrollHeight).toBeGreaterThan(metrics.clientHeight * 100);

    await grid.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect(
      page.locator(".fm-grid").getByText("file-4998.txt", { exact: true }),
    ).toBeVisible();
    expect(await page.locator(".fm-grid .fm-tile").count()).toBeLessThan(80);
  });

  test("a focused tile keeps focus when scrolled out of the grid window", async ({
    page,
  }) => {
    await openFiles(page);
    await page.getByRole("button", { name: "Grid view" }).click();
    const tile = page
      .locator(".fm-tile-wrap", { has: page.locator('b:text-is("aaa-dir")') })
      .locator(".fm-tile");
    await tile.focus();
    await expect(tile).toBeFocused();

    const grid = page.locator(".fm-grid");
    await grid.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect.poll(() => grid.evaluate((element) => element.scrollTop)).toBeGreaterThan(1000);

    await expect(page.locator(".fm-tile-wrap.is-pinned")).toHaveCount(1);
    await expect(tile).toBeFocused();
    const gridBox = (await grid.boundingBox())!;
    const tileBox = (await tile.boundingBox())!;
    expect(tileBox.x).toBeGreaterThanOrEqual(gridBox.x - 1);
    expect(tileBox.x + tileBox.width).toBeLessThanOrEqual(gridBox.x + gridBox.width + 1);
  });

  test("the grid exposes the full set size to assistive tech", async ({ page }) => {
    await openFiles(page);
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();

    await expect(page.locator(".fm-grid")).toHaveAttribute("role", "list");
    const tiles = page.locator(".fm-grid .fm-tile-wrap");
    expect(await tiles.count()).toBeLessThan(80);
    await expect(tiles.first()).toHaveAttribute("role", "listitem");
    await expect(tiles.first()).toHaveAttribute("aria-setsize", String(COUNT));
    await expect(tiles.first()).toHaveAttribute("aria-posinset", "1");
    await expect(tiles.last()).toHaveAttribute("aria-setsize", String(COUNT));
  });

  test("the grid column count follows an in-session resize and zoom", async ({
    page,
  }) => {
    await openFiles(page);
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();

    const measured = () =>
      page.evaluate(() => {
        const grid = document.querySelector(".fm-grid") as HTMLElement;
        const style = getComputedStyle(grid);
        const columns = style.gridTemplateColumns.trim().split(/\s+/).length;
        const gap = parseFloat(style.columnGap);
        const pitch = parseFloat(style.getPropertyValue("--fm-tile-height")) + gap;
        return {
          scrollHeight: grid.scrollHeight,
          expected: Math.ceil(5000 / columns) * pitch - gap,
        };
      });
    const expectFollows = async () => {
      await expect
        .poll(async () => {
          const { scrollHeight, expected } = await measured();
          return Math.abs(scrollHeight - expected);
        })
        .toBeLessThan(400);
    };

    await page.setViewportSize({ width: 900, height: 900 });
    await expectFollows();
    await page.setViewportSize({ width: 600, height: 900 });
    await expectFollows();
    await page.setViewportSize({ width: 1440, height: 900 });
    await expectFollows();

    await page.evaluate(() => {
      document.documentElement.style.zoom = "1.5";
    });
    await expectFollows();
    await page.evaluate(() => {
      document.documentElement.style.zoom = "";
    });
    await expectFollows();
  });
});

test.describe("row keyboard navigation (JUS-139)", () => {
  async function focusedEntry(page: Page): Promise<string | null> {
    return page.evaluate(() => {
      const active = document.activeElement as HTMLElement | null;
      return active?.closest<HTMLElement>("[data-entry]")?.dataset.entry ?? null;
    });
  }

  test("arrows move the roving focus; Home/End/Page reach far rows", async ({
    page,
  }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").locator(".fm-label").focus();

    await page.keyboard.press("ArrowDown");
    await expect.poll(() => focusedEntry(page)).toBe("file-0000.txt");
    await page.keyboard.press("ArrowDown");
    await expect.poll(() => focusedEntry(page)).toBe("file-0001.txt");
    await page.keyboard.press("ArrowUp");
    await expect.poll(() => focusedEntry(page)).toBe("file-0000.txt");

    await page.keyboard.press("PageDown");
    await expect.poll(() => focusedEntry(page)).not.toBe("file-0000.txt");
    const paged = await focusedEntry(page);
    expect(paged).not.toBeNull();

    await page.keyboard.press("End");
    await expect.poll(() => focusedEntry(page)).toBe("file-4998.txt");
    await expect(
      page.locator(".fm-table b").filter({ hasText: "file-4998.txt" }),
    ).toBeVisible();

    await page.keyboard.press("Home");
    await expect.poll(() => focusedEntry(page)).toBe("aaa-dir");
  });

  test("Space toggles the focused row and Ctrl/Cmd+A selects all", async ({
    page,
  }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").locator(".fm-label").focus();

    await page.keyboard.press(" ");
    await expect(page.locator(".fm-bulk-count")).toContainText("1 selected");
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-bulk-count-live")).toHaveText("");

    await page.keyboard.press("ControlOrMeta+a");
    await expect(page.locator(".fm-bulk-count")).toContainText("5000 selected");
    await expect(
      page.locator(".fm-table .fm-row.is-selected").first(),
    ).toBeVisible();
  });

  test("Shift+Arrow extends a range from the keyboard anchor", async ({
    page,
  }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").locator(".fm-label").focus();

    await page.keyboard.press("ArrowDown");
    await expect.poll(() => focusedEntry(page)).toBe("file-0000.txt");
    await page.keyboard.press("Shift+ArrowDown");
    await page.keyboard.press("Shift+ArrowDown");

    await expect(page.locator(".fm-bulk-count")).toContainText("3 selected");
    await expect(
      page.locator('.fm-row[data-entry="file-0000.txt"].is-selected'),
    ).toHaveCount(1);
    await expect(
      page.locator('.fm-row[data-entry="file-0002.txt"].is-selected'),
    ).toHaveCount(1);
  });

  test("type-to-focus jumps to a prefix across unrendered rows", async ({
    page,
  }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").locator(".fm-label").focus();

    await page.keyboard.type("file-4998", { delay: 20 });
    await expect.poll(() => focusedEntry(page)).toBe("file-4998.txt");
    await expect(
      page.locator(".fm-table b").filter({ hasText: "file-4998.txt" }),
    ).toBeVisible();
  });

  test("the grid adds Left/Right and moves a visual row with Up/Down", async ({
    page,
  }) => {
    await openFiles(page);
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();
    await expect(
      page.locator('.fm-tile-wrap[data-entry="file-0000.txt"]'),
    ).toBeVisible();

    await page.locator('.fm-tile-wrap[data-entry="aaa-dir"] .fm-tile').focus();
    await page.keyboard.press("ArrowRight");
    await expect.poll(() => focusedEntry(page)).toBe("file-0000.txt");
    await page.keyboard.press("ArrowDown");
    await expect.poll(() => focusedEntry(page)).not.toBe("file-0000.txt");
    await page.keyboard.press("ArrowUp");
    await expect.poll(() => focusedEntry(page)).toBe("file-0000.txt");
    await page.keyboard.press("ArrowLeft");
    await expect.poll(() => focusedEntry(page)).toBe("aaa-dir");
  });

  test("one roving tab stop, aria state and the focus ring stay correct", async ({
    page,
  }) => {
    await openFiles(page);
    await expect(
      page.locator('.fm-table .fm-label[tabindex="0"]'),
    ).toHaveCount(1);
    const total = await page.locator(".fm-table .fm-label").count();
    expect(await page.locator('.fm-table .fm-label[tabindex="-1"]').count()).toBe(
      total - 1,
    );

    await rowFor(page, "aaa-dir").locator(".fm-label").focus();
    await page.keyboard.press("ArrowDown");
    await expect.poll(() => focusedEntry(page)).toBe("file-0000.txt");
    await expect(
      page.locator('.fm-row[data-entry="file-0000.txt"] .fm-label'),
    ).toHaveAttribute("tabindex", "0");
    await expect(
      page.locator('.fm-row[data-entry="aaa-dir"] .fm-label'),
    ).toHaveAttribute("tabindex", "-1");

    await page.keyboard.press(" ");
    await expect(
      page.locator('.fm-row[data-entry="file-0000.txt"]'),
    ).toHaveAttribute("aria-selected", "true");
    await expect(page.locator(".fm-table")).toHaveAttribute(
      "aria-rowcount",
      String(COUNT + 1),
    );

    const focused = page.locator('.fm-row[data-entry="file-0000.txt"] .fm-label');
    const outline = await focused.evaluate(
      (element) => getComputedStyle(element).outlineStyle,
    );
    expect(outline).not.toBe("none");
  });

  test("the header sort controls keep their native Enter/Space", async ({
    page,
  }) => {
    await openFiles(page);
    const nameHeader = page.locator('.fm-head .fm-name[aria-sort]');
    await expect(nameHeader).toHaveAttribute("aria-sort", "ascending");

    await page.getByRole("button", { name: "Name", exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(nameHeader).toHaveAttribute("aria-sort", "descending");
    await expect(page.locator(".fm-foot")).toContainText("5000 entries");
    await expect(rowFor(page, "aaa-dir")).toBeVisible();

    await page.getByRole("button", { name: "Size", exact: true }).focus();
    await page.keyboard.press(" ");
    await expect(page.locator('.fm-head .fm-cell--size[aria-sort]')).toHaveAttribute(
      "aria-sort",
      "ascending",
    );
    await expect(page.locator(".fm-bulk-count-live")).toHaveText("");

    await page.locator(".fm-head .n-checkbox").focus();
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-bulk-count")).toContainText("5000 selected");
  });

  test("blur + scroll away still leaves exactly one roving tab stop", async ({
    page,
  }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").locator(".fm-label").focus();
    await page.keyboard.press("ArrowDown");
    await expect.poll(() => focusedEntry(page)).toBe("file-0000.txt");
    await expect(page.locator('.fm-label[tabindex="0"]')).toHaveCount(1);

    await page.locator(".fm-foot .grow").click();
    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect.poll(() => wrap.evaluate((el) => el.scrollTop)).toBeGreaterThan(
      1000,
    );
    await expect(page.locator('.fm-row[data-entry="file-0000.txt"]')).toHaveCount(0);
    await expect(page.locator('.fm-label[tabindex="0"]')).toHaveCount(1);

    await wrap.evaluate((element) => {
      element.scrollTop = 0;
    });
    await expect(page.locator('.fm-label[tabindex="0"]')).toHaveCount(1);
    await expect(
      page.locator('.fm-row[data-entry="file-0000.txt"] .fm-label'),
    ).toHaveAttribute("tabindex", "0");
  });

  test("blur + scroll away still leaves exactly one roving tab stop in the grid", async ({
    page,
  }) => {
    await openFiles(page);
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();
    await expect(
      page.locator('.fm-tile-wrap[data-entry="file-0000.txt"]'),
    ).toBeVisible();
    await page.locator('.fm-tile-wrap[data-entry="aaa-dir"] .fm-tile').focus();
    await page.keyboard.press("ArrowRight");
    await expect.poll(() => focusedEntry(page)).toBe("file-0000.txt");
    await expect(page.locator('.fm-tile[tabindex="0"]')).toHaveCount(1);

    await page.locator(".fm-foot .grow").click();
    const grid = page.locator(".fm-grid");
    await grid.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    await expect.poll(() => grid.evaluate((el) => el.scrollTop)).toBeGreaterThan(
      1000,
    );
    await expect(page.locator('.fm-tile[tabindex="0"]')).toHaveCount(1);
  });

  test("a folder change re-resolves the roving stop", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "aaa-dir").locator(".fm-label").focus();
    await page.keyboard.press("ArrowDown");
    await expect.poll(() => focusedEntry(page)).toBe("file-0000.txt");

    await rowFor(page, "aaa-dir").locator(".fm-label").dblclick();
    await expect(page.locator(".fm-foot")).toContainText("150 entries");
    await expect(page.locator('.fm-label[tabindex="0"]')).toHaveCount(1);
    await expect(
      page.locator('.fm-row[data-entry="aaa-0000.txt"] .fm-label'),
    ).toHaveAttribute("tabindex", "0");
    await expect(
      page.locator('.fm-row[data-entry="file-0000.txt"] .fm-label'),
    ).toHaveCount(0);
  });
});
