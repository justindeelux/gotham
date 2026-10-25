import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, requestAnimationFrame:readonly, URL, window:readonly, getComputedStyle:readonly */

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

async function mockApi(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: { user: session.user } });
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
    await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

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

    await expect
      .poll(async () => {
        const box = await card.boundingBox();
        return box ? box.y + box.height : 0;
      })
      .toBeGreaterThan(viewport.height - 120);

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

  await expect(page.locator(".breadcrumb")).toHaveCount(0);

  await expect(page.locator(".fm-contextbar .fm-field-label")).toHaveCount(0);
  const toolbarCaptions = page.locator(".fm-toolbar .fm-field-label");
  await expect(toolbarCaptions).toHaveCount(1);
  await expect(toolbarCaptions).toHaveText("Root");
  const toolbarSizes = await toolbarCaptions.evaluateAll((elements) =>
    elements.map((element) => getComputedStyle(element).fontSize),
  );
  expect(toolbarSizes).toEqual(["12px"]);

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
  expect(Math.abs(nodeBox.y - tabsBox.y)).toBeLessThanOrEqual(8);
  expect(nodeBox.x).toBeGreaterThanOrEqual(tabsBox.x + tabsBox.width);
  expect(nodeBox.y + nodeBox.height).toBeLessThanOrEqual(toolbarBox.y + 1);
  expect(rootBox.y).toBeGreaterThanOrEqual(toolbarBox.y);

  await node.click();
  const offline = page.locator(".n-base-select-option", { hasText: "prod-03" });
  await expect(offline).toHaveClass(/disabled/);
  await expect(offline).toContainText("Offline");

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
    await page.locator(".fm-head .n-checkbox").click();

    const bar = page.locator(".fm-head-bulk");
    const barBox = await bar.boundingBox();
    expect(barBox, `bulk bar missing at ${viewport.width}px`).not.toBeNull();
    await expect(page.locator(".fm-bulk-count")).toContainText("60 selected");

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
      expect(overflow, `bulk row scrolls at ${viewport.width}px`).toBeGreaterThan(0);
    }
    if (viewport.width <= 640) {
      const height = await bar
        .getByRole("button", { name: "Delete", exact: true })
        .evaluate((element) => element.getBoundingClientRect().height);
      expect(height).toBeGreaterThanOrEqual(44);
    }
    if (!scrolling) {
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

async function firstRowY(page: Page): Promise<number> {
  return page.evaluate(
    () =>
      (
        document.querySelector(".fm-table .fm-row:not(.fm-head)") as HTMLElement
      ).getBoundingClientRect().y,
  );
}

async function installListing(page: Page, entries: unknown[]): Promise<void> {
  await page.route(`**/api/v1/servers/${server1}/files/list*`, async (route) => {
    await route.fulfill({ json: { entries, truncated: false, cursor: "" } });
  });
}

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

async function settleLayout(page: Page): Promise<void> {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) =>
        requestAnimationFrame(() => requestAnimationFrame(() => resolve())),
      ),
  );
}

async function countCellWidth(page: Page): Promise<number> {
  return page
    .locator(".fm-bulk-count")
    .evaluate((element) => element.getBoundingClientRect().width);
}

async function axColumnHeaders(page: Page): Promise<number> {
  const snapshot = await page.locator(".fm-head").ariaSnapshot();
  return snapshot
    .split("\n")
    .filter((line) => line.includes("columnheader")).length;
}

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

    await page.locator(".fm-selection-actions .n-button").last().click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(0);
    expect(
      Math.abs((await firstRowY(page)) - beforeY),
      `row shift on clear at ${width}px`,
    ).toBeLessThanOrEqual(1);
  }
}

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
  await expect(page.locator(".fm-bulk-count-reserve")).toContainText("1500");

  const widths: number[] = [];
  for (let width = 700; width <= 1500; width += 6) {
    widths.push(width);
  }
  await assertNoRowShift(page, widths);
});

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

