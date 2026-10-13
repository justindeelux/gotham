import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, getComputedStyle:readonly, HTMLElement:readonly, URL, window:readonly */
// JUS-123 browser tabs proof: the committed webdist is served statically with
// the control-plane file API mocked in real Chromium. Measured claims:
//   - two tabs keep independent paths and the active tab is a proper tablist,
//   - the clipboard is shared, so a cut in one tab pastes in another,
//   - the tablist is keyboard navigable (roles, arrow keys),
//   - tab titles carry the full path and the strip scrolls at narrow widths.

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

/** file builds one listing row. */
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

/** folder builds one directory row. */
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

/** listingFor returns a distinct listing per directory so paths are provable. */
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

/** openFiles seeds a session, mocks the file API and lands on the browser. */
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

/** rowFor returns the browser row whose name matches. */
function rowFor(page: Page, name: string) {
  return page.locator(".fm-table .fm-row", {
    has: page.locator(`b:text-is("${name}")`),
  });
}

/** tabs returns every tab button in the tablist. */
function tabs(page: Page) {
  return page.locator('.fm-tabstrip [role="tab"]');
}

/** activeTab returns the tab marked aria-selected. */
function activeTab(page: Page) {
  return page.locator('.fm-tabstrip [role="tab"][aria-selected="true"]');
}

/** openExtraTab clicks + and waits for the new tab to appear. */
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

  // Descend into alpha in the first tab.
  await rowFor(page, "alpha").locator(".fm-label").click();
  await expect(page.locator(".fm-label b").first()).toHaveText("a.txt");

  // Open a second tab: it starts at the root and lists the folders again.
  await openExtraTab(page);
  await expect(tabs(page)).toHaveCount(2);
  await expect(activeTab(page)).toHaveText("volumes");
  await expect(page.locator(".fm-label b").first()).toHaveText("alpha");

  // Back to the first tab: it is still inside alpha.
  await tabs(page).first().click();
  await expect(activeTab(page)).toHaveText("alpha");
  await expect(page.locator(".fm-label b").first()).toHaveText("a.txt");

  // And the second tab is still at the root.
  await tabs(page).nth(1).click();
  await expect(activeTab(page)).toHaveText("volumes");
  await expect(page.locator(".fm-label b").first()).toHaveText("alpha");

  // JUS-126: + sits immediately after the last tab, not at the far right.
  const lastTab = await tabs(page).nth(1).boundingBox();
  const addBox = await page.locator(".fm-tab-add").boundingBox();
  expect(lastTab).not.toBeNull();
  expect(addBox).not.toBeNull();
  expect(addBox!.x).toBeGreaterThan(lastTab!.x + lastTab!.width);
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

  // Tab A: enter alpha and cut a.txt from its row menu.
  await rowFor(page, "alpha").locator(".fm-label").click();
  await expect(page.locator(".fm-label b").first()).toHaveText("a.txt");
  await rowFor(page, "a.txt").click({ button: "right" });
  const menu = page.getByRole("menu", { name: "More actions for a.txt" });
  await expect(menu).toBeVisible();
  await menu.getByRole("menuitem", { name: "Cut" }).click();

  // Tab B: a new tab at the root, still holding the clipboard.
  await openExtraTab(page);
  const paste = page.locator(".fm-toolbar").getByRole("button", { name: "Paste" });
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

  // Focus the active tab and move left to the first tab.
  await activeTab(page).focus();
  await page.keyboard.press("ArrowLeft");
  await expect(tabs(page).first()).toHaveAttribute("aria-selected", "true");
  await expect(tabs(page).first()).toBeFocused();

  // ArrowRight wraps back to the second tab.
  await page.keyboard.press("ArrowRight");
  await expect(tabs(page).nth(1)).toHaveAttribute("aria-selected", "true");
  await expect(tabs(page).nth(1)).toBeFocused();
});

test("the tablist exposes tabs with a panel and moves focus on close", async ({
  page,
}) => {
  await openFiles(page);
  await openExtraTab(page);

  // Every owned child of the tablist is a tab (no stray close buttons), and the
  // active tab controls the single panel that labels itself by that tab.
  const childRoles = await page.evaluate(() =>
    [...document.querySelectorAll(".fm-tabstrip > *")].map((element) =>
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

  // Closing the active tab via the × moves focus to the neighbour that takes over.
  await activeTab(page).locator(".fm-tab-close").click();
  await expect(tabs(page)).toHaveCount(1);
  await expect(activeTab(page)).toBeFocused();

  // Delete on the sole tab is a no-op (the × is hidden there too).
  await activeTab(page).focus();
  await page.keyboard.press("Delete");
  await expect(tabs(page)).toHaveCount(1);
});

test("tab titles carry the full path and the strip scrolls at narrow widths", async ({
  page,
}) => {
  await openFiles(page);

  // A tab's tooltip is the full root-relative location.
  await rowFor(page, "alpha").locator(".fm-label").click();
  await expect(page.locator(".fm-label b").first()).toHaveText("a.txt");
  await expect(activeTab(page)).toHaveAttribute("title", "volumes/alpha");

  // Open enough tabs that the strip must scroll instead of wrapping the page.
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
  // The strip scrolls itself; the page never gains a horizontal scrollbar.
  expect(strip!.pageOverflow).toBeLessThanOrEqual(1);

  // JUS-126: + follows the last tab and stays on-screen when the strip
  // overflows, instead of being pushed out of view.
  const addBox = await page.locator(".fm-tab-add").boundingBox();
  const stripBox = await page.locator(".fm-tabstrip").boundingBox();
  expect(addBox).not.toBeNull();
  expect(stripBox).not.toBeNull();
  expect(addBox!.x).toBeGreaterThanOrEqual(stripBox!.x + stripBox!.width - 1);
  expect(addBox!.x + addBox!.width).toBeLessThanOrEqual(420 + 1);
});
