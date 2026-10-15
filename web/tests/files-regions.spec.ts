import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, getComputedStyle:readonly, HTMLElement:readonly, URL, window:readonly */
// JUS-157 region-separation proof: the committed webdist is served statically
// with the control-plane file API mocked in real Chromium. Measured claims: the
// five browser regions (header, tablist, path bar, toolbar, entry list) stack
// top-to-bottom and every adjacent pair has a distinct boundary — a border or a
// different background, token-derived (no hard-coded colour) — at 1440, 1024
// and 390px. The new bands must not clip the shared :focus-visible ring.

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
  borderTopColor: string;
  borderBottomColor: string;
  borderTopLeftRadius: string;
  borderTopRightRadius: string;
  overflowX: string;
  overflowY: string;
}

/** regionsOf measures the five browser regions and the two tokens in one pass. */
async function regionsOf(page: Page): Promise<{
  regions: Record<string, RegionBox>;
  cardRadius: string;
  surfaceWarm: string;
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
        borderTopColor: style.borderTopColor,
        borderBottomColor: style.borderBottomColor,
        borderTopLeftRadius: style.borderTopLeftRadius,
        borderTopRightRadius: style.borderTopRightRadius,
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
        header: region(".files-browser .n-card-header"),
        tablist: region(".fm-tabs"),
        pathbar: region(".fm-bar.fm-pathbar"),
        toolbar: region(".fm-bar.fm-toolbar"),
        panel: region(".table-wrap"),
      },
      cardRadius: getComputedStyle(
        document.querySelector(".files-browser") as HTMLElement,
      ).borderTopLeftRadius,
      surfaceWarm: rootStyle.getPropertyValue("--surface-warm").trim(),
      border: rootStyle.getPropertyValue("--border").trim(),
    };
  });
}

// JUS-157: the five regions stack in order and each adjacent pair has a visible
// boundary (a border on either side, or a different background) at every width.
for (const viewport of [
  { width: 1440, height: 900 },
  { width: 1024, height: 768 },
  { width: 390, height: 844 },
]) {
  test(`the browser regions have distinct boundaries at ${viewport.width}px`, async ({
    page,
  }) => {
    await openFiles(page, viewport.width, viewport.height);
    const { regions, cardRadius, surfaceWarm, border } = await regionsOf(page);
    const order = ["header", "tablist", "pathbar", "toolbar", "panel"] as const;

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

    // Each adjacent pair is separated by a real gap AND a boundary the eye can
    // see: a border drawn at the shared edge in a colour that differs from its
    // own fill, or a background step. Two regions with no border and the same
    // fill fail, so this can only pass while the bands really separate them.
    for (let index = 1; index < order.length; index += 1) {
      const upper = regions[order[index - 1]];
      const lower = regions[order[index]];
      const gap = lower.top - upper.bottom;
      expect(
        gap,
        `${order[index - 1]} -> ${order[index]} gap at ${viewport.width}px`,
      ).toBeGreaterThan(0);
      const borderAtBoundary =
        (upper.borderBottom > 0 &&
          normalizeColor(upper.borderBottomColor) !==
            normalizeColor(upper.background)) ||
        (lower.borderTop > 0 &&
          normalizeColor(lower.borderTopColor) !==
            normalizeColor(lower.background));
      const backgroundDiffers = upper.background !== lower.background;
      expect(
        borderAtBoundary || backgroundDiffers,
        `${order[index - 1]} -> ${order[index]} boundary at ${viewport.width}px`,
      ).toBe(true);
    }

    // F1/JUS-157: the warm header band must follow the card's rounded top
    // corners — a square band would bleed over the 8px radius.
    expect(Number.parseFloat(cardRadius), "card has a top radius").toBeGreaterThan(0);
    expect(
      regions.header.borderTopLeftRadius,
      "header top-left corner follows the card radius",
    ).toBe(cardRadius);
    expect(
      regions.header.borderTopRightRadius,
      "header top-right corner follows the card radius",
    ).toBe(cardRadius);

    // The separation is token-derived: the header band and the two bars paint
    // the deepest surface token, and the bars are ringed with the border token.
    expect(regions.header.background, "header uses --surface-warm").toBe(
      toRgb(surfaceWarm),
    );
    expect(regions.pathbar.background, "path bar uses --surface-warm").toBe(
      toRgb(surfaceWarm),
    );
    expect(regions.toolbar.background, "toolbar uses --surface-warm").toBe(
      toRgb(surfaceWarm),
    );
    for (const name of ["pathbar", "toolbar"] as const) {
      expect(regions[name].borderTop, `${name} has a top border`).toBeGreaterThan(0);
      expect(regions[name].borderBottom, `${name} has a bottom border`).toBeGreaterThan(0);
      expect(
        normalizeColor(regions[name].borderTopColor),
        `${name} border uses --border`,
      ).toBe(normalizeColor(toRgb(border)));
    }

    // The new bands must not clip focus rings: none of the regions added by
    // JUS-157 (header, tablist, path bar, toolbar) creates a clipping box.
    for (const name of ["header", "tablist", "pathbar", "toolbar"] as const) {
      expect(regions[name].overflowX, `${name} overflow-x`).toBe("visible");
      expect(regions[name].overflowY, `${name} overflow-y`).toBe("visible");
    }
  });
}

// JUS-157: the path band must leave the breadcrumb's shared :focus-visible ring
// room, so adding the band never clips the ring on the card's overflow-hidden
// content box (which is the real clip ancestor).
test("the path band keeps the breadcrumb focus ring unclipped", async ({ page }) => {
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

// JUS-157: the tab focus ring is drawn INSIDE the tab (outline-offset: -2px)
// because the scrolling strip clips at its edge. A new border or overflow on
// the region bands must never cut it: the whole ring paints inside the strip.
test("the region bands never clip the tab focus ring", async ({ page }) => {
  await openFiles(page, 1440, 900);
  const tab = page.locator('.fm-tabstrip [role="tab"][aria-selected="true"]');
  await tab.focus();
  await page.keyboard.press("ArrowLeft");
  await page.keyboard.press("ArrowRight");
  await expect(tab).toBeFocused();

  const ring = await page.evaluate(() => {
    const strip = document.querySelector(".fm-tabstrip") as HTMLElement | null;
    if (strip === null) return null;
    const tabEl = strip.querySelector<HTMLElement>(
      '[role="tab"][aria-selected="true"]',
    );
    if (tabEl === null) return null;
    const style = getComputedStyle(tabEl);
    const width = Number.parseFloat(style.outlineWidth);
    const offset = Number.parseFloat(style.outlineOffset);
    const box = tabEl.getBoundingClientRect();
    // The strip is the scroll container, so it clips at its own border box.
    const clip = strip.getBoundingClientRect();
    return {
      outline: style.outlineStyle,
      width,
      // Positive means the ring reaches past the clip edge (i.e. is cut off).
      overhang: Math.max(
        clip.top - (box.top - offset - width),
        box.bottom + offset + width - clip.bottom,
        clip.left - (box.left - offset - width),
        box.right + offset + width - clip.right,
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
