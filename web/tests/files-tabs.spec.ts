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

function file(name: string, mode = 0o100644) {
  return {
    name,
    kind: "file",
    size: 2458,
    mode,
    modified_at: "2026-10-12T09:02:00Z",
    etag: `"${name}"`,
  };
}

function folder(name: string) {
  return {
    name,
    kind: "directory",
    size: 0,
    mode: 0o040755,
    modified_at: "2026-10-12T09:14:00Z",
    etag: `"${name}"`,
  };
}

function listingFor(path: string) {
  if (path === "alpha") {
    return [file("a.txt")];
  }
  if (path === "beta") {
    return [file("b.txt")];
  }
  return [folder("alpha"), folder("beta"), file("root.txt")];
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

async function openFiles(page: Page): Promise<void> {
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
    await route.fulfill({
      json: { entries: listingFor(path), truncated: false, cursor: "" },
    });
  });

  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label b").first()).toBeVisible();
}

function rowFor(page: Page, name: string) {
  return page.locator(".fm-table .fm-row", {
    has: page.locator(`b:text-is("${name}")`),
  });
}

function tabs(page: Page) {
  return page.locator('.fm-tabstrip [role="tab"]');
}

function activeTab(page: Page) {
  return page.locator('.fm-tabstrip [role="tab"][aria-selected="true"]');
}

async function openExtraTab(page: Page): Promise<void> {
  const before = await tabs(page).count();
  await page.locator(".fm-tab-add").click();
  await expect(tabs(page)).toHaveCount(before + 1);
}

test("two tabs keep independent paths and the tab bar is a real tablist", async ({
  page,
}) => {
  await openFiles(page);

  const tablist = page.getByRole("tablist", { name: "Browser tabs" });
  await expect(tablist).toBeVisible();
  await expect(tabs(page)).toHaveCount(1);

  await rowFor(page, "alpha").locator(".fm-label").dblclick();
  await expect(page.locator(".fm-label b").first()).toHaveText("a.txt");

  await openExtraTab(page);
  await expect(tabs(page)).toHaveCount(2);
  await expect(activeTab(page)).toHaveText("volumes");
  await expect(page.locator(".fm-label b").first()).toHaveText("alpha");

  await tabs(page).first().click();
  await expect(activeTab(page)).toHaveText("alpha");
  await expect(page.locator(".fm-label b").first()).toHaveText("a.txt");

  await tabs(page).nth(1).click();
  await expect(activeTab(page)).toHaveText("volumes");
  await expect(page.locator(".fm-label b").first()).toHaveText("alpha");

  const lastTab = await tabs(page).nth(1).boundingBox();
  const addBox = await page.locator(".fm-tab-add").boundingBox();
  expect(lastTab).not.toBeNull();
  expect(addBox).not.toBeNull();
  expect(addBox!.x).toBeGreaterThan(lastTab!.x + lastTab!.width);
});

test("the tablist rides on the card surface with the active tab highlighted (JUS-150)", async ({
  page,
}) => {
  await openFiles(page);
  await openExtraTab(page);

  await expect(page.locator(".fm-tabs > .fm-tabstrip")).toHaveCount(1);
  await expect(page.locator(".fm-tabstrip > .fm-tab-add")).toHaveCount(1);
  const group = await page.locator(".fm-tabs").evaluate((el) => {
    const style = getComputedStyle(el as HTMLElement);
    return {
      background: style.backgroundColor,
      borderTop: style.borderTopWidth,
      borderBottom: style.borderBottomWidth,
      borderLeft: style.borderLeftWidth,
      borderRight: style.borderRightWidth,
      radius: style.borderTopLeftRadius,
      padding: style.paddingTop,
    };
  });
  expect(group.background).toBe("rgba(0, 0, 0, 0)");
  for (const edge of ["borderTop", "borderBottom", "borderLeft", "borderRight"] as const) {
    expect(Number.parseFloat(group[edge]), `tablist ${edge}`).toBe(0);
  }
  expect(Number.parseFloat(group.radius)).toBe(0);
  expect(Number.parseFloat(group.padding)).toBe(0);

  const indicator = await page.evaluate(() => {
    const active = document.querySelector(".fm-tab.is-active") as HTMLElement;
    const inactive = document.querySelector(".fm-tab:not(.is-active)") as HTMLElement;
    const strip = document.querySelector(".fm-tabstrip") as HTMLElement;
    return {
      active: getComputedStyle(active).backgroundColor,
      inactive: getComputedStyle(inactive).backgroundColor,
      strip: getComputedStyle(strip).backgroundColor,
      stripBorder: getComputedStyle(strip).borderTopWidth,
    };
  });
  expect(indicator.active, "active tab keeps its own indicator fill").not.toBe(
    "rgba(0, 0, 0, 0)",
  );
  expect(indicator.inactive, "inactive tab has no fill").toBe("rgba(0, 0, 0, 0)");
  expect(indicator.strip, "strip has no fill").toBe("rgba(0, 0, 0, 0)");
  expect(Number.parseFloat(indicator.stripBorder), "strip has no border").toBe(0);
});

