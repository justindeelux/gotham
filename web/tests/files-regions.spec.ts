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
  left: number;
  right: number;
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

async function regionsOf(page: Page): Promise<{
  regions: Record<string, RegionBox>;
  cardBackground: string;
  border: string;
  surfaceWarm: string;
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
        left: box.left,
        right: box.right,
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
        card: region(".files-browser"),
        topbar: region(".fm-topbar"),
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
      surfaceWarm: rootStyle.getPropertyValue("--surface-warm").trim(),
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
    const { regions, cardBackground, border, surfaceWarm } = await regionsOf(page);
    const order = ["topbar", "pathbar", "toolbar", "panel"] as const;

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

    const tabMid = (regions.tablist.top + regions.tablist.bottom) / 2;
    const nodeMid = (regions.contextbar.top + regions.contextbar.bottom) / 2;
    expect(
      Math.abs(nodeMid - tabMid),
      `switcher centred on the tab row at ${viewport.width}px`,
    ).toBeLessThanOrEqual(1);
    expect(
      regions.contextbar.left - regions.tablist.right,
      `switcher sits right of the tabs at ${viewport.width}px`,
    ).toBeCloseTo(12, 0);
    expect(
      regions.contextbar.borderBottom,
      `contextbar draws no bottom rule at ${viewport.width}px`,
    ).toBe(0);
    expect(
      regions.tablist.borderTop,
      `tablist draws no top rule at ${viewport.width}px`,
    ).toBe(0);

    const gaps = [
      ["tablist", "pathbar", 0],
      ["pathbar", "toolbar", viewport.width <= 640 ? 16 : 32],
      ["toolbar", "panel", 16],
    ] as const;
    await expect(page.locator(".fm-bar.fm-pathbar")).toHaveCSS(
      "padding-top",
      "16px",
    );
    await expect(page.locator(".fm-bar.fm-pathbar")).toHaveCSS(
      "background-color",
      cardBackground,
    );
    for (const [upper, lower, expected] of gaps) {
      expect(regions[lower].top - regions[upper].bottom, `${upper} -> ${lower}`)
        .toBeCloseTo(expected, 0);
    }

    for (const name of ["pathbar", "toolbar", "panel"] as const) {
      expect(regions[name].borderTop, `${name} has no band divider`).toBe(0);
    }
    const field = page.locator(".fm-navfield");
    const strip = page.locator(".fm-navcrumbs");
    await expect(strip).toHaveCSS("border-top-width", "1px");
    await expect(strip).toHaveCSS("border-bottom-width", "1px");
    const fieldBorder = await strip.evaluate((element) => getComputedStyle(element).borderTopColor);
    expect(normalizeColor(fieldBorder)).toBe(normalizeColor(toRgb(border)));
    await expect(page.locator(".table-wrap")).toHaveCSS("border-top-width", "1px");
    const fieldBox = (await field.boundingBox())!;
    expect(fieldBox.height, "path bar is one 32px row").toBe(32);
    for (const selector of [".fm-navback", ".fm-navrefresh"]) {
      const button = page.locator(selector);
      await expect(button.locator(".n-button__border")).toHaveCount(1);
      const box = (await button.boundingBox())!;
      expect(box.height, `${selector} fills the row height`).toBe(32);
    }
    const backBox = (await page.locator(".fm-navback").boundingBox())!;
    const refreshBox = (await page.locator(".fm-navrefresh").boundingBox())!;
    expect(backBox.x - fieldBox.x, "back attaches at the row start").toBeLessThanOrEqual(1);
    expect(
      fieldBox.x + fieldBox.width - refreshBox.x - refreshBox.width,
      "refresh attaches at the row end",
    ).toBeLessThanOrEqual(1);
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

    expect(
      regions.topbar.top - regions.card.top,
      `band flush with card top at ${viewport.width}px`,
    ).toBeLessThanOrEqual(1);
    expect(
      regions.card.left - regions.topbar.left,
      `band bleeds to the card left edge at ${viewport.width}px`,
    ).toBeLessThanOrEqual(1);
    expect(
      regions.topbar.right - regions.card.right,
      `band bleeds to the card right edge at ${viewport.width}px`,
    ).toBeLessThanOrEqual(1);

    expect(
      normalizeColor(regions.topbar.background),
      `header band uses the warm token at ${viewport.width}px`,
    ).toBe(normalizeColor(toRgb(surfaceWarm)));
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

    for (const name of ["contextbar", "pathbar", "toolbar"] as const) {
      const background = normalizeColor(regions[name].background);
      expect(
        background === "rgba(0,0,0,0)" || background === normalizeColor(cardBackground),
        `${name} has no distinct band background at ${viewport.width}px`,
      ).toBe(true);
    }

    for (const name of ["topbar", "contextbar", "tablist", "pathbar", "toolbar"] as const) {
      expect(regions[name].overflowX, `${name} overflow-x`).toBe("visible");
      expect(regions[name].overflowY, `${name} overflow-y`).toBe("visible");
    }
    if (viewport.width <= 480) {
      await expect(page.locator(".fm-toolbar-scroll")).toHaveCSS("overflow-x", "auto");
      await expect(page.locator(".fm-toolbar-scroll")).toHaveCSS("overflow-y", "hidden");
    }
  });
}

test("the path bar keeps the breadcrumb focus ring unclipped", async ({ page }) => {
  await openFiles(page, 1440, 900);
  const crumb = page.locator(".fm-crumb.is-current");
  await crumb.focus();
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
    const bounds = clip.getBoundingClientRect();
    return {
      outline: style.outlineStyle,
      width,
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

function toRgb(value: string): string {
  if (value.startsWith("#") && value.length === 7) {
    const r = Number.parseInt(value.slice(1, 3), 16);
    const g = Number.parseInt(value.slice(3, 5), 16);
    const b = Number.parseInt(value.slice(5, 7), 16);
    return `rgb(${r}, ${g}, ${b})`;
  }
  return value;
}

function normalizeColor(value: string): string {
  return value.toLowerCase().replace(/\s+/g, "").replace(/0\./g, ".");
}
