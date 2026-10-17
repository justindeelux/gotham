import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, getComputedStyle:readonly, HTMLElement:readonly, URL, window:readonly */
// JUS-205: token gaps separate the chrome bands; the navigation field and
// entry list own their borders. Check real layout and focus-ring clearance.

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

function folder(name: string) {
  return {
    name,
    kind: "directory",
    size: 4096,
    mode: 0o040755,
    modified_at: "2026-10-12T09:14:00Z",
    etag: `"${name}"`,
  };
}

function file(name: string) {
  return {
    name,
    kind: "file",
    size: 2458,
    mode: 0o100644,
    modified_at: "2026-10-12T09:02:00Z",
    etag: `"${name}"`,
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
      const filePath = resolve(webdistRoot, `.${path}`);
      if (!filePath.startsWith(webdistRoot)) {
        response.writeHead(403);
        response.end();
        return;
      }
      const body = await readFile(filePath);
      response.writeHead(200, {
        "content-type": mimeTypes[extname(filePath)] ?? "application/octet-stream",
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
async function openFiles(page: Page, width: number, height: number): Promise<void> {
  await page.setViewportSize({ width, height });
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
        entries: [folder("config"), folder("data"), file("app.yaml"), file("logo.png")],
        truncated: false,
        cursor: "",
      },
    });
  });
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label b").first()).toBeVisible();
}

interface RegionBox {
  top: number;
  bottom: number;
  background: string;
  borderTop: number;
  borderBottom: number;
  borderLeft: number;
  borderRight: number;
  borderTopColor: string;
  borderBottomColor: string;
  overflowX: string;
  overflowY: string;
}

/** regionsOf measures the five browser regions and the tokens in one pass. */
async function regionsOf(page: Page): Promise<{
  regions: Record<string, RegionBox>;
  cardBackground: string;
  border: string;
}> {
  return page.evaluate(() => {
    const read = (selector: string): RegionBox | null => {
      const el = document.querySelector(selector) as HTMLElement | null;
      if (el === null) return null;
      const style = getComputedStyle(el);
      const box = el.getBoundingClientRect();
      return {
        top: box.top,
        bottom: box.bottom,
        background: style.backgroundColor,
        borderTop: Number.parseFloat(style.borderTopWidth),
        borderBottom: Number.parseFloat(style.borderBottomWidth),
        borderLeft: Number.parseFloat(style.borderLeftWidth),
        borderRight: Number.parseFloat(style.borderRightWidth),
        borderTopColor: style.borderTopColor,
        borderBottomColor: style.borderBottomColor,
        overflowX: style.overflowX,
        overflowY: style.overflowY,
      };
    };
    const rootStyle = getComputedStyle(document.documentElement);
    const region = (selector: string): RegionBox => {
      const box = read(selector);
      if (box === null) throw new Error(`missing region: ${selector}`);
      return box;
    };
    return {
      regions: {
        contextbar: region(".fm-contextbar"),
        tablist: region(".fm-tabs"),
        pathbar: region(".fm-bar.fm-pathbar"),
        toolbar: region(".fm-bar.fm-toolbar"),
        panel: region(".fm-panel"),
      },
      cardBackground: getComputedStyle(
        document.querySelector(".files-browser") as HTMLElement,
      ).backgroundColor,
      border: rootStyle.getPropertyValue("--border").trim(),
    };
  });
}

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 1024, height: 768 },
  { width: 390, height: 844 },
]) {
  test(`the browser regions are separated by token spacing at ${viewport.width}px`, async ({
    page,
  }) => {
    await openFiles(page, viewport.width, viewport.height);
    const { regions, cardBackground, border } = await regionsOf(page);
    const order = ["contextbar", "tablist", "pathbar", "toolbar", "panel"] as const;

    // Every region has a height and stacks top-to-bottom without overlap.
    for (let index = 0; index < order.length; index += 1) {
      const box = regions[order[index]];
      expect(box.bottom - box.top, `${order[index]} has height`).toBeGreaterThan(0);
      if (index > 0) {
        expect(
          box.top,
          `${order[index]} starts below ${order[index - 1]}`,
        ).toBeGreaterThanOrEqual(regions[order[index - 1]].bottom - 1);
      }
    }

    // The plain context bar meets the tablist with a margin gap only (no rule
    // on either edge) — since JUS-160 it keeps only the agent-node slot, the
    // Root selector and the view toggle having moved into the toolbar.
    expect(
      regions.tablist.top - regions.contextbar.bottom,
      `contextbar -> tablist gap at ${viewport.width}px`,
    ).toBeGreaterThan(0);
    expect(
      regions.contextbar.borderBottom,
      `contextbar draws no bottom rule at ${viewport.width}px`,
    ).toBe(0);
    expect(
      regions.tablist.borderTop,
      `tablist draws no top rule at ${viewport.width}px`,
    ).toBe(0);

    const gaps = [
      ["tablist", "pathbar", 16],
      ["pathbar", "toolbar", viewport.width <= 640 ? 16 : 32],
      ["toolbar", "panel", 16],
    ] as const;
    for (const [upper, lower, expected] of gaps) {
      expect(regions[lower].top - regions[upper].bottom, `${upper} -> ${lower}`)
        .toBeCloseTo(expected, 0);
    }

    // Only the field and the list are boxed; no full-width rules crowd the bands.
    for (const name of ["pathbar", "toolbar", "panel"] as const) {
      expect(regions[name].borderTop, `${name} has no band divider`).toBe(0);
    }
    const field = page.locator(".fm-navfield");
    await expect(field).toHaveCSS("border-top-width", "1px");
    const fieldBorder = await field.evaluate((element) => getComputedStyle(element).borderTopColor);
    expect(normalizeColor(fieldBorder)).toBe(normalizeColor(toRgb(border)));
    await expect(page.locator(".table-wrap")).toHaveCSS("border-top-width", "1px");
    const fieldBox = (await field.boundingBox())!;
    for (const selector of [".fm-navback", ".fm-navrefresh"]) {
      const button = page.locator(selector);
      const box = (await button.boundingBox())!;
      expect(box.x - fieldBox.x).toBeGreaterThanOrEqual(8);
      expect(fieldBox.x + fieldBox.width - box.x - box.width).toBeGreaterThanOrEqual(8);
      expect(box.y - fieldBox.y).toBeGreaterThanOrEqual(8);
      expect(fieldBox.y + fieldBox.height - box.y - box.height).toBeGreaterThanOrEqual(8);
      await expect(button.locator(".n-button__border")).toHaveCount(0);
    }
    await test.info().attach(`files-chrome-${viewport.width}px`, {
      body: await page.screenshot({
        path: test.info().outputPath(`files-chrome-${viewport.width}px.png`),
        animations: "disabled",
      }),
      contentType: "image/png",
    });
    await expect(page.locator(".fm-tab-icon")).toHaveCount(1);
    await expect(page.locator('.fm-table .fm-mark[data-kind="folder"]')).toHaveCount(2);
    await page.getByRole("button", { name: "Grid view", exact: true }).click();
    await expect(page.locator('.fm-grid .fm-mark[data-kind="folder"]')).toHaveCount(2);

    // The pill tabs sit directly on the card without a boxed strip.
    expect(
      normalizeColor(regions.tablist.background),
      `tablist has no band background at ${viewport.width}px`,
    ).toBe("rgba(0,0,0,0)");
    for (const edge of ["borderTop", "borderBottom", "borderLeft", "borderRight"] as const) {
      expect(
        regions.tablist[edge],
        `tablist ${edge} draws no boxed border at ${viewport.width}px`,
      ).toBe(0);
    }

    // JUS-150: the JUS-157 tinted bands are gone — the context bar, path bar
    // and toolbar paint no background of their own, so the card surface shows
    // through (transparent) instead of a distinct band.
    for (const name of ["contextbar", "pathbar", "toolbar"] as const) {
      const background = normalizeColor(regions[name].background);
      expect(
        background === "rgba(0,0,0,0)" || background === normalizeColor(cardBackground),
        `${name} has no distinct band background at ${viewport.width}px`,
      ).toBe(true);
    }

    // The regions must not clip focus rings: none of them creates a clipping box.
    for (const name of ["contextbar", "tablist", "pathbar", "toolbar"] as const) {
      expect(regions[name].overflowX, `${name} overflow-x`).toBe("visible");
      expect(regions[name].overflowY, `${name} overflow-y`).toBe("visible");
    }
  });
}

