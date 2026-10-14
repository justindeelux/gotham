import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, URL, window:readonly */
// JUS-118/119/120 layout proof: the committed webdist is served statically with
// the control-plane file API mocked in real Chromium. Measured claims:
//   - JUS-118 the browser panel fills the viewport and only the entry list
//     scrolls (no page-level scroll) at desktop and mobile widths.
//   - JUS-119/134 the page-top breadcrumb is gone and the agent-node selector
//     lives in the browser card header (it switches to another node's files).
//   - JUS-120 the toolbar groups New file/New folder under one dropdown, keeps
//     bulk tools on the right, and collapses to labelled icon-only buttons on
//     small screens.
//   - JUS-131 the permissions dialog shows the bits grid first with the octal
//     field below it, and drops the explanatory copy.
//   - JUS-135 the phone toolbar keeps the list at >=60% of the viewport with a
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

  // JUS-134: the agent-node selector decides the listing, so it lives in the
  // browser card header next to the Root selector, not a bar at the page bottom.
  const node = page.locator(".fm-node__select");
  await expect(node).toBeVisible();
  await expect(node).toContainText("prod-01");
  const nodeBox = (await node.boundingBox())!;
  const rootBox = (await page.locator(".fm-root-select").boundingBox())!;
  expect(nodeBox.x).toBeLessThan(rootBox.x);
  expect(nodeBox.y).toBeLessThan(rootBox.y + rootBox.height);

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

test("the header node selector stays reachable at desktop and mobile", async ({
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
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 768, height: 1024 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${server1}/files`);
    // Select the whole listing so every bulk action is enabled and Clear shows.
    await page.locator(".fm-head .n-checkbox").click();

    const toolbar = page.locator(".fm-toolbar");
    const toolbarBox = await toolbar.boundingBox();
    expect(toolbarBox, `toolbar missing at ${viewport.width}px`).not.toBeNull();
    await expect(page.locator(".fm-bulk-count")).toContainText("60 selected");

    // JUS-135: on a phone the bulk actions live in one horizontally scrolling
    // row (so the toolbar never wraps into the list), so each action is
    // scrolled into view before it is measured.
    const scrolling = viewport.width <= 640;
    for (const name of bulkActions) {
      const button = toolbar.getByRole("button", { name, exact: true });
      if (scrolling) {
        await button.scrollIntoViewIfNeeded();
      }
      await assertReachable(
        page,
        button,
        toolbarBox!,
        viewport,
        name,
        `${name}@${viewport.width}`,
      );
    }

    if (scrolling) {
      // The row is the intended scroll region: it must scroll so Delete/Clear
      // stay reachable, and every action is a 44px touch target.
      const overflow = await page.evaluate(() => {
        const row = document.querySelector(".fm-selection-actions") as HTMLElement | null;
        return row ? row.scrollWidth - row.clientWidth : null;
      });
      expect(overflow, "phone bulk row scrolls").not.toBeNull();
      expect(overflow!).toBeGreaterThan(0);
      const height = await toolbar
        .getByRole("button", { name: "Delete", exact: true })
        .evaluate((element) => element.getBoundingClientRect().height);
      expect(height).toBeGreaterThanOrEqual(44);
    } else {
      // Nothing in the toolbar (buttons or the count) spills past its right edge.
      const spill = await page.evaluate(() => {
        const bar = document.querySelector(".fm-toolbar") as HTMLElement | null;
        if (bar === null) {
          return null;
        }
        const right = bar.getBoundingClientRect().right;
        return [...bar.querySelectorAll("button, .fm-bulk-count")]
          .map((element) => element.getBoundingClientRect().right - right)
          .reduce((max, value) => Math.max(max, value), Number.NEGATIVE_INFINITY);
      });
      expect(spill, `no toolbar spill at ${viewport.width}px`).not.toBeNull();
      expect(spill!).toBeLessThanOrEqual(1);
    }
  }
});

// JUS-135: the phone toolbar must not eat the list. With a selection active
// (the tallest chrome state) the entry list keeps at least 60% of an iPhone-14
// viewport.
// JUS-134/JUS-141: selecting a row must not reflow the toolbar, or the second
// click of a double-click lands on a different row. The count's width follows
// the folder's content (its empty state reserves the folder's widest summary),
// so the toolbar keeps the same height before and after the first click at
// every width.
test("selecting a row never shifts the list at any width", async ({ page }) => {
  for (const width of [768, 900, 1024, 1100, 1180, 1280, 1440]) {
    await page.setViewportSize({ width, height: 1024 });
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${server1}/files`);
    await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

    const firstRowY = () =>
      page.evaluate(
        () =>
          (
            document.querySelector(
              ".fm-table .fm-row:not(.fm-head)",
            ) as HTMLElement
          ).getBoundingClientRect().y,
      );
    const before = await firstRowY();

    // The first click of a double-click selects the row (JUS-130).
    await page.locator(".fm-label").first().click();
    await expect(page.locator(".fm-bulk-count")).toContainText("1 selected");
    const selected = await firstRowY();
    expect(
      Math.abs(selected - before),
      `row shift on select at ${width}px`,
    ).toBeLessThanOrEqual(1);

    // Clearing the selection returns to the reserved empty state.
    await page.getByRole("button", { name: "Clear", exact: true }).click();
    const cleared = await firstRowY();
    expect(
      Math.abs(cleared - before),
      `row shift on clear at ${width}px`,
    ).toBeLessThanOrEqual(1);
  }
});

test("the entry list keeps at least 60% of a phone viewport with a selection", async ({
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
  expect(share, "entry list share of the phone viewport").toBeGreaterThanOrEqual(0.6);
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

  // JUS-128/134: no separate bulk bar and no disabled row — the bulk actions
  // are hidden (not disabled) until entries are selected.
  await expect(page.locator(".fm-bulk")).toHaveCount(0);
  const toolbar = page.locator(".fm-toolbar");
  const bulkDownload = toolbar.getByRole("button", { name: "Download", exact: true });
  await expect(bulkDownload).toBeHidden();

  // Select the whole listing so the bulk actions appear and the count shows.
  await page.locator(".fm-head .n-checkbox").click();
  await expect(bulkDownload).toBeVisible();
  await expect(toolbar.locator(".fm-bulk-count")).toContainText("60 selected");

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
  await expect(head).toHaveCSS("background-color", "rgb(43, 45, 49)");
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
  await expect(page.locator(".fm-foot")).toContainText("1 folders");
  await expect(page.locator(".fm-foot")).not.toContainText("symlink");

  // Opening the filtered folder clears the needle: its own listing is not
  // silently hidden behind the stale filter.
  await page.locator(".fm-label").filter({ hasText: "etc" }).dblclick();
  await expect(page.locator(".fm-label").filter({ hasText: "hosts" })).toBeVisible();
  await expect(search).toHaveValue("");
  await expect(page.locator(".fm-foot")).toContainText("1 entries");

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
  const subtitle = page.locator(".fm-table .fm-label span").first();
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
