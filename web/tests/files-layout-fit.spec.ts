import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, requestAnimationFrame:readonly, URL, window:readonly, getComputedStyle:readonly */
// JUS-118/119/120 layout proof: the committed webdist is served statically with
// the control-plane file API mocked in real Chromium. Measured claims:
//   - JUS-118 the browser panel fills the viewport and only the entry list
//     scrolls (no page-level scroll) at desktop and mobile widths.
//   - JUS-119/134 the page-top breadcrumb is gone and the agent-node selector
//     lives in the browser context bar (it switches to another node's files).
//   - JUS-120 the toolbar groups New file/New folder under one dropdown, keeps
//     bulk tools on the right, and collapses to labelled icon-only buttons on
//     small screens.
//   - JUS-131 the permissions dialog shows the bits grid first with the octal
//     field below it, and drops the explanatory copy.
//   - JUS-135 the phone toolbar keeps the list at >=59% of the viewport with a
//     selection active (the bulk actions become one scrolling 44px row) and
//     the server rail collapses into a Naive UI drawer below the tablet
//     breakpoint.

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
const server3 = "33333333-3333-4333-8333-333333333333";

function serverRow(id: string, name: string, ip: string, status = "ready") {
  return {
    id,
    name,
    ip,
    status,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

const row1 = serverRow(server1, "prod-01", "10.0.0.1");
const row2 = serverRow(server2, "prod-02", "10.0.0.2");
const row3 = serverRow(server3, "prod-03", "10.0.0.3", "offline");
const rows = [row1, row2, row3];

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

/**
 * assertReachable fails when `locator` is not laid out inside `container` (and
 * the viewport) or when its centre is not the topmost element at that point —
 * so a clipped, spilling or covered element fails even though it has an
 * aria-label.
 */
async function assertReachable(
  page: Page,
  locator: Locator,
  container: { x: number; y: number; width: number; height: number },
  viewport: { width: number; height: number },
  name: string,
  context: string = name,
): Promise<void> {
  await expect(locator, context).toBeVisible();
  const box = await locator.boundingBox();
  expect(box, `${context}: no box`).not.toBeNull();
  expect(box!.x, `${context}: left inside container`).toBeGreaterThanOrEqual(container.x - 1);
  expect(box!.x + box!.width, `${context}: right inside container`).toBeLessThanOrEqual(
    container.x + container.width + 1,
  );
  expect(box!.y, `${context}: top inside viewport`).toBeGreaterThanOrEqual(-1);
  expect(box!.y + box!.height, `${context}: bottom inside viewport`).toBeLessThanOrEqual(
    viewport.height + 1,
  );
  const hit = await page.evaluate(
    ({ x, y }) => {
      const element = document.elementFromPoint(x, y) as HTMLElement | null;
      return element?.closest("button")?.getAttribute("aria-label") ?? null;
    },
    { x: box!.x + box!.width / 2, y: box!.y + box!.height / 2 },
  );
  expect(hit, `${context}: hit-testable at its centre`).toBe(name);
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
  // A small editable file so the editor window can open and be minimized.
  await page.route("**/api/v1/servers/*/files/content*", async (route) => {
    await route.fulfill({
      json: {
        content: "hello\n",
        etag: '"v1"',
        sha256: "abc",
        size: 6,
        mode: 0o100644,
      },
    });
  });
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
    // Wait for the whole listing to render before measuring: the flex height
    // and the scroll metrics settle only after the rows exist (measuring
    // mid-render was the source of the one-off flake in the full-suite run).
    await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

    // No page-level scroll: neither the window nor the .view container scrolls.
    // Polled so a transient mid-layout frame cannot fail the check.
    await expect
      .poll(
        () =>
          page.evaluate(() => {
            const view = document.querySelector(".view") as HTMLElement | null;
            const doc = document.scrollingElement as HTMLElement;
            return Math.max(
              doc.scrollHeight - window.innerHeight,
              view ? view.scrollHeight - view.clientHeight : 0,
            );
          }),
        { message: "no page-level scroll" },
      )
      .toBeLessThanOrEqual(1);

    // The panel reaches the bottom of the content column (bar + padding).
    await expect
      .poll(async () => {
        const box = await card.boundingBox();
        return box ? box.y + box.height : 0;
      })
      .toBeGreaterThan(viewport.height - 120);

    // The list owns the overflow: it is taller than its scrollport.
    await expect
      .poll(() =>
        page.evaluate(() => {
          const wrap = document.querySelector(".table-wrap") as HTMLElement | null;
          return wrap ? wrap.scrollHeight - wrap.clientHeight : 0;
        }),
      )
      .toBeGreaterThan(0);
  }
});

test("the top breadcrumb is gone and the header node selector switches nodes", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);

  // JUS-119: the page-top breadcrumb (Servers / node / Files) is removed.
  await expect(page.locator(".breadcrumb")).toHaveCount(0);

  // JUS-154/JUS-206 round 4: the visible "Agent node" caption is gone (the
  // group keeps its accessible name); only the "Root" caption in the toolbar
  // remains, at 12px (--text-xs).
  await expect(page.locator(".fm-contextbar .fm-field-label")).toHaveCount(0);
  const toolbarCaptions = page.locator(".fm-toolbar .fm-field-label");
  await expect(toolbarCaptions).toHaveCount(1);
  await expect(toolbarCaptions).toHaveText("Root");
  const toolbarSizes = await toolbarCaptions.evaluateAll((elements) =>
    elements.map((element) => getComputedStyle(element).fontSize),
  );
  expect(toolbarSizes).toEqual(["12px"]);

  // JUS-134/JUS-160: the agent-node selector decides the listing, so it lives
  // in the context bar, while the Root selector moved to the action toolbar
  // below — not a bar at the page bottom. Round 4: the switcher shares the tab
  // row (tabs left, switcher right) with its accessible name and no caption.
  const node = page.locator(".fm-node__select");
  await expect(node).toBeVisible();
  await expect(node).toContainText("prod-01");
  await expect(page.locator(".fm-node")).toHaveAttribute(
    "aria-label",
    "Switch agent node",
  );
  const tabsBox = (await page.locator(".fm-tabs").boundingBox())!;
  const nodeBox = (await node.boundingBox())!;
  const rootBox = (await page.locator(".fm-root-select").boundingBox())!;
  const toolbarBox = (await page.locator(".fm-toolbar").boundingBox())!;
  // Playwright boxes carry x/y/width/height (no top/bottom): derive edges.
  expect(Math.abs(nodeBox.y - tabsBox.y)).toBeLessThanOrEqual(8);
  expect(nodeBox.x).toBeGreaterThanOrEqual(tabsBox.x + tabsBox.width);
  expect(nodeBox.y + nodeBox.height).toBeLessThanOrEqual(toolbarBox.y + 1);
  expect(rootBox.y).toBeGreaterThanOrEqual(toolbarBox.y);

  // An offline node is listed with its status but cannot be selected.
  await node.click();
  const offline = page.locator(".n-base-select-option", { hasText: "prod-03" });
  await expect(offline).toHaveClass(/disabled/);
  await expect(offline).toContainText("Offline");

  // Switching nodes reloads that node's files.
  await page.locator(".n-base-select-option", { hasText: "prod-02" }).click();
  await expect(page.locator(".fm-node__select")).toContainText("prod-02");
  expect(new URL(page.url()).pathname).toContain(server2);
  await expect(page.locator(".fm-label").first()).toBeVisible();
});