// JUS-150: the path bar must leave the breadcrumb's shared :focus-visible ring
// room, so the plain row never clips the ring on the card's overflow-hidden
// content box (which is the real clip ancestor).
test("the path bar keeps the breadcrumb focus ring unclipped", async ({ page }) => {
  await openFiles(page, 1440, 900);
  const crumb = page.locator(".fm-crumb.is-current");
  await crumb.focus();
  // An arrow press is a no-op on a button; it only flags the keyboard modality
  // so :focus-visible matches (mirrors the tab-strip ring check).
  await page.keyboard.press("ArrowDown");
  await expect(crumb).toBeFocused();

  const ring = await page.evaluate(() => {
    const crumbEl = document.querySelector(
      ".fm-crumb.is-current",
    ) as HTMLElement | null;
    const clip = document.querySelector(
      ".files-browser .n-card-content",
    ) as HTMLElement | null;
    if (crumbEl === null || clip === null) return null;
    const style = getComputedStyle(crumbEl);
    const width = Number.parseFloat(style.outlineWidth);
    const offset = Number.parseFloat(style.outlineOffset);
    const box = crumbEl.getBoundingClientRect();
    // The content box has overflow: hidden, so it clips at its border box.
    const bounds = clip.getBoundingClientRect();
    return {
      outline: style.outlineStyle,
      width,
      // Positive means the ring reaches past the clip box (i.e. is cut off).
      overhang: Math.max(
        bounds.top - (box.top - offset - width),
        box.bottom + offset + width - bounds.bottom,
        bounds.left - (box.left - offset - width),
        box.right + offset + width - bounds.right,
      ),
    };
  });

  expect(ring).not.toBeNull();
  expect(ring!.outline).not.toBe("none");
  expect(ring!.width).toBeGreaterThan(0);
  expect(ring!.overhang).toBeLessThanOrEqual(0.5);
});

/** toRgb normalises a `#rrggbb` token (or an already-rgb value) to `rgb(...)`. */
function toRgb(value: string): string {
  if (value.startsWith("#") && value.length === 7) {
    const r = Number.parseInt(value.slice(1, 3), 16);
    const g = Number.parseInt(value.slice(3, 5), 16);
    const b = Number.parseInt(value.slice(5, 7), 16);
    return `rgb(${r}, ${g}, ${b})`;
  }
  return value;
}

/** normalizeColor compares colours across Chromium's `0.06`/`.06` spellings. */
function normalizeColor(value: string): string {
  return value.toLowerCase().replace(/\s+/g, "").replace(/0\./g, ".");
}