test("the bulk bar replaces the header row with no reserved band", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1876, height: 1080 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

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
  expect(spacing.toolbarHeight, "desktop toolbar is one row").toBe(32);
  expect(spacing.gap, "16px band gap plus table border").toBe(17);

  await page.locator(".fm-label").first().click();
  await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "hidden");
  await expect(page.locator(".fm-head-bulk")).toHaveCSS("visibility", "visible");
  await expect(page.locator(".fm-head-bulk")).toContainText("1 selected");
  expect(await axColumnHeaders(page), "one set of header cells (selected)").toBe(2);

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

  await expect(page.locator(".fm-sort-toggle")).toHaveCount(0);
  await expect(page.locator(".fm-bulk-sort")).toBeHidden();

  await page.locator(".fm-label").first().click();
  await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "hidden");
  const sort = page.locator(".fm-bulk-sort");
  await expect(sort).toBeVisible();
  await expect(sort).toHaveAttribute("aria-label", /Name/);

  await sort.click();
  await page.getByRole("menuitem", { name: "Size", exact: true }).click();
  await expect(sort).toHaveAttribute("aria-label", /Size/);
  await expect(
    page.locator(".fm-table .fm-row:not(.fm-head)").first().locator("b"),
  ).toHaveText("bbb.txt");

  await sort.focus();
  await page.keyboard.press("Enter");
  await page.getByRole("menuitem", { name: "Name", exact: true }).click();
  await expect(
    page.locator(".fm-table .fm-row:not(.fm-head)").first().locator("b"),
  ).toHaveText("aaa.txt");

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

test("on a phone the clipboard chip and the bulk bar never wrap", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);

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