test("the context-bar node selector stays reachable at desktop and mobile", async ({
  page,
}) => {
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${server1}/files`);
    const node = page.locator(".fm-node__select");
    await expect(node).toBeVisible();
    const box = (await node.boundingBox())!;
    expect(box.width).toBeGreaterThan(90);
    expect(box.x).toBeGreaterThanOrEqual(-1);
    expect(box.x + box.width).toBeLessThanOrEqual(viewport.width + 1);
  }
});

test("bulk actions stay reachable with a selection at every width", async ({
  page,
}) => {
  const bulkActions = ["Download", "Copy", "Cut", "Compress…", "Delete", "Clear"];
  // 1876/1280/1024/861 cover the desktop table overflowing the scroller (the
  // bar pinned to the scroller's visible width); 768/390 the compact layout
  // where the table fits and the bar just fills its cell (JUS-145/146).
  for (const viewport of [
    { width: 1876, height: 1080 },
    { width: 1280, height: 1024 },
    { width: 1024, height: 1024 },
    { width: 861, height: 1024 },
    { width: 768, height: 1024 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${server1}/files`);
    // Select the whole listing so every bulk action is enabled and Clear shows.
    await page.locator(".fm-head .n-checkbox").click();

    // JUS-146: the bulk actions (and their count) live in the sticky header bar
    // that replaces the column-header row while a selection exists.
    const bar = page.locator(".fm-head-bulk");
    const barBox = await bar.boundingBox();
    expect(barBox, `bulk bar missing at ${viewport.width}px`).not.toBeNull();
    await expect(page.locator(".fm-bulk-count")).toContainText("60 selected");

    // JUS-146: where the table is wider than the scroller (tablet/phone) the
    // bar's OWN action row scrolls, so every action stays reachable without
    // scrolling the table. Detect the overflow and scroll each action into view
    // before measuring it.
    const overflow = await page
      .locator(".fm-selection-actions")
      .evaluate((element) => element.scrollWidth - element.clientWidth);
    const scrolling = overflow > 1;
    for (const name of bulkActions) {
      const button = bar.getByRole("button", { name, exact: true });
      if (scrolling) {
        await button.scrollIntoViewIfNeeded();
      }
      await assertReachable(
        page,
        button,
        barBox!,
        viewport,
        name,
        `${name}@${viewport.width}`,
      );
    }

    if (scrolling) {
      // The bar's action row is the intended scroll region: it must scroll so
      // Delete/Clear stay reachable.
      expect(overflow, `bulk row scrolls at ${viewport.width}px`).toBeGreaterThan(0);
    }
    if (viewport.width <= 640) {
      // ...and every action is a 44px touch target on a phone.
      const height = await bar
        .getByRole("button", { name: "Delete", exact: true })
        .evaluate((element) => element.getBoundingClientRect().height);
      expect(height).toBeGreaterThanOrEqual(44);
    }
    if (!scrolling) {
      // Nothing in the bar (buttons or the count) spills past its right edge.
      const spill = await page.evaluate(() => {
        const el = document.querySelector(".fm-head-bulk") as HTMLElement | null;
        if (el === null) {
          return null;
        }
        const right = el.getBoundingClientRect().right;
        return [...el.querySelectorAll("button, .fm-bulk-count")]
          .map((element) => element.getBoundingClientRect().right - right)
          .reduce((max, value) => Math.max(max, value), Number.NEGATIVE_INFINITY);
      });
      expect(spill, `no bar spill at ${viewport.width}px`).not.toBeNull();
      expect(spill!).toBeLessThanOrEqual(1);
    }
  }
});

// JUS-134/JUS-141: selecting a row must not reflow the toolbar, or the second
// click of a double-click lands on a different row. The count cell stacks a
// hidden reserve sizer and the live summary in one grid area, so its width is
// max(reserve, live) in every selection state and the toolbar's height is the
// same before and after the first click at every width. The fixture is the
// MED-1 boundary: total "1.00 MiB" but a subset "1023.0 KiB" (formatBytes is
// non-monotonic), plus a long name.
async function firstRowY(page: Page): Promise<number> {
  return page.evaluate(
    () =>
      (
        document.querySelector(".fm-table .fm-row:not(.fm-head)") as HTMLElement
      ).getBoundingClientRect().y,
  );
}

/** installListing overrides the listing route with the given entries. */
async function installListing(page: Page, entries: unknown[]): Promise<void> {
  await page.route(`**/api/v1/servers/${server1}/files/list*`, async (route) => {
    await route.fulfill({ json: { entries, truncated: false, cursor: "" } });
  });
}

/** installBoundaryListing overrides the listing with a formatBytes boundary. */
async function installBoundaryListing(page: Page, count: number): Promise<void> {
  const entries = Array.from({ length: count }, (_, index) =>
    index === 0
      ? {
          name: "a-very-long-entry-name-that-uses-the-whole-name-column-backup.tar.gz",
          kind: "file",
          size: 1047552,
          mode: 0o100644,
          modified_at: "2026-10-12T09:02:00Z",
          etag: '"big"',
        }
      : {
          name: `small-${index}.txt`,
          kind: "file",
          size: Math.floor(1024 / (count - 1)),
          mode: 0o100644,
          modified_at: "2026-10-12T09:02:00Z",
          etag: `"s${index}"`,
        },
  );
  await installListing(page, entries);
}

/** settleLayout waits for the responsive re-layout after a viewport change. */
async function settleLayout(page: Page): Promise<void> {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
}

/** countCellWidth is the count cell's rendered width. */
async function countCellWidth(page: Page): Promise<number> {
  return page
    .locator(".fm-bulk-count")
    .evaluate((element) => element.getBoundingClientRect().width);
}

/**
 * axColumnHeaders counts the header row's columnheader cells in the
 * ACCESSIBILITY tree (not the DOM), so a visibility-hidden variant is not
 * counted — the a11y tree must expose exactly one set of header cells at a time
 * (JUS-146).
 */
async function axColumnHeaders(page: Page): Promise<number> {
  const snapshot = await page.locator(".fm-head").ariaSnapshot();
  return snapshot
    .split("\n")
    .filter((line) => line.includes("columnheader")).length;
}

/** assertStableOnSelect measures the count-cell width and first-row y on select
 *  and clear, so a reserve narrower than the live summary (a widened cell that
 *  wraps the toolbar) fails loudly. */
async function assertStableOnSelect(page: Page, widths: number[]): Promise<void> {
  for (const width of widths) {
    await page.setViewportSize({ width, height: 1024 });
    await settleLayout(page);
    const beforeY = await firstRowY(page);
    const beforeWidth = await countCellWidth(page);

    await page.locator(".fm-label").first().click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);
    const selectedWidth = await countCellWidth(page);
    expect(
      Math.abs(selectedWidth - beforeWidth),
      `count cell width delta on select at ${width}px`,
    ).toBeLessThanOrEqual(0.5);
    expect(
      Math.abs((await firstRowY(page)) - beforeY),
      `row shift on select at ${width}px`,
    ).toBeLessThanOrEqual(1);

    // Clear via the last bulk button (locale-agnostic: its label is translated).
    await page.locator(".fm-selection-actions .n-button").last().click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(0);
    expect(
      Math.abs((await firstRowY(page)) - beforeY),
      `row shift on clear at ${width}px`,
    ).toBeLessThanOrEqual(1);
  }
}

/** assertNoRowShift measures the first-row y across select and clear. */
async function assertNoRowShift(page: Page, widths: number[]): Promise<void> {
  for (const width of widths) {
    await page.setViewportSize({ width, height: 1024 });
    await settleLayout(page);
    const before = await firstRowY(page);

    await page.locator(".fm-label").first().click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);
    const selected = await firstRowY(page);
    expect(
      Math.abs(selected - before),
      `row shift on select at ${width}px`,
    ).toBeLessThanOrEqual(1);

    // Clear via the last bulk button (locale-agnostic: its label is translated).
    await page.locator(".fm-selection-actions .n-button").last().click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(0);
    const cleared = await firstRowY(page);
    expect(
      Math.abs(cleared - before),
      `row shift on clear at ${width}px`,
    ).toBeLessThanOrEqual(1);
  }
}

test("selecting a row never shifts the list across 700-1500px", async ({ page }) => {
  test.setTimeout(300_000);
  await page.setViewportSize({ width: 1280, height: 1024 });
  await mockApi(page);
  await installBoundaryListing(page, 5);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(5);

  // The 5-entry fixture exposes the formatBytes boundary on a single click
  // ("5 selected · 1.00 MiB" reserve vs "1 selected · 1023.0 KiB" live); 796,
  // 802 and 808 are inside the even step-2 sweep.
  const widths: number[] = [];
  for (let width = 700; width <= 1500; width += 2) {
    widths.push(width);
  }
  await assertNoRowShift(page, widths);
});

test("the count cell never widens at the 100x-unit rounding boundary", async ({
  page,
}) => {
  test.setTimeout(300_000);
  await page.setViewportSize({ width: 1280, height: 1024 });
  await mockApi(page);
  // A folder of 102399 + 1 bytes: the total formats "100.0 KiB" but a subset
  // (the 102399-byte entry) formats the WIDER "100.00 KiB" (formatBytes drops
  // from 2 to 1 decimal at 100, and toFixed rounds 99.999 up). This is the
  // round-2 BLOCK-1 counterexample; the guard must fail on it.
  await installListing(page, [
    {
      name: "almost-one-hundred-kib.bin",
      kind: "file",
      size: 102399,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"a"',
    },
    {
      name: "one-byte.bin",
      kind: "file",
      size: 1,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"b"',
    },
  ]);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(2);

  // The reported band, then the full dense sweep.
  const widths = [808, 810, 812, 1148, 1150, 1152];
  for (let width = 700; width <= 1500; width += 2) {
    widths.push(width);
  }
  await assertStableOnSelect(page, widths);
});

test("selecting a row never shifts the list in vi with a large count", async ({
  page,
}) => {
  test.setTimeout(300_000);
  await page.setViewportSize({ width: 1280, height: 1024 });
  await page.addInitScript(() => {
    window.localStorage.setItem("gotham-locale", "vi");
  });
  await mockApi(page);
  await installBoundaryListing(page, 1500);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  // The list windows, so assert the folder total (via the reserve sizer) rather
  // than the rendered rows. Localized copy, so match the count only.
  await expect(page.locator(".fm-bulk-count-reserve")).toContainText("1500");

  const widths: number[] = [];
  for (let width = 700; width <= 1500; width += 6) {
    widths.push(width);
  }
  await assertNoRowShift(page, widths);
});