test("a focused tab draws its whole ring inside the scrolling strip (JUS-152)", async ({
  page,
}) => {
  await openFiles(page);

  await activeTab(page).focus();
  await page.keyboard.press("ArrowLeft");
  await page.keyboard.press("ArrowRight");
  await expect(activeTab(page)).toBeFocused();

  const ring = await page.locator(".fm-tabstrip").evaluate((strip) => {
    const tab = strip.querySelector<HTMLElement>(
      '[role="tab"][aria-selected="true"]',
    );
    if (tab === null) return null;
    const style = getComputedStyle(tab);
    const width = Number.parseFloat(style.outlineWidth);
    const offset = Number.parseFloat(style.outlineOffset);
    const box = tab.getBoundingClientRect();
    const clip = strip.getBoundingClientRect();
    return {
      outline: style.outlineStyle,
      width,
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

test("a cut in one tab pastes into another tab (shared clipboard)", async ({
  page,
}) => {
  await openFiles(page);
  const moves: Array<Record<string, unknown>> = [];
  await page.route(`**/api/v1/servers/${server1}/files/move`, async (route) => {
    moves.push(route.request().postDataJSON() as Record<string, unknown>);
    await route.fulfill({ json: { entry: file("a.txt") } });
  });

  await rowFor(page, "alpha").locator(".fm-label").dblclick();
  await expect(page.locator(".fm-label b").first()).toHaveText("a.txt");
  await rowFor(page, "a.txt").click({ button: "right" });
  const menu = page.getByRole("menu", { name: "More actions for a.txt" });
  await expect(menu).toBeVisible();
  await menu.getByRole("menuitem", { name: "Cut" }).click();

  await openExtraTab(page);
  await page
    .locator(".fm-toolbar")
    .getByRole("button", { name: "File operations", exact: true })
    .click();
  const paste = page
    .getByRole("menu", { name: "File operations" })
    .getByRole("menuitem", { name: /^Paste \(1\)/ });
  await expect(paste).toBeVisible();
  await paste.click();

  await expect.poll(() => moves.length).toBe(1);
  expect(moves[0]).toMatchObject({
    root: "volumes",
    source: "alpha/a.txt",
    destination: "a.txt",
    overwrite: false,
  });
});

test("the tablist is keyboard navigable with arrow keys", async ({ page }) => {
  await openFiles(page);
  await openExtraTab(page);
  await expect(activeTab(page)).toHaveText("volumes");

  await activeTab(page).focus();
  await page.keyboard.press("ArrowLeft");
  await expect(tabs(page).first()).toHaveAttribute("aria-selected", "true");
  await expect(tabs(page).first()).toBeFocused();

  await page.keyboard.press("ArrowRight");
  await expect(tabs(page).nth(1)).toHaveAttribute("aria-selected", "true");
  await expect(tabs(page).nth(1)).toBeFocused();
});

test("the tablist exposes tabs with a panel and moves focus on close", async ({
  page,
}) => {
  await openFiles(page);
  await openExtraTab(page);

  const childRoles = await page.evaluate(() =>
    [...document.querySelectorAll(".fm-tablist > *")].map((element) =>
      element.getAttribute("role"),
    ),
  );
  expect(childRoles.length).toBeGreaterThan(0);
  expect(childRoles.every((role) => role === "tab")).toBe(true);

  const panel = page.locator("#fm-tabpanel");
  await expect(panel).toHaveAttribute("role", "tabpanel");
  const activeId = await activeTab(page).getAttribute("id");
  await expect(activeTab(page)).toHaveAttribute("aria-controls", "fm-tabpanel");
  await expect(panel).toHaveAttribute("aria-labelledby", activeId!);

  await activeTab(page).locator(".fm-tab-close").click();
  await expect(tabs(page)).toHaveCount(1);
  await expect(activeTab(page)).toBeFocused();

  await activeTab(page).focus();
  await page.keyboard.press("Delete");
  await expect(tabs(page)).toHaveCount(1);
});

test("tab titles carry the full path and the strip scrolls at narrow widths", async ({
  page,
}) => {
  await openFiles(page);

  await rowFor(page, "alpha").locator(".fm-label").dblclick();
  await expect(page.locator(".fm-label b").first()).toHaveText("a.txt");
  await expect(activeTab(page)).toHaveAttribute("aria-label", "volumes/alpha");
  await expect(activeTab(page)).not.toHaveAttribute("title", /.+/);

  for (let index = 0; index < 5; index += 1) {
    await openExtraTab(page);
  }
  await page.setViewportSize({ width: 420, height: 844 });

  const strip = await page.evaluate(() => {
    const element = document.querySelector(".fm-tabstrip") as HTMLElement | null;
    const doc = document.scrollingElement as HTMLElement;
    return element === null
      ? null
      : {
          overflowX: getComputedStyle(element).overflowX,
          scroll: element.scrollWidth,
          client: element.clientWidth,
          pageOverflow: doc.scrollWidth - window.innerWidth,
        };
  });
  expect(strip).not.toBeNull();
  expect(strip!.overflowX).toBe("auto");
  expect(strip!.scroll).toBeGreaterThan(strip!.client);
  expect(strip!.pageOverflow).toBeLessThanOrEqual(1);

  const addBox = await page.locator(".fm-tab-add").boundingBox();
  const stripBox = await page.locator(".fm-tabstrip").boundingBox();
  expect(addBox).not.toBeNull();
  expect(stripBox).not.toBeNull();
  expect(
    Math.abs(addBox!.x + addBox!.width - (stripBox!.x + stripBox!.width)),
    "+ stuck to the strip's visible right edge",
  ).toBeLessThanOrEqual(1);
  expect(addBox!.x + addBox!.width).toBeLessThanOrEqual(420 + 1);
  await page.locator(".fm-tabstrip").evaluate((element) => {
    element.scrollLeft += 120;
  });
  const addScrolled = await page.locator(".fm-tab-add").boundingBox();
  expect(addScrolled).not.toBeNull();
  expect(
    Math.abs(
      addScrolled!.x + addScrolled!.width - (stripBox!.x + stripBox!.width),
    ),
    "+ stays pinned while the strip scrolls",
  ).toBeLessThanOrEqual(1);
});
