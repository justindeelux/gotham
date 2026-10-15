import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, getComputedStyle:readonly, HTMLElement:readonly, URL, window:readonly */
// JUS-150 region-separation proof: the committed webdist is served statically
// with the control-plane file API mocked in real Chromium. Measured claims: the
// five browser regions (header, tablist, path bar, toolbar, entry list) stack
// top-to-bottom, every adjacent pair is joined by a single 1px --border rule,
// and none of the regions paints a band of its own — the header, tablist, path
// bar and toolbar stay on the card surface (the tablist also draws no boxed
// border) at 1440, 1024 and 390px. The path bar must not clip the breadcrumb's
// shared :focus-visible ring.

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
        header: region(".files-browser .n-card-header"),
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

// JUS-150: the five regions stack in order and each adjacent pair is joined by a
// single 1px --border rule; no region paints a band of its own at every width.
for (const viewport of [
  { width: 1440, height: 900 },
  { width: 1024, height: 768 },
  { width: 390, height: 844 },
]) {
  test(`the browser regions are separated by divider rules at ${viewport.width}px`, async ({
    page,
  }) => {
    await openFiles(page, viewport.width, viewport.height);
    const { regions, cardBackground, border } = await regionsOf(page);
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

    // Each adjacent pair is separated by a real gap AND a 1px --border rule the
    // eye can see. The gap rules out a coincident double line; the pair must
    // share at least one rule, drawn at the shared edge by the upper region's
    // bottom border or the lower region's top border.
    for (let index = 1; index < order.length; index += 1) {
      const upper = regions[order[index - 1]];
      const lower = regions[order[index]];
      const gap = lower.top - upper.bottom;
      expect(
        gap,
        `${order[index - 1]} -> ${order[index]} gap at ${viewport.width}px`,
      ).toBeGreaterThan(0);
      const upperRule = upper.borderBottom > 0;
      const lowerRule = lower.borderTop > 0;
      expect(
        upperRule || lowerRule,
        `${order[index - 1]} -> ${order[index]} divider at ${viewport.width}px`,
      ).toBe(true);
      // A shared edge must not carry two stacked rules (one divider per boundary).
      expect(
        upperRule && lowerRule && gap < 2,
        `${order[index - 1]} -> ${order[index]} double line at ${viewport.width}px`,
      ).toBe(false);
    }

    // JUS-150: the four dividers are plain 1px --border rules, each owned by one
    // region: the header's bottom edge, then the top edge of the path bar, the
    // toolbar and the entry list. The tablist itself draws no rule of its own.
    const dividers = [
      ["header", "borderBottom"],
      ["pathbar", "borderTop"],
      ["toolbar", "borderTop"],
      ["panel", "borderTop"],
    ] as const;
    for (const [name, edge] of dividers) {
      const box = regions[name];
      const width = edge === "borderTop" ? box.borderTop : box.borderBottom;
      expect(width, `${name} ${edge} is 1px`).toBe(1);
      const colour = edge === "borderTop" ? box.borderTopColor : box.borderBottomColor;
      expect(normalizeColor(colour), `${name} ${edge} uses --border`).toBe(
        normalizeColor(toRgb(border)),
      );
    }

    // JUS-150: the tablist region is a divider-separated row, not a boxed band —
    // it paints no background of its own and draws no border on any edge (the
    // dividers around it belong to the header above and the path bar below).
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

    // JUS-150: the JUS-157 tinted bands are gone — the header, path bar and
    // toolbar paint no background of their own, so the card surface shows
    // through (transparent) instead of a distinct band.
    for (const name of ["header", "pathbar", "toolbar"] as const) {
      const background = normalizeColor(regions[name].background);
      expect(
        background === "rgba(0,0,0,0)" || background === normalizeColor(cardBackground),
        `${name} has no distinct band background at ${viewport.width}px`,
      ).toBe(true);
    }

    // The regions must not clip focus rings: none of them creates a clipping box.
    for (const name of ["header", "tablist", "pathbar", "toolbar"] as const) {
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