// JUS-146: the same guard at the phone/tablet breakpoints and their edges — the
// header row swaps for the bulk bar at every width, so the first row must not
// move when it swaps.
test("selecting a row never shifts the list at phone and tablet widths", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await installBoundaryListing(page, 5);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(5);

  await assertNoRowShift(page, [390, 414, 480, 639, 640, 641, 768, 820]);
});

// JUS-146: the sticky column-header row is REPLACED by the bulk bar while a
// selection exists (same row, same height). Nothing is permanently reserved, so
// there is no blank band above the list on desktop; the select-all checkbox
// stays reachable in both states and the sort buttons return on Clear.
test("the bulk bar replaces the header row with no reserved band", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1876, height: 1080 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

  // Nothing selected: the column headers show and the bulk bar is hidden (not
  // merely transparent), so it is not an extra empty columnheader in the a11y
  // tree — exactly the six real header cells are exposed (JUS-161 dropped Type).
  await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "visible");
  await expect(page.locator(".fm-head-bulk")).toHaveCSS("visibility", "hidden");
  expect(await axColumnHeaders(page), "one set of header cells (idle)").toBe(6);
  const spacing = await page.evaluate(() => {
    const toolbar = document.querySelector(".fm-toolbar") as HTMLElement;
    const head = document.querySelector(".fm-head") as HTMLElement;
    const t = toolbar.getBoundingClientRect();
    return {
      toolbarHeight: t.height,
      gap: head.getBoundingClientRect().top - t.bottom,
    };
  });
  // One toolbar row, then the header — no blank reserved band in between.
  expect(spacing.toolbarHeight, "desktop toolbar is one row").toBe(32);
  expect(spacing.gap, "16px band gap plus table border").toBe(17);

  // Selected: the column headers hide and the bulk bar takes their place — the
  // a11y tree still exposes exactly one set of header cells (the select-all cell
  // plus the bar).
  await page.locator(".fm-label").first().click();
  await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "hidden");
  await expect(page.locator(".fm-head-bulk")).toHaveCSS("visibility", "visible");
  await expect(page.locator(".fm-head-bulk")).toContainText("1 selected");
  expect(await axColumnHeaders(page), "one set of header cells (selected)").toBe(2);

  // The leading select-all checkbox stays reachable while the bar shows; Clear
  // brings the sort buttons (and their aria-sort) back.
  await expect(page.locator(".fm-head .n-checkbox")).toBeVisible();
  await page
    .locator(".fm-selection-actions")
    .getByRole("button", { name: "Clear", exact: true })
    .click();
  await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "visible");
  await expect(page.locator(".fm-head .fm-name[aria-sort]")).toHaveAttribute(
    "aria-sort",
    "ascending",
  );
});

// JUS-146: the selection count is announced from an always-mounted polite live
// region (mounted empty before any selection, updated once the selection
// settles), so the first selection is announced and a range select does not
// chatter; the select-all checkbox stays reachable while the bar shows.
test("the selection count is announced politely while the bar shows", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1876, height: 1080 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

  const live = page.locator('[role="status"]');
  await expect(live).toHaveCount(1);
  await expect(live).toHaveText("");

  await page.locator(".fm-head .n-checkbox").click();
  await expect(page.locator(".fm-row.is-selected")).toHaveCount(60);
  await expect(live).toHaveText("60 selected");
});

// JUS-146/147: while the bulk bar replaces the column headers the sort must stay
// reachable at EVERY width, not only the compact/grid layouts. JUS-147 moved the
// selection-time sort OUT of the toolbar (where as a hidden control it wrapped
// the toolbar onto an empty row) and INTO the bulk bar, which already replaces
// the header row.
test("sorting stays reachable while the bulk bar replaces the headers", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1876, height: 1080 });
  await mockApi(page);
  await installListing(page, [
    { name: "aaa.txt", kind: "file", size: 300, mode: 0o100644, modified_at: "2026-10-12T09:02:00Z", etag: '"a"' },
    { name: "bbb.txt", kind: "file", size: 100, mode: 0o100644, modified_at: "2026-10-12T09:02:00Z", etag: '"b"' },
    { name: "ccc.txt", kind: "file", size: 200, mode: 0o100644, modified_at: "2026-10-12T09:02:00Z", etag: '"c"' },
  ]);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(3);

  // Nothing selected: the headers carry the sort. The toolbar control is not
  // rendered at all on desktop (JUS-147 — no reserved space) and the bar's own
  // sort rides the hidden bar.
  await expect(page.locator(".fm-sort-toggle")).toHaveCount(0);
  await expect(page.locator(".fm-bulk-sort")).toBeHidden();

  await page.locator(".fm-label").first().click();
  await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "hidden");
  // The bar replaces the headers; its sort control is the reachable affordance
  // and mirrors the active sort.
  const sort = page.locator(".fm-bulk-sort");
  await expect(sort).toBeVisible();
  await expect(sort).toHaveAttribute("aria-label", /Name/);

  // It changes the order (click/touch path).
  await sort.click();
  await page.getByRole("menuitem", { name: "Size", exact: true }).click();
  await expect(sort).toHaveAttribute("aria-label", /Size/);
  await expect(
    page.locator(".fm-table .fm-row:not(.fm-head)").first().locator("b"),
  ).toHaveText("bbb.txt");

  // Keyboard path: Enter on the focused control opens its menu.
  await sort.focus();
  await page.keyboard.press("Enter");
  await page.getByRole("menuitem", { name: "Name", exact: true }).click();
  await expect(
    page.locator(".fm-table .fm-row:not(.fm-head)").first().locator("b"),
  ).toHaveText("aaa.txt");

  // Clear returns the headers and their truthful aria-sort; the bar (and its
  // sort) hides again.
  await page
    .locator(".fm-selection-actions")
    .getByRole("button", { name: "Clear", exact: true })
    .click();
  await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "visible");
  await expect(page.locator(".fm-head .fm-name[aria-sort]")).toHaveAttribute(
    "aria-sort",
    "ascending",
  );
  await expect(sort).toBeHidden();
});

// JUS-146: the grid has no column-header row, so its bar stays in the toolbar —
// selecting a tile must not shift the tiles (they open on double-click).
test("the grid selection bar does not shift the tiles", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await page.getByRole("button", { name: "Grid view" }).click();
  await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();

  const firstTileY = (): Promise<number> =>
    page.evaluate(() =>
      Math.round(
        (
          document.querySelector(".fm-tile-wrap") as HTMLElement
        ).getBoundingClientRect().y,
      ),
    );
  const before = await firstTileY();
  await page.locator(".fm-tile-wrap").first().click();
  await expect(page.locator(".fm-tile.is-selected")).toHaveCount(1);
  expect(
    Math.abs((await firstTileY()) - before),
    "tile shift on select",
  ).toBeLessThanOrEqual(1);
});

// JUS-146: with a clipboard AND a selection on a phone, the clipboard hint and
// the bulk actions must not compete for one narrow width. The hint is a
// single-line chip; the bar's own action row scrolls, so every action
// (Delete/Clear included) is reachable without scrolling the table.
test("on a phone the clipboard chip and the bulk bar never wrap", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

  // Put a Cut clipboard on the store, then select a row.
  await page.getByRole("button", { name: /More actions for file-00/ }).click();
  await page.getByRole("menuitem", { name: "Cut", exact: true }).click();
  await page.locator(".fm-label").first().click();
  await expect(page.locator(".fm-bulk-count")).toContainText("1 selected");

  const hint = page.locator(".clipboard-hint");
  await expect(hint).toBeVisible();
  const hintBox = (await hint.boundingBox())!;
  expect(hintBox.width, "clipboard hint is not squeezed").toBeGreaterThan(60);
  expect(hintBox.height, "clipboard hint is one line").toBeLessThan(30);
  expect(
    await hint.evaluate((element) => element.getClientRects().length),
    "clipboard hint does not wrap into columns",
  ).toBe(1);

  // Scroll the bar's OWN action row to the end: the table must not move, and
  // Clear (with Delete before it) lands fully inside the viewport as a 44px
  // target.
  const tableScrollBefore = await page.evaluate(
    () => (document.querySelector(".table-wrap") as HTMLElement).scrollLeft,
  );
  await page.evaluate(() => {
    const row = document.querySelector(".fm-selection-actions") as HTMLElement;
    row.scrollLeft = row.scrollWidth;
  });
  for (const name of ["Delete", "Clear"]) {
    const box = (await page
      .getByRole("button", { name, exact: true })
      .boundingBox())!;
    expect(box.x, `${name} left inside the viewport`).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width, `${name} right inside the viewport`).toBeLessThanOrEqual(391);
    expect(box.height, `${name} is a 44px target`).toBeGreaterThanOrEqual(44);
  }
  expect(
    await page.evaluate(
      () => (document.querySelector(".table-wrap") as HTMLElement).scrollLeft,
    ),
    "scrolling the bar does not scroll the table",
  ).toBe(tableScrollBefore);
});