test("the server rail collapses into a phone drawer", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label").first()).toBeVisible();

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

  await expect(page.locator(".fm-bulk")).toHaveCount(0);
  const bar = page.locator(".fm-head-bulk");
  const bulkDownload = bar.getByRole("button", { name: "Download", exact: true });
  await expect(bulkDownload).toBeHidden();

  await page.locator(".fm-head .n-checkbox").click();
  await expect(bulkDownload).toBeVisible();
  await expect(page.locator(".fm-bulk-count")).toContainText("60 selected");

  const count = await page.locator(".fm-bulk-count").boundingBox();
  const compress = await bar
    .getByRole("button", { name: "Compress…", exact: true })
    .boundingBox();
  expect(count).not.toBeNull();
  expect(compress).not.toBeNull();
  expect(compress!.x).toBeGreaterThan(count!.x + count!.width);

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

test("the minimized editor rail icon never covers the header node switcher", async ({
  page,
}) => {
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${server1}/files`);

    await page.locator(".fm-label").first().focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".ed-window")).toBeVisible({ timeout: 15_000 });
    await page.locator(".ed-window-min").click();
    await expect(page.locator(".ed-window")).toBeHidden();
    if (viewport.width > 1024) {
      await expect(page.locator(".rail-btn--editor")).toBeVisible();
    } else {
      await expect(page.locator(".ed-window")).toHaveCount(1);
    }

    await expect(page.locator(".ed-dock")).toHaveCount(0);
    const hit = await page.evaluate(() => {
      const select = document.querySelector(".fm-node__select");
      if (!(select instanceof HTMLElement)) {
        return null;
      }
      const box = select.getBoundingClientRect();
      const target = document.elementFromPoint(
        box.x + box.width / 2,
        box.y + box.height / 2,
      );
      return {
        hitInsideSelect: target !== null && select.contains(target),
      };
    });
    expect(hit).not.toBeNull();
    expect(hit!.hitInsideSelect).toBe(true);
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

  const grid = await dialog.locator(".perms-grid").boundingBox();
  const octal = await dialog.locator(".octal input").boundingBox();
  expect(grid).not.toBeNull();
  expect(octal).not.toBeNull();
  expect(octal!.y).toBeGreaterThan(grid!.y + grid!.height);

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
  expect(headBox.y).toBeGreaterThanOrEqual(wrapBox.y - 1);
  expect(headBox.y).toBeLessThan(wrapBox.y + 4);

  await head.hover();
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

  await expect(page.locator(".fm-foot")).toContainText("1 of 3 entries");
  await expect(page.locator(".fm-foot")).toContainText("1 folder");
  await expect(page.locator(".fm-foot")).not.toContainText("symlink");

  await page.locator(".fm-label").filter({ hasText: "etc" }).dblclick();
  await expect(page.locator(".fm-label").filter({ hasText: "hosts" })).toBeVisible();
  await expect(search).toHaveValue("");
  await expect(page.locator(".fm-foot")).toContainText("1 entry");

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

  await expect(page.locator(".fm-table").getByText("→ usr/bin")).toBeVisible();
  const subtitle = page.locator(".fm-table .fm-target").first();
  await expect(subtitle).toHaveCSS("text-overflow", "ellipsis");
  await expect(subtitle).toHaveCSS("white-space", "nowrap");
  await expect(subtitle).toHaveAttribute("title", "→ usr/bin");
  await page.getByRole("button", { name: "Grid view" }).click();
  await expect(page.locator(".fm-grid").getByText("Symlink → usr/bin")).toBeVisible();
  await page.getByRole("button", { name: "List view" }).click();

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

  await expect(page.locator(".n-message")).toContainText("Could not open the target of bin");
  await expect(page.locator(".fm-table").getByText("→ usr/bin")).toBeVisible();
  await expect(page.locator(".fm-state")).toHaveCount(0);
});

test("no blank band between the toolbar and the header at desktop widths", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await installListing(page, [
    { name: "storefront-8f2c1ab", kind: "directory", size: 4096, mode: 0o040755, modified_at: "2026-10-12T09:02:00Z", etag: '"d"' },
    ...manyEntries(),
  ]);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await page.locator(".fm-label", { hasText: "storefront-8f2c1ab" }).dblclick();
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(61);

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
  const hintBox = (await hint.boundingBox())!;
  const rowBox = (await row.boundingBox())!;
  expect(hintBox.x + hintBox.width).toBeGreaterThanOrEqual(
    rowBox.x + rowBox.width - 2,
  );

  await row.evaluate((el) => {
    el.scrollLeft = el.scrollWidth;
  });
  for (const name of ["Delete", "Clear"]) {
    const box = (await page.getByRole("button", { name, exact: true }).boundingBox())!;
    expect(box.x, `${name} left inside the viewport`).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width, `${name} right inside the viewport`).toBeLessThanOrEqual(391);
  }
});

test("the scroll chevron shows only while the action row overflows", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1876, height: 1080 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);
  await page.locator(".fm-head .n-checkbox").click();
  await expect(page.locator(".fm-bulk-count")).toContainText("60 selected");

  const row = page.locator(".fm-selection-actions");
  expect(await row.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(1);
  await expect(page.locator(".fm-scroll-hint")).toHaveCount(0);

  await page.setViewportSize({ width: 390, height: 844 });
  await settleLayout(page);
  await expect(page.locator(".fm-scroll-hint")).toBeVisible();

  await row.evaluate((el) => {
    el.scrollLeft = el.scrollWidth;
  });
  await expect(page.locator(".fm-scroll-hint")).toHaveCount(0);
});

test("the always-on toolbar groups never open an empty row", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-table .fm-row:not(.fm-head)")).toHaveCount(60);
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
        heightCap:
          content +
          Math.max(0, bands.length - 1) *
            (Number.parseFloat(style.rowGap || "0") || 0) +
          (Number.parseFloat(style.borderTopWidth || "0") || 0) +
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
      expect(maxHeight, `no empty toolbar row at ${width}px`).toBeGreaterThanOrEqual(20);
    }
    expect(
      layout.toolbarHeight,
      `toolbar height backed by content rows at ${width}px`,
    ).toBeLessThanOrEqual(layout.heightCap);
    if (width <= 480) {
      expect(layout.rows, "phone toolbar is one row").toBe(1);
      expect(layout.toolbarHeight, "phone toolbar has no orphan rows").toBe(32);
      await expect(page.locator(".fm-toolbar-scroll")).toHaveCSS("overflow-x", "auto");
    } else {
      expect(layout.spill, `no toolbar spill at ${width}px`).toBeLessThanOrEqual(1);
    }
    expect(layout.gap, `16px band gap plus table border at ${width}px`).toBe(17);
  }
});

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

  const sub = row.locator(".fm-name-sub");
  await expect(sub.locator(".fm-target")).toHaveText("→ /nonexistent/pretty-long-target");
  await expect(sub.locator(".fm-row-sub")).toHaveText(/\S/);
  const labelBox = (await row.locator(".fm-label").boundingBox())!;
  const subBox = (await sub.boundingBox())!;
  expect(
    subBox.y,
    "the target + time sit below the name",
  ).toBeGreaterThanOrEqual(labelBox.y + labelBox.height - 2);

  const name = row.locator(".fm-label b");
  const truncated = await name.evaluate((el) => el.scrollWidth > el.clientWidth + 1);
  expect(truncated, "the symlink name is fully readable").toBe(false);
});

test("the compact sort menu does not reopen after a width change", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  const sort = page.getByRole("button", { name: /^Sort:/ });
  await sort.click();
  await expect(page.getByRole("menu", { name: "Sort" })).toBeVisible();
  await expect(sort).toHaveAttribute("aria-expanded", "true");

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

test("a long owner clips inside the Owner/Mode cell without reaching Size", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1366, height: 900 });
  await mockApi(page);
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
      "systemd-network / 755",
    );
    expect(m.textOverflow, `clipped with an ellipsis at ${width}px`).toBe(
      "ellipsis",
    );
    expect(m.overflow, `overflow hidden at ${width}px`).toBe("hidden");
    expect(m.modeWidth, `mode cell within the track at ${width}px`).toBeLessThanOrEqual(110.5);
    expect(m.modeRight, `mode cell left of Size at ${width}px`).toBeLessThanOrEqual(
      m.sizeLeft + 0.5,
    );
    expect(m.headRight, `header within the track at ${width}px`).toBeLessThanOrEqual(
      m.headSizeLeft + 0.5,
    );
  }
});

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

  await page.setViewportSize({ width: 1024, height: 768 });
  await settleLayout(page);
  await expect(table).toHaveAttribute("aria-colcount", "6");
  await expect(bar).toHaveAttribute("aria-colspan", "5");

  await page.setViewportSize({ width: 390, height: 844 });
  await settleLayout(page);
  await expect(table).toHaveAttribute("aria-colcount", "4");
  await expect(bar).toHaveAttribute("aria-colspan", "3");
});

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

  await expect(pathbar.locator(".fm-path")).toHaveCount(1);
  await expect(pathbar.locator(".fm-search")).toHaveCount(1);
  await expect(pathbar.getByRole("button", { name: "Refresh" })).toHaveCount(1);
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
    const hasInert = (target: Locator): Promise<boolean> =>
      target.evaluate((element) => element.hasAttribute("inert"));
    expect(await toolbarHeight(), `idle toolbar is one row at ${width}px`).toBe(32);
    const beforeY = await tileY();

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
      const bulkBox = (await bulk.boundingBox())!;
      const focusedBox = (await page.locator(":focus").boundingBox())!;
      expect(
        focusedBox.y >= bulkBox.y - 1 &&
          focusedBox.y + focusedBox.height <= bulkBox.y + bulkBox.height + 1,
        `focused control inside the overlay box at ${width}px`,
      ).toBe(true);
    }

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
