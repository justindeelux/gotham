import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, document:readonly, window:readonly, HTMLElement:readonly, getComputedStyle:readonly */
// JUS-145 proof, round 1: at EVERY width where the list overflows horizontally
// the row/tile menu trigger is on screen and hit-testable (the actions cell is
// pinned sticky-right), the compact layout keeps Modified visible and the
// Modified/Owner-Mode columns sortable, and the windowed list keeps its exact
// scroll maths. The committed webdist is served statically with the
// control-plane file API mocked in real Chromium.

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

/**
 * reachWidths covers the phone, tablet, the previously-broken 861..1400 band
 * (where the table overflows but the compact columns have not kicked in) and
 * desktop. `run.sock` (a special row) is the only row without a menu.
 */
const reachWidths = [390, 540, 768, 861, 1024, 1280, 1440];

/** listing is a small folder with one of every entry kind (12 rows). */
function listing() {
  return [
    { name: "app.yaml", kind: "file" as const, size: 2458, mode: 0o100644, modified_at: "2026-10-12T09:02:00Z", etag: '"a"' },
    { name: "backup.tar.gz", kind: "file" as const, size: 4096, mode: 0o100644, modified_at: "2026-10-12T09:10:00Z", etag: '"b"' },
    { name: "config", kind: "directory" as const, size: 0, mode: 0o040755, modified_at: "2026-10-12T09:14:00Z", etag: '"c"' },
    { name: "link-to-data", kind: "symlink" as const, size: 0, mode: 0o120777, modified_at: "2026-10-12T09:16:00Z", symlink_target: "data", etag: '"s"' },
    { name: "run.sock", kind: "special" as const, size: 0, mode: 0o140777, modified_at: "2026-10-12T09:19:00Z", etag: '"d"' },
    ...Array.from({ length: 7 }, (_, index) => ({
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
  const viewport = options.viewport ?? { width: 1440, height: 900 };
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

/** expectNoPageOverflow proves the page itself never scrolls sideways. */
async function expectNoPageOverflow(page: Page): Promise<void> {
  const doc = await page.evaluate(() => {
    const el = document.documentElement;
    return el.scrollWidth - el.clientWidth;
  });
  expect(doc, "page horizontal overflow").toBeLessThanOrEqual(1);
}

/** bringIntoView scrolls a row/tile into the VERTICAL view of its scroller only
 * (never sideways), so the hit test runs on a genuinely visible trigger. */
async function bringIntoView(trigger: Locator): Promise<void> {
  await trigger.evaluate((el) => {
    const wrap = el.closest(".table-wrap, .fm-grid") as HTMLElement | null;
    if (wrap === null) {
      return;
    }
    const row = el.getBoundingClientRect();
    const box = wrap.getBoundingClientRect();
    if (row.top < box.top + 2) {
      wrap.scrollTop -= box.top - row.top + 8;
    } else if (row.bottom > box.bottom - 2) {
      wrap.scrollTop += row.bottom - box.bottom + 8;
    }
  });
}

/** expectReachable asserts the trigger is on screen (no sideways scroll) AND the
 * top-most element at its centre is the trigger itself. */
async function expectReachable(trigger: Locator, width: number): Promise<void> {
  await expect(trigger).toBeVisible();
  const box = await trigger.boundingBox();
  expect(box, "trigger has no box").not.toBeNull();
  expect(box!.x, "trigger left edge").toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width, "trigger right edge").toBeLessThanOrEqual(width + 1);
  const hit = await trigger.evaluate((el) => {
    const r = el.getBoundingClientRect();
    const top = document.elementFromPoint(
      r.left + r.width / 2,
      r.top + r.height / 2,
    );
    return top === el || el.contains(top);
  });
  expect(hit, "elementFromPoint hits the trigger").toBe(true);
}

test.describe("row and tile menus are reachable without scrolling (JUS-145)", () => {
  for (const width of reachWidths) {
    test(`list at ${width}px: every row ⋮ is on screen and hit-testable`, async ({
      page,
    }) => {
      await openFiles(page, { viewport: { width, height: 900 } });
      await expectNoPageOverflow(page);

      // Below the compact breakpoint the table must also fit internally, so no
      // sideways scroll is needed at all.
      if (width <= 860) {
        const overflow = await page
          .locator(".table-wrap")
          .evaluate((el) => el.scrollWidth - el.clientWidth);
        expect(overflow, ".table-wrap horizontal overflow").toBeLessThanOrEqual(1);
      }

      const rows = page.locator(".fm-table .fm-row:not(.fm-head)");
      expect(await rows.count()).toBe(12);
      for (let index = 0; index < 12; index += 1) {
        const name = (await rows.nth(index).getAttribute("data-entry")) ?? "";
        // A special row genuinely has no actions, so no trigger.
        if (name === "run.sock") {
          expect(await moreFor(page, name).count()).toBe(0);
          continue;
        }
        const trigger = moreFor(page, name);
        await bringIntoView(trigger);
        await expectReachable(trigger, width);
      }
    });

    test(`grid at ${width}px: every tile ⋮ is on screen and hit-testable`, async ({
      page,
    }) => {
      await openFiles(page, { viewport: { width, height: 900 } });
      await page.getByRole("button", { name: "Grid view" }).click();
      await expectNoPageOverflow(page);

      const tiles = page.locator(".fm-grid .fm-tile-wrap");
      expect(await tiles.count()).toBe(12);
      for (let index = 0; index < 12; index += 1) {
        const name = (await tiles.nth(index).getAttribute("data-entry")) ?? "";
        if (name === "run.sock") {
          expect(await moreFor(page, name).count()).toBe(0);
          continue;
        }
        const trigger = moreFor(page, name);
        await bringIntoView(trigger);
        await expectReachable(trigger, width);
      }
    });
  }

  // JUS-163 fix round 1: the search input, the "Include subdirectories" switch
  // and the Search button share the nav row with Refresh. At every narrow width
  // they must stay inside the viewport (no sideways scroll) — the group is
  // allowed to wrap its switch under the input rather than overflow.
  for (const width of [390, 480, 640]) {
    test(`search controls fit the nav row at ${width}px`, async ({ page }) => {
      await openFiles(page, { viewport: { width, height: 900 } });
      await expectNoPageOverflow(page);

      const groupBox = await page.locator(".fm-search-group").boundingBox();
      expect(groupBox, "search group has no box").not.toBeNull();
      expect(groupBox!.x, "search group left edge").toBeGreaterThanOrEqual(-1);
      expect(
        groupBox!.x + groupBox!.width,
        "search group right edge",
      ).toBeLessThanOrEqual(width + 1);

      for (const selector of [
        ".fm-search input",
        ".fm-search-group .n-checkbox",
        ".fm-search-submit",
      ]) {
        const control = page.locator(selector);
        await expect(control, `${selector} visible`).toBeVisible();
        const box = await control.boundingBox();
        expect(box, `${selector} has no box`).not.toBeNull();
        expect(box!.x, `${selector} left edge`).toBeGreaterThanOrEqual(-1);
        expect(
          box!.x + box!.width,
          `${selector} right edge`,
        ).toBeLessThanOrEqual(width + 1);
      }
    });
  }

  test("the sticky actions cell keeps its row-state background and the header stays pinned", async ({
    page,
  }) => {
    await openFiles(page, { viewport: { width: 1024, height: 768 } });

    // Select a row so the selected tint must follow the pinned cell.
    await page.locator('.fm-table .fm-row:not(.fm-head)[data-entry="config"]').click();
    const cells = await page.evaluate(() => {
      const selected = document.querySelector(
        '.fm-table .fm-row.is-selected > .fm-actions',
      ) as HTMLElement;
      const plain = document.querySelector(
        '.fm-table .fm-row:not(.fm-head):not(.is-selected) > .fm-actions',
      ) as HTMLElement;
      return {
        selectedBg: getComputedStyle(selected).backgroundColor,
        selectedImage: getComputedStyle(selected).backgroundImage,
        plainBg: getComputedStyle(plain).backgroundColor,
        plainImage: getComputedStyle(plain).backgroundImage,
        selectedPos: getComputedStyle(selected).position,
      };
    });
    expect(cells.selectedPos).toBe("sticky");
    // Opaque surface (never transparent, or the columns scrolling under it show
    // through) with the selected tint layered on top.
    expect(cells.selectedBg).not.toBe("rgba(0, 0, 0, 0)");
    expect(cells.selectedBg).toBe(cells.plainBg);
    expect(cells.selectedImage).not.toBe("none");
    expect(cells.plainImage).toBe("none");
  });
});

test.describe("windowed list keeps its maths and reachability (JUS-145)", () => {
  /** scrollMath reads the table's exact scroll height against the row token. */
  async function scrollMath(page: Page) {
    return page.evaluate(() => {
      const wrap = document.querySelector(".table-wrap") as HTMLElement;
      const table = document.querySelector(".fm-table") as HTMLElement;
      const row = document.querySelector(
        ".fm-table .fm-row:not(.fm-head)",
      ) as HTMLElement;
      const head = document.querySelector(".fm-table .fm-head") as HTMLElement;
      return {
        scrollHeight: table.scrollHeight,
        rowHeight: row.getBoundingClientRect().height,
        headerHeight: head.getBoundingClientRect().height,
        rendered: document.querySelectorAll(".fm-table .fm-row:not(.fm-head)")
          .length,
        wrapScrollTop: wrap.scrollTop,
      };
    });
  }

  test("scrollHeight == entries x row height at 42px and 52px, across the breakpoint", async ({
    page,
  }) => {
    await openFiles(page, {
      entries: bigListing(),
      viewport: { width: 861, height: 900 },
    });

    const checked = async (expectedRow: number): Promise<void> => {
      await expect
        .poll(
          async () => {
            const math = await scrollMath(page);
            const expected = COUNT * math.rowHeight + math.headerHeight;
            const fits = Math.abs(math.scrollHeight - expected) <= 2;
            return `${Math.round(math.rowHeight)}:${fits}:${math.rendered < 500}`;
          },
          { timeout: 5000 },
        )
        .toBe(`${expectedRow}:true:true`);
    };

    await checked(42);
    // Cross the compact breakpoint: the token grows to 52px, then back.
    await page.setViewportSize({ width: 860, height: 900 });
    await checked(52);
    await page.setViewportSize({ width: 861, height: 900 });
    await checked(42);
  });

  test("the far end of a 5,000-entry list keeps its ⋮ reachable and hit-testable", async ({
    page,
  }) => {
    await openFiles(page, {
      entries: bigListing(),
      viewport: { width: 390, height: 844 },
    });
    // Scroll to the bottom (vertical only); the pinned actions cell must be on
    // screen and hit-testable there too.
    await page.locator(".table-wrap").evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
    await expect(
      page.locator(".fm-table b", { hasText: "file-4998.txt" }),
    ).toBeVisible();
    await expectReachable(moreFor(page, "file-4998.txt"), 390);
    expect(
      await page.locator(".table-wrap").evaluate((el) => el.scrollLeft),
    ).toBe(0);
  });
});

test.describe("folded row actions and compact sort (JUS-145)", () => {
  test("every inline action has an equivalent in the ⋮ menu at compact width", async ({
    page,
  }) => {
    await openFiles(page, { viewport: { width: 390, height: 844 } });

    const menuLabels = async (name: string): Promise<string[]> => {
      await moreFor(page, name).click();
      const menu = page.getByRole("menu", { name: `More actions for ${name}` });
      await expect(menu).toBeVisible();
      const labels = await menu.getByRole("menuitem").allInnerTexts();
      await page.keyboard.press("Escape");
      return labels.map((text) => text.trim());
    };

    // A file: edit/download/permissions/rename/cut/copy/delete.
    for (const label of [
      "Edit in editor",
      "Download",
      "Permissions…",
      "Rename",
      "Cut",
      "Copy",
      "Delete",
    ]) {
      expect(await menuLabels("app.yaml"), label).toContain(label);
    }
    // An archive keeps Extract here….
    expect(await menuLabels("backup.tar.gz")).toContain("Extract here…");
    // A folder: open/open-in-new-tab/.../paste/delete.
    const folder = await menuLabels("config");
    for (const label of ["Open", "Open in new tab", "Paste", "Delete"]) {
      expect(folder, label).toContain(label);
    }
    // A symlink keeps Go to target.
    expect(await menuLabels("link-to-data")).toContain("Go to target");
    // A special row has no menu at all.
    expect(await moreFor(page, "run.sock").count()).toBe(0);
  });

  test("the compact list shows Modified and the sort control reorders every column", async ({
    page,
  }) => {
    await openFiles(page, { viewport: { width: 390, height: 844 } });

    // Modified is a visible, muted secondary line on the compact row.
    const sub = page.locator(
      '.fm-table .fm-row[data-entry="app.yaml"] .fm-row-sub',
    );
    await expect(sub).toBeVisible();
    await expect(sub).not.toHaveText("");

    const order = (): Promise<string[]> =>
      page
        .locator(".fm-table .fm-row:not(.fm-head)")
        .evaluateAll((els) =>
          els.map((el) => (el as HTMLElement).dataset.entry ?? ""),
        );

    // Headers for Modified / Owner-Mode are hidden, so the toolbar control is
    // the only way to sort by them.
    await expect(page.locator(".fm-head .fm-cell--modified")).toBeHidden();
    await expect(page.locator(".fm-head .fm-cell--mode")).toBeHidden();

    const sortButton = page.getByRole("button", { name: /^Sort:/ });
    await expect(sortButton).toBeVisible();
    await expect(sortButton).toHaveAttribute("aria-label", /Name/);

    // Baseline: Name ascending → app.yaml before backup.tar.gz.
    let names = await order();
    expect(names.indexOf("app.yaml")).toBeLessThan(
      names.indexOf("backup.tar.gz"),
    );

    // Sort by Size ascending: the smallest file before the largest, and the
    // folder still groups first (kind rank is direction-independent).
    await sortButton.click();
    await page.getByRole("menu", { name: "Sort" }).getByRole("menuitem", { name: "Size" }).click();
    await expect(sortButton).toHaveAttribute("aria-label", /Size/);
    names = await order();
    expect(names[0]).toBe("config");
    expect(names.indexOf("report-0000.txt")).toBeLessThan(
      names.indexOf("backup.tar.gz"),
    );

    // Flip the direction: the largest file now comes before the smallest.
    await sortButton.click();
    await page
      .getByRole("menu", { name: "Sort" })
      .getByRole("menuitem", { name: "Descending" })
      .click();
    await expect(sortButton).toHaveAttribute("aria-label", /Descending/);
    names = await order();
    expect(names.indexOf("backup.tar.gz")).toBeLessThan(
      names.indexOf("report-0000.txt"),
    );

    // A keyboard user reaches the control too (native Enter opens the menu).
    await sortButton.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("menu", { name: "Sort" })).toBeVisible();
    await expect(sortButton).toHaveAttribute("aria-expanded", "true");
  });
});

test.describe("the grid ⋮ follows the same popup/ARIA contract as the list", () => {
  test("haspopup/expanded agree and only one menu opens at a time", async ({
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

  test("a touch tablet above the compact range keeps a 44px ⋮ and reachable row", async ({
    page,
  }) => {
    await openFiles(page, { viewport: { width: 1024, height: 768 } });
    const more = moreFor(page, "app.yaml");
    const box = await more.boundingBox();
    expect(box!.width).toBeGreaterThanOrEqual(44);
    expect(box!.height).toBeGreaterThanOrEqual(44);
    await bringIntoView(more);
    await expectReachable(more, 1024);
    await more.tap();
    await expect(
      page.getByRole("menu", { name: "More actions for app.yaml" }),
    ).toBeVisible();
  });

  for (const width of [390, 768, 861, 1024, 1280]) {
    test(`touch at ${width}px: the row and tile ⋮ are hit-testable`, async ({
      page,
    }) => {
      await openFiles(page, { viewport: { width, height: 900 } });
      const listMore = moreFor(page, "app.yaml");
      await bringIntoView(listMore);
      await expectReachable(listMore, width);

      await page.getByRole("button", { name: "Grid view" }).click();
      const tileMore = moreFor(page, "app.yaml");
      await bringIntoView(tileMore);
      await expectReachable(tileMore, width);
    });
  }
});