// JUS-135: the phone toolbar must not eat the list. With a selection active
// (the tallest chrome state) the entry list keeps at least 59% of an iPhone-14
// viewport.
test("the entry list keeps at least 59% of the phone viewport", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

  await page.locator(".fm-head .n-checkbox").click();
  await expect(page.locator(".fm-bulk-count")).toContainText("60 selected");

  const share = await page.evaluate(() => {
    const wrap = document.querySelector(".table-wrap") as HTMLElement | null;
    return wrap ? wrap.clientHeight / window.innerHeight : 0;
  });
  expect(share, "entry list share of the phone viewport").toBeGreaterThanOrEqual(0.59);
});

test("phone chrome keeps the current folder readable and every toolbar action reachable", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label").first()).toBeVisible();

  const crumb = page.locator(".fm-crumb.is-current > span");
  const visibleWidth = await crumb.evaluate((element) => {
    const text = element.getBoundingClientRect();
    const nav = element.closest("nav")!.getBoundingClientRect();
    return Math.max(0, Math.min(text.right, nav.right) - Math.max(text.left, nav.left));
  });
  expect(visibleWidth, "current folder text is not clipped to a stem").toBeGreaterThanOrEqual(40);
  const field = (await page.locator(".fm-navfield").boundingBox())!;
  const search = (await page.locator(".fm-search-group").boundingBox())!;
  expect(search.y, "search has its own row").toBeGreaterThanOrEqual(field.y + field.height);

  const toolbar = page.locator(".fm-toolbar");
  const bounds = (await toolbar.boundingBox())!;
  expect(bounds.height, "toolbar stays one row").toBe(32);
  const actions = toolbar.getByRole("button");
  for (const action of await actions.all()) {
    await action.scrollIntoViewIfNeeded();
    const name = (await action.getAttribute("aria-label"))!;
    await assertReachable(page, action, bounds, { width: 390, height: 844 }, name);
  }
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
  expect(overflow, "the page never scrolls sideways").toBeLessThanOrEqual(1);
});

// JUS-135: below the tablet breakpoint the server rail is a Naive UI drawer
// (focus trap / Escape / dialog semantics) instead of an always-on column, so
// the content column owns the full width.
test("the server rail collapses into a phone drawer", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label").first()).toBeVisible();

  // No always-on rail at a phone width, and no drawer until it is opened.
  await expect(page.locator(".app > .rail")).toHaveCount(0);
  await expect(page.locator(".rail-drawer")).toHaveCount(0);

  const toggle = page.getByRole("button", { name: "Toggle servers" });
  await expect(toggle).toBeVisible();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");

  await toggle.click();
  await expect(toggle).toHaveAttribute("aria-expanded", "true");
  const dialog = page.locator(".n-drawer[role='dialog']");
  await expect(dialog).toBeVisible();
  await expect(dialog.locator(".rail")).toBeVisible();
  await expect(
    dialog.getByRole("link", { name: "Add server", exact: true }),
  ).toBeVisible();

  // Escape closes the drawer and focus returns to the trigger (NDrawer).
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
  await expect(toggle).toBeFocused();
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
  await expect(newButton).toHaveAttribute("aria-haspopup", "menu");
  await expect(newButton).toHaveAttribute("aria-expanded", "false");
  await newButton.click();
  await expect(newButton).toHaveAttribute("aria-expanded", "true");
  await expect(page.getByRole("menuitem", { name: "New file", exact: true })).toBeVisible();
  await expect(page.getByRole("menuitem", { name: "New folder", exact: true })).toBeVisible();
  await page.keyboard.press("Escape");

  // JUS-128/134/146: no separate bulk bar and no disabled row — the bulk
  // actions live in the header bar and are hidden (not disabled) until entries
  // are selected.
  await expect(page.locator(".fm-bulk")).toHaveCount(0);
  const bar = page.locator(".fm-head-bulk");
  const bulkDownload = bar.getByRole("button", { name: "Download", exact: true });
  await expect(bulkDownload).toBeHidden();

  // Select the whole listing so the bulk actions appear and the count shows.
  await page.locator(".fm-head .n-checkbox").click();
  await expect(bulkDownload).toBeVisible();
  await expect(page.locator(".fm-bulk-count")).toContainText("60 selected");

  // The count precedes the bulk actions in the bar (JUS-146).
  const count = await page.locator(".fm-bulk-count").boundingBox();
  const compress = await bar
    .getByRole("button", { name: "Compress…", exact: true })
    .boundingBox();
  expect(count).not.toBeNull();
  expect(compress).not.toBeNull();
  expect(compress!.x).toBeGreaterThan(count!.x + count!.width);

  // Small screens: labels are hidden and every toolbar button keeps its name.
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
    await expect(page.getByRole("button", { name, exact: true })).toHaveAttribute(
      "aria-label",
      name,
    );
  }
});

test("the minimized editor dock never covers the header node switcher", async ({
  page,
}) => {
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${server1}/files`);

    // Open a file and collapse the editor into the fixed bottom dock. Enter on
    // the focused row is the same JUS-130 activation path as a double-click and
    // keeps this check independent of the selection toolbar's layout.
    await page.locator(".fm-label").first().focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".ed-window")).toBeVisible({ timeout: 15_000 });
    await page.locator(".ed-window-min").click();
    await expect(page.locator(".ed-dock")).toBeVisible();

    // The header switcher sits above the bottom dock, so its centre is
    // clickable and never captured by the dock band (JUS-134).
    const hit = await page.evaluate(() => {
      const dock = document.querySelector(".ed-dock");
      const select = document.querySelector(".fm-node__select");
      if (!(dock instanceof HTMLElement) || !(select instanceof HTMLElement)) {
        return null;
      }
      const box = select.getBoundingClientRect();
      const target = document.elementFromPoint(
        box.x + box.width / 2,
        box.y + box.height / 2,
      );
      return {
        hitInsideDock: target !== null && dock.contains(target),
        hitInsideSelect: target !== null && select.contains(target),
        selectBottom: box.bottom,
        dockTop: dock.getBoundingClientRect().top,
      };
    });
    expect(hit).not.toBeNull();
    expect(hit!.hitInsideDock).toBe(false);
    expect(hit!.hitInsideSelect).toBe(true);
    expect(hit!.selectBottom).toBeLessThanOrEqual(hit!.dockTop);
  }
});

test("the permissions dialog puts the octal field under the bits and drops the copy", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);

  await page
    .getByRole("button", { name: "More actions for file-00.txt" })
    .click();
  await page.getByRole("menuitem", { name: "Permissions…" }).click();

  const dialog = page.locator(".n-modal").filter({ hasText: "Permissions" });
  await expect(dialog).toBeVisible();

  // JUS-131: the bits grid comes first, the octal field sits below it.
  const grid = await dialog.locator(".perms-grid").boundingBox();
  const octal = await dialog.locator(".octal input").boundingBox();
  expect(grid).not.toBeNull();
  expect(octal).not.toBeNull();
  expect(octal!.y).toBeGreaterThan(grid!.y + grid!.height);

  // The redundant explanatory copy is gone.
  const text = await dialog.innerText();
  for (const removed of [
    "Three octal digits",
    "Special bits",
    "keeps its type",
    "Current mode",
  ]) {
    expect(text).not.toContain(removed);
  }
});

// JUS-132: the list header stays pinned while a long folder scrolls, the
// folder filter resets on navigation with a "N of M entries" footer, and a
// symlink row shows its target and can jump to it.
test("the list header stays pinned while scrolling a long folder", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label").first()).toBeVisible();

  const head = page.locator(".fm-head");
  await expect(head).toHaveCSS("position", "sticky");

  const wrap = page.locator(".table-wrap");
  const wrapBox = (await wrap.boundingBox())!;
  await wrap.evaluate((element) => {
    element.scrollTop = 600;
  });

  const headBox = (await head.boundingBox())!;
  // The header rides the top of the scrollport instead of scrolling away.
  expect(headBox.y).toBeGreaterThanOrEqual(wrapBox.y - 1);
  expect(headBox.y).toBeLessThan(wrapBox.y + 4);

  // Hovering the pinned header keeps it opaque, so rows cannot bleed through.
  await head.hover();
  // JUS-206: the header band uses --surface-warm (#1e1f22), the reference tone.
  await expect(head).toHaveCSS("background-color", "rgb(30, 31, 34)");
});

test("the folder filter resets on navigation and the footer counts the filtered rows", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.route(`**/api/v1/servers/${server1}/files/list*`, async (route) => {
    const path = new URL(route.request().url()).searchParams.get("path") ?? "";
    await route.fulfill({
      json: {
        entries:
          path === "etc"
            ? [
                {
                  name: "hosts",
                  kind: "file",
                  size: 12,
                  mode: 0o100644,
                  modified_at: "2026-10-12T09:02:00Z",
                  etag: '"h"',
                },
              ]
            : [
                {
                  name: "bin",
                  kind: "symlink",
                  size: 7,
                  mode: 0o120777,
                  symlink_target: "usr/bin",
                  modified_at: "2026-10-12T09:02:00Z",
                  etag: '"b"',
                },
                {
                  name: "etc",
                  kind: "directory",
                  size: 4096,
                  mode: 0o040755,
                  modified_at: "2026-10-12T09:02:00Z",
                  etag: '"e"',
                },
                {
                  name: "usr",
                  kind: "directory",
                  size: 4096,
                  mode: 0o040755,
                  modified_at: "2026-10-12T09:02:00Z",
                  etag: '"u"',
                },
              ],
        truncated: false,
        cursor: "",
      },
    });
  });

  await page.goto(`${baseURL}/servers/${server1}/files`);
  const search = page.locator(".fm-search input");
  await search.fill("etc");

  // While filtering, the footer says how many of the folder's entries show,
  // and the breakdown follows the filtered rows: only the matching directory
  // is counted, so the whole folder's "2 folders · 1 symlink" is not mixed in
  // (JUS-141).
  await expect(page.locator(".fm-foot")).toContainText("1 of 3 entries");
  await expect(page.locator(".fm-foot")).toContainText("1 folder");
  await expect(page.locator(".fm-foot")).not.toContainText("symlink");

  // Opening the filtered folder clears the needle: its own listing is not
  // silently hidden behind the stale filter.
  await page.locator(".fm-label").filter({ hasText: "etc" }).dblclick();
  await expect(page.locator(".fm-label").filter({ hasText: "hosts" })).toBeVisible();
  await expect(search).toHaveValue("");
  await expect(page.locator(".fm-foot")).toContainText("1 entry");

  // A filtered single-entry folder reads "1 of 1 entry", not "1 of 1 entries".
  await search.fill("hosts");
  await expect(page.locator(".fm-foot")).toContainText("1 of 1 entry");
});

test("a symlink row shows its target and can jump to it", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  const requestedPaths: string[] = [];
  await page.route(`**/api/v1/servers/${server1}/files/list*`, async (route) => {
    const path = new URL(route.request().url()).searchParams.get("path") ?? "";
    requestedPaths.push(path);
    await route.fulfill({
      json: {
        entries:
          path === "usr/bin"
            ? [
                {
                  name: "python3",
                  kind: "file",
                  size: 9,
                  mode: 0o100755,
                  modified_at: "2026-10-12T09:02:00Z",
                  etag: '"p"',
                },
              ]
            : [
                {
                  name: "bin",
                  kind: "symlink",
                  size: 7,
                  mode: 0o120777,
                  symlink_target: "usr/bin",
                  modified_at: "2026-10-12T09:02:00Z",
                  etag: '"b"',
                },
              ],
        truncated: false,
        cursor: "",
      },
    });
  });

  await page.goto(`${baseURL}/servers/${server1}/files`);

  // The target is shown next to the type, in list and grid, and a long target
  // truncates with an ellipsis instead of wrapping the name column.
  await expect(page.locator(".fm-table").getByText("→ usr/bin")).toBeVisible();
  const subtitle = page.locator(".fm-table .fm-target").first();
  await expect(subtitle).toHaveCSS("text-overflow", "ellipsis");
  await expect(subtitle).toHaveCSS("white-space", "nowrap");
  await expect(subtitle).toHaveAttribute("title", "→ usr/bin");
  await page.getByRole("button", { name: "Grid view" }).click();
  await expect(page.locator(".fm-grid").getByText("Symlink → usr/bin")).toBeVisible();
  await page.getByRole("button", { name: "List view" }).click();

  // "Go to target" follows the link to the resolved path.
  await page.getByRole("button", { name: "More actions for bin" }).click();
  await page.getByRole("menuitem", { name: "Go to target" }).click();
  await expect(
    page.locator(".fm-table").getByText("python3", { exact: true }),
  ).toBeVisible();
  expect(requestedPaths).toContain("usr/bin");
});

test("Go to target on a file or broken link keeps the view and warns", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.route(`**/api/v1/servers/${server1}/files/list*`, async (route) => {
    const path = new URL(route.request().url()).searchParams.get("path") ?? "";
    if (path === "usr/bin") {
      // A target that is not a browsable directory answers 400 (ENOTDIR).
      await route.fulfill({ status: 400, json: { message: "not a directory" } });
      return;
    }
    await route.fulfill({
      json: {
        entries: [
          {
            name: "bin",
            kind: "symlink",
            size: 7,
            mode: 0o120777,
            symlink_target: "usr/bin",
            modified_at: "2026-10-12T09:02:00Z",
            etag: '"b"',
          },
        ],
        truncated: false,
        cursor: "",
      },
    });
  });

  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table").getByText("→ usr/bin")).toBeVisible();

  await page.getByRole("button", { name: "More actions for bin" }).click();
  await page.getByRole("menuitem", { name: "Go to target" }).click();

  // The list stays on the folder it was in; the failure is a warning, not an
  // error view.
  await expect(page.locator(".n-message")).toContainText("Could not open the target of bin");
  await expect(page.locator(".fm-table").getByText("→ usr/bin")).toBeVisible();
  await expect(page.locator(".fm-state")).toHaveCount(0);
});

// JUS-147: the toolbar must not reserve room for a hidden control, so the first
// toolbar row cannot wrap onto an empty second row at laptop widths. Nothing is
// selected here, so the gap from the LOWEST VISIBLE toolbar control to the header
// must be the plain inter-block rhythm (~12-14px) at EVERY desktop width, never
// the ~54px blank band the always-mounted hidden sort toggle used to open at
// 1366-1500px (a visibility:hidden control still occupies its row).
test("no blank band between the toolbar and the header at desktop widths", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  // A nested folder: the breadcrumb is as long as on a real box, which is what
  // brings the toolbar to the width where the hidden toggle tipped it over.
  await installListing(page, [
    { name: "storefront-8f2c1ab", kind: "directory", size: 4096, mode: 0o040755, modified_at: "2026-10-12T09:02:00Z", etag: '"d"' },
    ...manyEntries(),
  ]);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await page.locator(".fm-label", { hasText: "storefront-8f2c1ab" }).dblclick();
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(61);

  /** gap is the header top minus the bottom of the lowest VISIBLE toolbar cell. */
  const measure = (): Promise<number> =>
    page.evaluate(() => {
      const toolbar = document.querySelector(".fm-toolbar") as HTMLElement;
      const head = document.querySelector(".fm-head") as HTMLElement;
      const t = toolbar.getBoundingClientRect();
      let visibleBottom = t.top;
      for (const child of toolbar.querySelectorAll<HTMLElement>("*")) {
        const style = getComputedStyle(child);
        if (style.visibility === "hidden" || style.display === "none") {
          continue;
        }
        const box = child.getBoundingClientRect();
        if (box.height > 0) {
          visibleBottom = Math.max(visibleBottom, box.bottom);
        }
      }
      return head.getBoundingClientRect().top - visibleBottom;
    });

  for (const width of [1876, 1600, 1440, 1366, 1280, 1260, 920]) {
    await page.setViewportSize({ width, height: 900 });
    await settleLayout(page);
    const gap = await measure();
    expect(gap, `16px band gap plus table border at ${width}px`).toBe(17);
  }
});

// JUS-148: on a phone the selection bar's action row scrolls, but Delete/Clear
// used to sit off the right edge behind only a weak fade. A pinned chevron now
// marks the row as scrollable (and never eats a tap).
test("the phone selection bar marks its scrollable action row", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);
  await page.locator(".fm-head .n-checkbox").click();
  await expect(page.locator(".fm-bulk-count")).toContainText("60 selected");

  const row = page.locator(".fm-selection-actions");
  const overflow = await row.evaluate((el) => el.scrollWidth - el.clientWidth);
  expect(overflow, "action row overflows on a phone").toBeGreaterThan(0);

  const hint = page.locator(".fm-scroll-hint");
  await expect(hint).toBeVisible();
  expect(
    await hint.evaluate((el) => getComputedStyle(el).pointerEvents),
    "the hint never steals a tap",
  ).toBe("none");
  // It rides the row's right edge.
  const hintBox = (await hint.boundingBox())!;
  const rowBox = (await row.boundingBox())!;
  expect(hintBox.x + hintBox.width).toBeGreaterThanOrEqual(
    rowBox.x + rowBox.width - 2,
  );

  // Scrolling the row's own region brings Delete/Clear into the viewport.
  await row.evaluate((el) => {
    el.scrollLeft = el.scrollWidth;
  });
  for (const name of ["Delete", "Clear"]) {
    const box = (await page.getByRole("button", { name, exact: true }).boundingBox())!;
    expect(box.x, `${name} left inside the viewport`).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width, `${name} right inside the viewport`).toBeLessThanOrEqual(391);
  }
});

// JUS-148 fix round 1: the chevron is a real affordance only while the action row
// actually overflows to the right — no false "more →" on a wide bar, and it hides
// once the row is scrolled to its end.
test("the scroll chevron shows only while the action row overflows", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1876, height: 1080 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);
  await page.locator(".fm-head .n-checkbox").click();
  await expect(page.locator(".fm-bulk-count")).toContainText("60 selected");

  // Wide desktop: the row fits, so no chevron.
  const row = page.locator(".fm-selection-actions");
  expect(await row.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1);
  await expect(page.locator(".fm-scroll-hint")).toHaveCount(0);

  // Phone: the row overflows, so the chevron shows...
  await page.setViewportSize({ width: 390, height: 844 });
  await settleLayout(page);
  await expect(page.locator(".fm-scroll-hint")).toBeVisible();

  // ...and disappears once the row is scrolled to its end (no more to reveal).
  await row.evaluate((el) => {
    el.scrollLeft = el.scrollWidth;
  });
  await expect(page.locator(".fm-scroll-hint")).toHaveCount(0);
});

// JUS-148 fix round 1 + JUS-160: both toolbar groups (left actions, right
// recycle bin + view toggle) always render content, so the toolbar can wrap
// onto a second row at mid widths but never onto an EMPTY row (a content-free
// line whose row-gap still grew the toolbar). Every flex row must hold a real
// control, the toolbar height must be fully backed by content rows (no
// unexplained row-gap), it must not spill past the panel, and the gap to the
// header stays tight.
test("the always-on toolbar groups never open an empty row", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);
  // JUS-160: the right group (recycle bin + list/grid toggle) is always
  // rendered — its conditional rendering was removed with the regroup.
  await expect(page.locator(".fm-toolbar-end")).toHaveCount(1);
  await expect(
    page.getByRole("button", { name: "Recycle bin", exact: true }),
  ).toBeVisible();

  for (const width of [390, 480, 640, 768, 920, 1260, 1280, 1366, 1440, 1600, 1876]) {
    await page.setViewportSize({ width, height: 900 });
    await settleLayout(page);
    const layout = await page.evaluate(() => {
      const toolbar = document.querySelector(".fm-toolbar") as HTMLElement;
      const head = document.querySelector(".fm-head") as HTMLElement;
      const t = toolbar.getBoundingClientRect();
      const style = getComputedStyle(toolbar);
      // Cluster the laid-out controls into flex rows by vertical center. Two
      // box families are structural, never rows, and must not band: an icon's
      // SVG sub-paths (painted glyph geometry — e.g. a 2-unit menu dot renders
      // at ~1.17px and bands alone, yet is not a row) and the group containers
      // themselves (they span every wrapped line by construction).
      const bands: { center: number; maxHeight: number }[] = [];
      for (const child of toolbar.querySelectorAll<HTMLElement>("*")) {
        const tag = child.tagName.toLowerCase();
        if (tag !== "svg" && child.closest("svg") !== null) {
          continue;
        }
        if (child.classList.contains("fm-toolbar-group")) {
          continue;
        }
        const childStyle = getComputedStyle(child);
        if (childStyle.visibility === "hidden" || childStyle.display === "none") {
          continue;
        }
        const box = child.getBoundingClientRect();
        if (box.width <= 0 || box.height <= 0) {
          continue;
        }
        const center = box.top + box.height / 2;
        const band = bands.find((candidate) => Math.abs(candidate.center - center) < 4);
        if (band) {
          band.maxHeight = Math.max(band.maxHeight, box.height);
        } else {
          bands.push({ center, maxHeight: box.height });
        }
      }
      let visibleBottom = t.top;
      for (const child of toolbar.querySelectorAll<HTMLElement>("*")) {
        const childStyle = getComputedStyle(child);
        if (childStyle.visibility === "hidden" || childStyle.display === "none") {
          continue;
        }
        const box = child.getBoundingClientRect();
        if (box.height > 0) {
          visibleBottom = Math.max(visibleBottom, box.bottom);
        }
      }
      const content = bands.reduce((sum, band) => sum + band.maxHeight, 0);
      return {
        rows: bands.length,
        rowHeights: bands.map((band) => band.maxHeight),
        // Every row-gap in the toolbar height must be backed by a content row:
        // a content-free wrapped line adds a full row-gap of unexplained
        // height (the JUS-148 defect), which this cap catches.
        heightCap:
          content +
          Math.max(0, bands.length - 1) *
            (Number.parseFloat(style.rowGap || "0") || 0) +
          (Number.parseFloat(style.borderTopWidth || "0") || 0) +
          // JUS-203: the toolbar's own vertical padding is intended spacing,
          // not an empty row, so it belongs in the cap like the border does.
          (Number.parseFloat(style.paddingTop || "0") || 0) +
          (Number.parseFloat(style.paddingBottom || "0") || 0) +
          2,
        toolbarHeight: t.height,
        gap: head.getBoundingClientRect().top - visibleBottom,
        spill: toolbar.scrollWidth - toolbar.clientWidth,
      };
    });
    expect(layout.rows, `toolbar has rows at ${width}px`).toBeGreaterThan(0);
    for (const maxHeight of layout.rowHeights) {
      // Every row holds a real control (a small button row is ~28px).
      expect(maxHeight, `no empty toolbar row at ${width}px`).toBeGreaterThanOrEqual(20);
    }
    expect(
      layout.toolbarHeight,
      `toolbar height backed by content rows at ${width}px`,
    ).toBeLessThanOrEqual(layout.heightCap);
    if (width <= 480) {
      expect(layout.rows, "phone toolbar is one row").toBe(1);
      expect(layout.toolbarHeight, "phone toolbar has no orphan rows").toBe(32);
      // JUS-208 round 2: the scrollport moved one level in (the outer toolbar
      // keeps the row box so the grid bulk overlay can anchor to it) — the
      // scrollability guarantee is unchanged, only its host moved.
      await expect(page.locator(".fm-toolbar-scroll")).toHaveCSS("overflow-x", "auto");
    } else {
      expect(layout.spill, `no toolbar spill at ${width}px`).toBeLessThanOrEqual(1);
    }
    expect(layout.gap, `16px band gap plus table border at ${width}px`).toBe(17);
  }
});

// JUS-148: on a phone a symlink row truncated the NAME more than its target
// ("br… → /none…"). The name now owns the first line and the target rides the
// secondary line next to the relative time.
test("a phone symlink row keeps the name and moves the target under it", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await installListing(page, [
    {
      name: "broken-link",
      kind: "symlink",
      size: 7,
      mode: 0o120777,
      symlink_target: "/nonexistent/pretty-long-target",
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"l"',
    },
  ]);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  const row = page.locator('.fm-table .fm-row:not(.fm-head)[data-entry="broken-link"]');
  await expect(row).toBeVisible();

  // The target lives on the secondary line, not squeezed beside the name.
  const sub = row.locator(".fm-name-sub");
  await expect(sub.locator(".fm-target")).toHaveText("→ /nonexistent/pretty-long-target");
  await expect(sub.locator(".fm-row-sub")).toHaveText(/\S/);
  const labelBox = (await row.locator(".fm-label").boundingBox())!;
  const subBox = (await sub.boundingBox())!;
  expect(
    subBox.y,
    "the target + time sit below the name",
  ).toBeGreaterThanOrEqual(labelBox.y + labelBox.height - 2);

  // The name is not truncated.
  const name = row.locator(".fm-label b");
  const truncated = await name.evaluate((el) => el.scrollWidth > el.clientWidth + 1);
  expect(truncated, "the symlink name is fully readable").toBe(false);
});

// JUS-148: the compact sort menu is uncontrolled; a width change that remounts
// its trigger (compact ↔ desktop) used to reopen it by itself.
test("the compact sort menu does not reopen after a width change", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  const sort = page.getByRole("button", { name: /^Sort:/ });
  await sort.click();
  await expect(page.getByRole("menu", { name: "Sort" })).toBeVisible();
  await expect(sort).toHaveAttribute("aria-expanded", "true");

  // Widen past the compact breakpoint and back.
  await page.setViewportSize({ width: 1440, height: 900 });
  await settleLayout(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await settleLayout(page);

  await expect(
    page.getByRole("button", { name: /^Sort:/ }),
    "the remounted sort trigger stays closed",
  ).toHaveAttribute("aria-expanded", "false");
  await expect(page.getByRole("menu", { name: "Sort" })).toHaveCount(0);
});

// JUS-161: the Owner/Mode column sits right after Name and stays visible at
// every desktop width (the JUS-148 861-1280 drop is gone with the old Mode
// column position); it collapses only in the compact layout (<=860px).
test("the Owner/Mode column stays visible across desktop widths", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

  const headMode = page.locator(".fm-head .fm-cell--mode");
  const rowMode = page
    .locator(".fm-table .fm-row:not(.fm-head)")
    .first()
    .locator(".fm-cell--mode");

  for (const width of [1440, 1280, 1024, 861]) {
    await page.setViewportSize({ width, height: 768 });
    await settleLayout(page);
    await expect(headMode, `Owner/Mode header visible at ${width}px`).toBeVisible();
    await expect(rowMode, `Owner/Mode cell visible at ${width}px`).toBeVisible();
  }

  await page.setViewportSize({ width: 860, height: 844 });
  await settleLayout(page);
  await expect(headMode, "Owner/Mode header hidden in compact layout").toBeHidden();
  await expect(rowMode, "Owner/Mode cell hidden in compact layout").toBeHidden();
});

// JUS-214 fix round: the Owner/Mode track is a fixed 110px, so an owner longer
// than the track must clip inside the cell instead of spilling into the
// right-aligned Size cell (the review's "systemd-network/755" case). Measured in
// real Chromium at 1366/1920px. The full "owner/mode" text stays in the DOM so a
// screen reader still reads it.
test("a long owner clips inside the Owner/Mode cell without reaching Size", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1366, height: 900 });
  await mockApi(page);
  // Override the list with one entry whose owner outgrows the 110px track
  // ("systemd-network" is 15 chars; "systemd-network/755" does not fit).
  await page.route(
    `**/api/v1/servers/${server1}/files/list*`,
    async (route) => {
      await route.fulfill({
        json: {
          entries: [
            {
              name: "long.txt",
              kind: "file",
              size: 1024,
              mode: 0o100755,
              modified_at: "2026-10-12T09:02:00Z",
              etag: '"e"',
              owner: "systemd-network",
              group: "systemd-network",
            },
          ],
          truncated: false,
          cursor: "",
        },
      });
    },
  );
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(1);

  for (const width of [1366, 1920]) {
    await page.setViewportSize({ width, height: 900 });
    await settleLayout(page);
    const m = await page.evaluate(() => {
      const row = document.querySelector(
        ".fm-table .fm-row:not(.fm-head)",
      ) as HTMLElement;
      const mode = row.querySelector(".fm-mode") as HTMLElement;
      const size = row.querySelector(".fm-cell--size") as HTMLElement;
      const head = document.querySelector(".fm-head .fm-cell--mode") as HTMLElement;
      const headSize = document.querySelector(".fm-head .fm-cell--size") as HTMLElement;
      const style = getComputedStyle(mode);
      return {
        text: mode.textContent ?? "",
        modeRight: mode.getBoundingClientRect().right,
        modeWidth: mode.getBoundingClientRect().width,
        sizeLeft: size.getBoundingClientRect().left,
        headRight: head.getBoundingClientRect().right,
        headSizeLeft: headSize.getBoundingClientRect().left,
        overflow: style.overflow,
        textOverflow: style.textOverflow,
      };
    });
    expect(m.text, `full owner/mode text in the DOM at ${width}px`).toBe(
      "systemd-network/755",
    );
    expect(m.textOverflow, `clipped with an ellipsis at ${width}px`).toBe(
      "ellipsis",
    );
    expect(m.overflow, `overflow hidden at ${width}px`).toBe("hidden");
    // The cell is capped to its 110px track and never reaches the Size column;
    // the header's mode cell shares the same boundary.
    expect(m.modeWidth, `mode cell within the track at ${width}px`).toBeLessThanOrEqual(110.5);
    expect(m.modeRight, `mode cell left of Size at ${width}px`).toBeLessThanOrEqual(
      m.sizeLeft + 0.5,
    );
    expect(m.headRight, `header within the track at ${width}px`).toBeLessThanOrEqual(
      m.headSizeLeft + 0.5,
    );
  }
});

// JUS-148/JUS-161: while the bar replaces the headers the row exposes 2 cells
// against 6 body cells. aria-colcount on the table and aria-colspan on the bar
// keep the semantics consistent at every width.
test("the table and bulk bar agree on the column count", async ({ page }) => {
  await page.setViewportSize({ width: 1876, height: 1080 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

  const table = page.locator(".fm-table");
  const bar = page.locator(".fm-head-bulk");
  await expect(table).toHaveAttribute("aria-colcount", "6");
  await expect(bar).toHaveAttribute("aria-colspan", "5");
  expect(
    await page
      .locator(".fm-table .fm-row:not(.fm-head)")
      .first()
      .locator('[role="cell"]')
      .count(),
    "a body row exposes one cell per column",
  ).toBe(6);

  // No narrow-desktop band change anymore: Owner/Mode stays at 1024px.
  await page.setViewportSize({ width: 1024, height: 768 });
  await settleLayout(page);
  await expect(table).toHaveAttribute("aria-colcount", "6");
  await expect(bar).toHaveAttribute("aria-colspan", "5");

  // The compact layout collapses Modified/Owner-Mode.
  await page.setViewportSize({ width: 390, height: 844 });
  await settleLayout(page);
  await expect(table).toHaveAttribute("aria-colcount", "4");
  await expect(bar).toHaveAttribute("aria-colspan", "3");
});

// JUS-151: the empty state fills the browser panel, so its content group must
// sit on both of the panel's centre lines — horizontally AND vertically — at
// desktop and phone widths, instead of hugging the top-left.
test("the empty state is centered in the browser panel", async ({ page }) => {
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await installListing(page, []);
    await page.goto(`${baseURL}/servers/${server1}/files`);

    const state = page.locator(".fm-state");
    await expect(state).toBeVisible();
    await expect(state.locator("h4")).toHaveText("This folder is empty");

    // The group's centre (the union of the state's own children) against the
    // panel's centre. A top-left anchored state is hundreds of px off in y and
    // roughly half the panel wide in x.
    const measure = (): Promise<{
      panelCentreX: number;
      panelCentreY: number;
      contentCentreX: number;
      contentCentreY: number;
    }> =>
      page.evaluate(() => {
        const el = document.querySelector(".fm-state") as HTMLElement;
        const box = el.getBoundingClientRect();
        const children = [...el.children].map(
          (child) => (child as HTMLElement).getBoundingClientRect(),
        );
        const top = Math.min(...children.map((child) => child.top));
        const bottom = Math.max(...children.map((child) => child.bottom));
        const left = Math.min(...children.map((child) => child.left));
        const right = Math.max(...children.map((child) => child.right));
        return {
          panelCentreX: box.left + box.width / 2,
          panelCentreY: box.top + box.height / 2,
          contentCentreX: (left + right) / 2,
          contentCentreY: (top + bottom) / 2,
        };
      });

    const m = await measure();
    expect(
      Math.abs(m.contentCentreX - m.panelCentreX),
      `empty state horizontally centered at ${viewport.width}px`,
    ).toBeLessThanOrEqual(1);
    expect(
      Math.abs(m.contentCentreY - m.panelCentreY),
      `empty state vertically centered at ${viewport.width}px`,
    ).toBeLessThanOrEqual(1);
  }
});

// JUS-153: the breadcrumb, the folder filter and Refresh moved out of the
// toolbar into their own bar directly above it, so the toolbar carries actions
// only. At >=1024px both bars stay one row with no blank band or unintended
// wrap (the path group takes ~half the row, the search block the right side);
// below that the path bar may wrap, with the phone rows taking over at <=640px.
test("the path bar sits above the toolbar with no blank band at 1366-1500px", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

  const pathbar = page.locator(".fm-pathbar");
  const toolbar = page.locator(".fm-toolbar");
  await expect(pathbar).toHaveCount(1);

  // The path bar owns the breadcrumb, the filter and Refresh...
  await expect(pathbar.locator(".fm-path")).toHaveCount(1);
  await expect(pathbar.locator(".fm-search")).toHaveCount(1);
  await expect(pathbar.getByRole("button", { name: "Refresh" })).toHaveCount(1);
  // ...and the toolbar keeps only actions (JUS-160 left group: Upload, New,
  // Root selector, File operations; right group: Recycle bin, view toggle).
  await expect(toolbar.locator(".fm-path")).toHaveCount(0);
  await expect(toolbar.locator(".fm-search")).toHaveCount(0);
  await expect(
    toolbar.getByRole("button", { name: "File operations", exact: true }),
  ).toHaveCount(1);
  await expect(toolbar.locator(".fm-root-select")).toHaveCount(1);

  for (const width of [1024, 1366, 1440, 1500]) {
    await page.setViewportSize({ width, height: 900 });
    await settleLayout(page);
    const m = await page.evaluate(() => {
      const pb = document.querySelector(".fm-pathbar") as HTMLElement;
      const tb = document.querySelector(".fm-toolbar") as HTMLElement;
      const head = document.querySelector(".fm-head") as HTMLElement;
      const pr = pb.getBoundingClientRect();
      const tr = tb.getBoundingClientRect();
      return {
        aboveToolbar: pr.bottom <= tr.top + 1,
        pathbarToToolbar: tr.top - pr.bottom,
        toolbarToHeader: head.getBoundingClientRect().top - tr.bottom,
        // a wrapped extra row would roughly double a single small-control row
        pathbarHeight: pr.height,
        toolbarHeight: tr.height,
      };
    });
    expect(m.aboveToolbar, `path bar above the toolbar at ${width}px`).toBe(true);
    expect(
      m.pathbarToToolbar,
      `path-bar-to-toolbar blank band at ${width}px`,
    ).toBeCloseTo(32, 0);
    expect(
      m.toolbarToHeader,
      `16px band gap plus table border at ${width}px`,
    ).toBe(17);
    expect(m.pathbarHeight, `path bar is the 32px input-group row at ${width}px`)
      .toBeGreaterThanOrEqual(30);
    expect(m.pathbarHeight, `path bar stays one row at ${width}px`).toBeLessThanOrEqual(52);
    expect(m.toolbarHeight, `toolbar is one row at ${width}px`).toBeLessThanOrEqual(44);
  }
});

// JUS-208: in grid view the toolbar end group (Recycle bin + list/grid toggle)
// wrapped onto a second row at every desktop width, even with plenty of free
// space. The grid-only selection bar stayed mounted with visibility:hidden and
// reserved ~710px of layout width (count reserve plus every bulk button) while
// nothing was selected, and the grid-only sort toggle added the rest. Round 2:
// the idle toolbar is one row wherever the visible controls fit, and while a
// selection exists the cluster overlays the toolbar row as a contextual action
// bar instead of joining it — selecting never grows the toolbar (JUS-130/141).
test("the grid toolbar keeps its end group on the first row at desktop widths", async ({
  page,
}) => {
  for (const width of [1366, 1440, 1920]) {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${server1}/files`);
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();

    const toolbar = page.locator(".fm-toolbar");
    const box = (await toolbar.boundingBox())!;
    expect(box.height, `toolbar is one row at ${width}px`).toBe(32);

    const groups = toolbar.locator(".fm-toolbar-group");
    await expect(groups).toHaveCount(2);
    const leftBox = (await groups.nth(0).boundingBox())!;
    const endBox = (await groups.nth(1).boundingBox())!;
    expect(
      Math.abs(endBox.y - leftBox.y),
      `end group shares the row at ${width}px`,
    ).toBeLessThanOrEqual(1);
    expect(
      box.x + box.width - (endBox.x + endBox.width),
      `end group right-aligned at ${width}px`,
    ).toBeLessThanOrEqual(1);
  }
});

// JUS-208 round 2: selecting a grid tile must not shift the layout — the bulk
// cluster overlays the toolbar row, so the toolbar keeps its height and the
// first tile keeps its y, at desktop widths and on the phone.
test("selecting a grid tile never shifts the toolbar or the tiles", async ({
  page,
}) => {
  for (const width of [1366, 1920, 390]) {
    const height = width === 390 ? 844 : 900;
    await page.setViewportSize({ width, height });
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${server1}/files`);
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();

    const toolbar = page.locator(".fm-toolbar");
    const tile = page.locator(".fm-tile-wrap").first();
    const toolbarHeight = async (): Promise<number> =>
      (await toolbar.boundingBox())!.height;
    const tileY = async (): Promise<number> =>
      (await tile.boundingBox())!.y;
    // inert is presence-based (inert="false" still counts), so assert the
    // attribute's presence, never its value.
    const hasInert = (target: Locator): Promise<boolean> =>
      target.evaluate((element) => element.hasAttribute("inert"));
    expect(await toolbarHeight(), `idle toolbar is one row at ${width}px`).toBe(32);
    const beforeY = await tileY();

    // Idle the overlay is out of the a11y tree and tab order (inert and
    // hidden), and its actions are not reachable.
    const bulk = page.locator(".fm-grid-bulk");
    const scroller = page.locator(".fm-toolbar-scroll");
    expect(await hasInert(bulk), `idle overlay is inert at ${width}px`).toBe(true);
    await expect(bulk).toHaveCSS("visibility", "hidden");
    await expect(
      bulk.getByRole("button", { name: "Download", exact: true }),
    ).toBeHidden();

    await tile.click();
    await expect(page.locator(".fm-tile.is-selected")).toHaveCount(1);
    expect(
      await toolbarHeight(),
      `toolbar height unchanged on select at ${width}px`,
    ).toBe(32);
    expect(
      Math.abs((await tileY()) - beforeY),
      `first tile unmoved on select at ${width}px`,
    ).toBeLessThanOrEqual(1);

    // While the overlay shows, the covered scroll content goes inert (Tab
    // skips it, the tree exposes only the bar) and the overlay swallows the
    // row's pointer events: a hit test on the count text lands on the
    // overlay, never on the Upload/New control underneath.
    await expect(await hasInert(scroller), `covered content is inert at ${width}px`).toBe(true);
    await expect(await hasInert(bulk), `visible overlay leaves inert at ${width}px`).toBe(false);
    const hitInBulk = await page.evaluate(() => {
      const count = document.querySelector(
        ".fm-grid-bulk .fm-bulk-count",
      ) as HTMLElement | null;
      if (count === null) {
        return false;
      }
      const box = count.getBoundingClientRect();
      const hit = document.elementFromPoint(
        box.x + box.width / 2,
        box.y + box.height / 2,
      ) as HTMLElement | null;
      return hit !== null && hit.closest(".fm-grid-bulk") !== null;
    });
    expect(hitInBulk, `count hit lands on the overlay at ${width}px`).toBe(true);

    if (width === 390) {
      // The overlay row is 32px: every bulk button renders at the row height
      // and sits fully inside the overlay box (no 6px clip — the phone focus
      // ring is inset, so it cannot clip either).
      const bulkBox = (await bulk.boundingBox())!;
      const actions = bulk.locator(".fm-selection-actions").getByRole("button");
      const buttonCount = await actions.count();
      expect(buttonCount, "overlay renders its actions").toBeGreaterThan(0);
      for (let index = 0; index < buttonCount; index += 1) {
        const button = actions.nth(index);
        await button.scrollIntoViewIfNeeded();
        const box = (await button.boundingBox())!;
        expect(
          await button.evaluate((element) => getComputedStyle(element).height),
          `bulk button is the 32px row height at ${width}px`,
        ).toBe("32px");
        expect(
          box.y >= bulkBox.y - 1 &&
            box.y + box.height <= bulkBox.y + bulkBox.height + 1,
          `bulk button inside the overlay box at ${width}px`,
        ).toBe(true);
      }
    }

    if (width === 1366 || width === 390) {
      // Keyboard: Shift+Tab from the tile must land in the overlay (Clear is
      // its last control), never on a covered toolbar control — Tab skips the
      // inert content, and the bar stays reachable by keyboard while visible.
      let landed: { label: string; inBulk: boolean } | null = null;
      for (let press = 0; press < 10 && landed === null; press += 1) {
        await page.keyboard.press("Shift+Tab");
        landed = await page.evaluate(() => {
          const active = document.activeElement as HTMLElement | null;
          if (active === null || active === document.body) {
            return null;
          }
          if (active.closest(".fm-tile-wrap") !== null) {
            return null;
          }
          const labelled = active.closest("button") ?? active;
          return {
            label: (
              labelled.getAttribute("aria-label") ??
              labelled.textContent ??
              ""
            ).trim(),
            inBulk: active.closest(".fm-grid-bulk") !== null,
          };
        });
      }
      expect(landed, `Shift+Tab leaves the tile at ${width}px`).not.toBeNull();
      expect(landed!.label, `Shift+Tab reaches Clear at ${width}px`).toBe("Clear");
      expect(landed!.inBulk, `covered controls are Tab-skipped at ${width}px`).toBe(true);
      // The keyboard-focused control sits fully inside the overlay box, focus
      // ring included.
      const bulkBox = (await bulk.boundingBox())!;
      const focusedBox = (await page.locator(":focus").boundingBox())!;
      expect(
        focusedBox.y >= bulkBox.y - 1 &&
          focusedBox.y + focusedBox.height <= bulkBox.y + bulkBox.height + 1,
        `focused control inside the overlay box at ${width}px`,
      ).toBe(true);
    }

    // Delete and Clear stay reachable through the bar's own row: scroll each
    // into view, then assert it sits fully inside the viewport.
    const bar = page.locator(".fm-grid-bulk .fm-selection-actions");
    for (const name of ["Delete", "Clear"]) {
      const button = bar.getByRole("button", { name, exact: true });
      await button.scrollIntoViewIfNeeded();
      const box = (await button.boundingBox())!;
      expect(box.x, `${name} left inside viewport at ${width}px`).toBeGreaterThanOrEqual(-1);
      expect(
        box.x + box.width,
        `${name} right inside viewport at ${width}px`,
      ).toBeLessThanOrEqual(width + 1);
      expect(box.y, `${name} top inside viewport at ${width}px`).toBeGreaterThanOrEqual(-1);
      expect(
        box.y + box.height,
        `${name} bottom inside viewport at ${width}px`,
      ).toBeLessThanOrEqual(height + 1);
    }

    await bar.getByRole("button", { name: "Clear", exact: true }).click();
    await expect(page.locator(".fm-tile.is-selected")).toHaveCount(0);
    // Clearing restores the idle tree: the scroll content leaves inert duty
    // and the overlay returns to it.
    await expect(await hasInert(scroller), `scroll content uninerted on clear at ${width}px`).toBe(false);
    await expect(await hasInert(bulk), `overlay inert again on clear at ${width}px`).toBe(true);
    expect(
      await toolbarHeight(),
      `toolbar height unchanged on clear at ${width}px`,
    ).toBe(32);
    expect(
      Math.abs((await tileY()) - beforeY),
      `first tile unmoved on clear at ${width}px`,
    ).toBeLessThanOrEqual(1);
  }
});
