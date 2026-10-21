import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Box, Locator, Page } from "@playwright/test";

/* global document:readonly, getComputedStyle:readonly, HTMLElement:readonly, SVGSVGElement:readonly, navigator:readonly, URL, window:readonly */
// URL/window run inside the page and server callbacks (browser/Node URL
// globals); the repo lint envs do not cover tests/, so they are declared.

// Real-browser proof for the P18 step 8 editor conflict modal: the committed
// webdist is served statically with the control-plane file API mocked, a
// session is seeded in localStorage, and real Chromium measures the real Naive
// UI DOM. The file is opened in the editor (loading the CodeMirror chunk), a
// change is typed, Ctrl/Cmd+S meets a mocked 409, and the conflict modal is
// measured. Same harness as environment-layout.spec.ts.

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

const serverId = "33333333-3333-4333-8333-333333333333";

const serverRow = {
  id: serverId,
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

const entry = {
  name: "app.yaml",
  kind: "file",
  size: 24,
  mode: 0o100644,
  modified_at: "2026-10-12T09:02:00Z",
  etag: '"v1"',
};

const logoEntry = {
  name: "logo.png",
  kind: "file",
  size: 25_190,
  mode: 0o100644,
  modified_at: "2026-10-12T09:01:00Z",
  etag: '"p1"',
};

const notesEntry = {
  name: "notes.md",
  kind: "file",
  size: 18,
  mode: 0o100644,
  modified_at: "2026-10-12T09:03:00Z",
  etag: '"v2"',
};

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

/** mockApi seeds the session and answers the calls the files page makes. */
async function mockApi(
  page: Page,
  options: { historyVersions?: number } = {},
): Promise<void> {
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
    await route.fulfill({ json: { servers: [serverRow] } });
  });
  await page.route(`**/api/v1/servers/${serverId}`, async (route) => {
    await route.fulfill({ json: serverRow });
  });
  await page.route(`**/api/v1/servers/${serverId}/files/roots`, async (route) => {
    await route.fulfill({ json: { roots: [root] } });
  });
  await page.route(`**/api/v1/servers/${serverId}/files/list*`, async (route) => {
    await route.fulfill({
      json: { entries: [entry, notesEntry, logoEntry], truncated: false, cursor: "" },
    });
  });
  await page.route(`**/api/v1/servers/${serverId}/files/content*`, async (route) => {
    if (route.request().method() === "PUT") {
      // A concurrent write: the editor's If-Match no longer matches.
      await route.fulfill({ status: 409, json: { message: "conflict" } });
      return;
    }
    const path = new URL(route.request().url()).searchParams.get("path") ?? "";
    if (path.endsWith("logo.png")) {
      // A binary read: the editor renders the download-only state.
      await route.fulfill({ status: 415, json: { message: "not editable" } });
      return;
    }
    await route.fulfill({
      json: {
        content: "services:\n  web:\n",
        etag: '"v1"',
        sha256: "abc",
        size: 24,
        mode: 0o100644,
      },
    });
  });
  if (options.historyVersions) {
    const versions = Array.from({ length: options.historyVersions }, (_, index) => ({
      id: `v${options.historyVersions! - index}`,
      created_at: `2026-10-1${index}T09:00:00Z`,
      size: 12 * (index + 1),
      etag: `"v${options.historyVersions! - index}"`,
    }));
    await page.route(`**/api/v1/servers/${serverId}/files/history*`, async (route) => {
      await route.fulfill({ json: { versions } });
    });
  }
}

/** openConflict opens app.yaml, edits it, and triggers the 409 conflict modal. */
async function openConflict(page: Page): Promise<Locator> {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  const label = page.locator(".fm-label", { hasText: "app.yaml" }).first();
  await expect(label).toBeVisible();
  await label.dblclick();

  // The CodeMirror chunk loads lazily; wait for the real editor surface.
  const editor = page.locator(".cm-content");
  await expect(editor).toBeVisible({ timeout: 15_000 });
  await editor.click();
  await page.keyboard.type("# changed");

  // The dirty dot appears on the tab.
  await expect(page.locator(".ed-dot")).toHaveCount(1);

  await page.keyboard.press("ControlOrMeta+s");

  const dialog = page.locator(".n-modal.n-card");
  await expect(dialog).toBeVisible({ timeout: 10_000 });
  await expect
    .poll(async () => (await dialog.boundingBox())?.width ?? 0, { timeout: 5000 })
    .toBeGreaterThanOrEqual(400);
  return dialog;
}

/**
 * minimizeEditor collapses the floating editor window into the rail icon so
 * the file browser underneath becomes clickable (the window overlays it).
 */
async function minimizeEditor(page: Page): Promise<void> {
  await page.locator(".ed-window-min").click();
  await expect(page.locator(".rail-btn--editor")).toBeVisible();
  await expect(page.locator(".ed-window")).toBeHidden();
}

/**
 * badgeGeometry measures the rail badge of the code-editor button: the box it
 * paints and the corner radius the browser resolved. The badge must be a
 * perfect circle (JUS-241), for a one-digit count and for "9+".
 */
async function badgeGeometry(
  page: Page,
  label?: string,
): Promise<{ width: number; height: number; radius: number }> {
  return page.evaluate((text) => {
    const badge = document.querySelector(".rail-btn--editor .rail-badge");
    if (!(badge instanceof HTMLElement)) {
      throw new Error("the editor rail badge is missing");
    }
    if (text !== undefined) {
      badge.textContent = text;
    }
    const box = badge.getBoundingClientRect();
    const radius = Number.parseFloat(getComputedStyle(badge).borderTopLeftRadius);
    return { width: box.width, height: box.height, radius };
  }, label);
}

/**
 * openViaMenu opens one file's editor tab through its row menu. A binary file
 * (JUS-130) has no double-click action, so its binary/download-only tab is only
 * reachable through "Edit in editor".
 */
async function openViaMenu(page: Page, name: string): Promise<void> {
  await page.getByRole("button", { name: `More actions for ${name}` }).click();
  await page.getByRole("menuitem", { name: "Edit in editor" }).click();
}

test("the minimized editor parks on the rail with a tab-count badge", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  await minimizeEditor(page);

  // A second file while parked reopens the window; parking again counts 2.
  await page.locator(".fm-label", { hasText: "notes.md" }).first().dblclick();
  await expect(page.locator(".ed-window")).toBeVisible();
  await minimizeEditor(page);

  // The rail icon carries a badge with the open-tab count.
  const railButton = page.locator(".rail-btn--editor");
  await expect(railButton).toHaveAttribute("aria-label", /2 open/);
  await expect(railButton.locator(".rail-badge")).toHaveText("2");

  // The badge rides the button's top-right corner and never covers the
  // icon's centre, so the icon stays fully visible at every count.
  const geometry = await page.evaluate(() => {
    const button = document.querySelector(".rail-btn--editor");
    const badge = button?.querySelector(".rail-badge");
    const icon = button?.querySelector("svg");
    if (
      !(button instanceof HTMLElement) ||
      !(badge instanceof HTMLElement) ||
      !(icon instanceof SVGSVGElement)
    ) {
      return null;
    }
    const buttonBox = button.getBoundingClientRect();
    const badgeBox = badge.getBoundingClientRect();
    const iconBox = icon.getBoundingClientRect();
    const iconCenterX = iconBox.x + iconBox.width / 2;
    const iconCenterY = iconBox.y + iconBox.height / 2;
    return {
      badgeCoversIconCentre:
        iconCenterX >= badgeBox.x &&
        iconCenterX <= badgeBox.x + badgeBox.width &&
        iconCenterY >= badgeBox.y &&
        iconCenterY <= badgeBox.y + badgeBox.height,
      badgeInTopRight:
        badgeBox.x + badgeBox.width > buttonBox.x + buttonBox.width / 2 &&
        badgeBox.y < buttonBox.y + buttonBox.height / 2,
    };
  });
  expect(geometry).not.toBeNull();
  expect(geometry!.badgeCoversIconCentre).toBe(false);
  expect(geometry!.badgeInTopRight).toBe(true);

  // The badge is a perfect circle, not a pill (JUS-241): equal sides and a
  // fully rounded corner. A two-digit count ("9+") must keep that circle.
  const single = await badgeGeometry(page);
  expect(Math.abs(single.width - single.height)).toBeLessThanOrEqual(0.5);
  expect(single.radius).toBeGreaterThanOrEqual(single.width / 2 - 0.5);
  const double = await badgeGeometry(page, "9+");
  expect(Math.abs(double.width - double.height)).toBeLessThanOrEqual(0.5);
  expect(double.radius).toBeGreaterThanOrEqual(double.width / 2 - 0.5);
  // The badge keeps its size whatever the label says.
  expect(Math.abs(double.width - single.width)).toBeLessThanOrEqual(0.5);
  await badgeGeometry(page, "2");

  // Placement: above Overview and Alert, separated by dividers from the
  // server list and from those two buttons.
  const footItems = await page.locator(".rail-foot .rail-item").all();
  const labels = await Promise.all(
    footItems.map((item) => item.innerText()),
  );
  const editorIndex = labels.findIndex((text) => text.includes("2"));
  expect(editorIndex).toBeGreaterThanOrEqual(0);
  const separators = await page.locator(".rail-foot .rail-sep").count();
  expect(separators).toBeGreaterThanOrEqual(2);

  // A click restores the window with the buffer intact.
  await railButton.click();
  await expect(page.locator(".ed-window")).toBeVisible();
  await expect(page.locator(".ed-host:visible .cm-content")).toBeVisible();
});

test("the editor window resizes from its corner within the viewport", async ({
  page,
}) => {
  const viewport = { width: 1440, height: 900 };
  await page.setViewportSize(viewport);
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });

  const window_ = page.locator(".ed-window");
  const before = await window_.boundingBox();
  expect(before).not.toBeNull();

  // Drag the corner grip up and to the left: the window shrinks. The drag
  // must clear the viewport margin cap, so it moves well past it.
  const grip = page.locator(".ed-resize--se");
  const gripBox = await grip.boundingBox();
  expect(gripBox).not.toBeNull();
  await page.mouse.move(
    gripBox!.x + gripBox!.width / 2,
    gripBox!.y + gripBox!.height / 2,
  );
  await page.mouse.down();
  await page.mouse.move(gripBox!.x - 160, gripBox!.y - 320, { steps: 10 });
  await page.mouse.up();
  const resized = await window_.boundingBox();
  expect(resized).not.toBeNull();
  expect(resized!.width).toBeLessThan(before!.width);
  expect(resized!.height).toBeLessThan(before!.height);
  expect(resized!.x + resized!.width).toBeLessThanOrEqual(viewport.width + 1);
  expect(resized!.y + resized!.height).toBeLessThanOrEqual(viewport.height + 1);
});

/**
 * RESIZE_CASES lists the eight grips of the window frame with a drag that
 * shrinks the window from that side, and the edges that must move with it.
 * The edge opposite the grip stays anchored.
 */
const RESIZE_CASES = [
  { direction: "n", dx: 0, dy: 40, left: 0, top: 40, right: 0, bottom: 0 },
  { direction: "s", dx: 0, dy: -40, left: 0, top: 0, right: 0, bottom: -40 },
  { direction: "e", dx: -40, dy: 0, left: 0, top: 0, right: -40, bottom: 0 },
  { direction: "w", dx: 40, dy: 0, left: 40, top: 0, right: 0, bottom: 0 },
  { direction: "nw", dx: 40, dy: 40, left: 40, top: 40, right: 0, bottom: 0 },
  { direction: "ne", dx: -40, dy: 40, left: 0, top: 40, right: -40, bottom: 0 },
  { direction: "se", dx: -40, dy: -40, left: 0, top: 0, right: -40, bottom: -40 },
  { direction: "sw", dx: 40, dy: -40, left: 40, top: 0, right: 0, bottom: -40 },
];

/**
 * dragResizeGrip drags one resize grip by the given delta and returns the
 * window box before and after the drag.
 */
async function dragResizeGrip(
  page: Page,
  direction: string,
  dx: number,
  dy: number,
): Promise<{ before: Box; after: Box }> {
  const before = await page.locator(".ed-window").boundingBox();
  expect(before).not.toBeNull();
  const grip = page.locator(`.ed-resize--${direction}`);
  await expect(grip).toBeVisible();
  const gripBox = await grip.boundingBox();
  expect(gripBox).not.toBeNull();
  const startX = gripBox!.x + gripBox!.width / 2;
  const startY = gripBox!.y + gripBox!.height / 2;
  await page.mouse.move(startX, startY);
  await page.mouse.down();
  await page.mouse.move(startX + dx, startY + dy, { steps: 8 });
  await page.mouse.up();
  const after = await page.locator(".ed-window").boundingBox();
  expect(after).not.toBeNull();
  return { before: before!, after: after! };
}

test("the editor window resizes from every edge and corner", async ({ page }) => {
  for (const viewport of [
    { width: 1280, height: 800 },
    { width: 1366, height: 800 },
  ]) {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await page.goto(`${baseURL}/servers/${serverId}/files`);
    // Every pass starts from the fitted default, not from the geometry the
    // previous pass left behind.
    await page.evaluate(() => window.localStorage.clear());
    await page.reload();
    await reopenEditor(page);

    for (const resizeCase of RESIZE_CASES) {
      const { before, after } = await dragResizeGrip(
        page,
        resizeCase.direction,
        resizeCase.dx,
        resizeCase.dy,
      );
      const near = (value: number, expected: number): boolean =>
        Math.abs(value - expected) <= 1.5;
      // The grip moves its own edges; the opposite ones stay anchored.
      expect(
        near(after.x, before.x + resizeCase.left),
        `${resizeCase.direction} left edge`,
      ).toBe(true);
      expect(
        near(after.y, before.y + resizeCase.top),
        `${resizeCase.direction} top edge`,
      ).toBe(true);
      expect(
        near(
          after.x + after.width,
          before.x + before.width + resizeCase.right,
        ),
        `${resizeCase.direction} right edge`,
      ).toBe(true);
      expect(
        near(
          after.y + after.height,
          before.y + before.height + resizeCase.bottom,
        ),
        `${resizeCase.direction} bottom edge`,
      ).toBe(true);
      // The window stays fully inside the viewport.
      expect(after.x).toBeGreaterThanOrEqual(-1);
      expect(after.y).toBeGreaterThanOrEqual(-1);
      expect(after.x + after.width).toBeLessThanOrEqual(viewport.width + 1);
      expect(after.y + after.height).toBeLessThanOrEqual(viewport.height + 1);
    }
  }
});

/**
 * RESIZE_GRIP_CURSORS maps every grip to the resize cursor it must keep. JUS-269
 * removed the grip marks; the invisible hit strip is now the only affordance,
 * so the cursor carries the resize signal.
 */
const RESIZE_GRIP_CURSORS: Record<string, string> = {
  n: "ns-resize",
  s: "ns-resize",
  e: "ew-resize",
  w: "ew-resize",
  nw: "nwse-resize",
  ne: "nesw-resize",
  sw: "nesw-resize",
  se: "nwse-resize",
};

test("the editor resize grips paint nothing and keep their hit areas and cursors", async ({
  page,
}) => {
  const viewport = { width: 1280, height: 800 };
  await page.setViewportSize(viewport);
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);
  await page.evaluate(() => window.localStorage.clear());
  await page.reload();
  await reopenEditor(page);

  for (const [direction, cursor] of Object.entries(RESIZE_GRIP_CURSORS)) {
    const grip = page.locator(`.ed-resize--${direction}`);
    // The invisible strip still occupies its frame area, so the drag target
    // survives even though the mark is gone.
    await expect(grip).toBeVisible();
    const gripBox = await grip.boundingBox();
    expect(gripBox, `${direction} grip box`).not.toBeNull();
    expect(gripBox!.width, `${direction} grip width`).toBeGreaterThan(0);
    expect(gripBox!.height, `${direction} grip height`).toBeGreaterThan(0);

    // Nothing is painted: no pseudo-element mark, no background, no border.
    // The cursor is the one visible affordance left.
    const paint = await grip.evaluate((element) => {
      const mark = getComputedStyle(element, "::after");
      const style = getComputedStyle(element);
      return {
        markContent: mark.content,
        markWidth: mark.width,
        background: style.backgroundColor,
        backgroundImage: style.backgroundImage,
        borderWidth: style.borderTopWidth,
        cursor: style.cursor,
      };
    });
    expect(paint.markContent, `${direction} mark`).toBe("none");
    expect(paint.markWidth, `${direction} mark width`).toBe("auto");
    expect(paint.background, `${direction} background`).toBe("rgba(0, 0, 0, 0)");
    expect(paint.backgroundImage, `${direction} background image`).toBe("none");
    expect(paint.borderWidth, `${direction} border`).toBe("0px");
    expect(paint.cursor, `${direction} cursor`).toBe(cursor);
  }

  // The hit area still resizes: dragging the east strip shrinks the window.
  const { before, after } = await dragResizeGrip(page, "e", -40, 0);
  expect(Math.abs(after.width - (before.width - 40))).toBeLessThanOrEqual(1.5);
  expect(Math.abs(after.height - before.height)).toBeLessThanOrEqual(1.5);
});

test("the editor window keeps its minimum size and stays on screen", async ({
  page,
}) => {
  const viewport = { width: 1280, height: 800 };
  await page.setViewportSize(viewport);
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);
  await page.evaluate(() => window.localStorage.clear());
  await page.reload();
  await reopenEditor(page);

  // Dragging the west grip far to the right stops at the minimum width: the
  // window never shrinks below 480px, and its right edge stays put.
  const shrunk = await dragResizeGrip(page, "w", 4000, 0);
  expect(shrunk.after.width).toBeGreaterThanOrEqual(480);
  expect(shrunk.after.width).toBeLessThanOrEqual(481);
  expect(shrunk.after.x + shrunk.after.width).toBeLessThanOrEqual(
    viewport.width + 1,
  );

  // The same for the height: the north grip stops at the minimum height.
  const shortened = await dragResizeGrip(page, "n", 0, 4000);
  expect(shortened.after.height).toBeGreaterThanOrEqual(320);
  expect(shortened.after.height).toBeLessThanOrEqual(321);
  expect(shortened.after.y + shortened.after.height).toBeLessThanOrEqual(
    viewport.height + 1,
  );

  // Growing the east grip far to the right stops at the viewport edge: the
  // window grows, but it never leaves the screen.
  const grown = await dragResizeGrip(page, "e", 4000, 0);
  expect(grown.after.x).toBeGreaterThanOrEqual(-1);
  expect(grown.after.x + grown.after.width).toBeLessThanOrEqual(
    viewport.width + 1,
  );
  expect(grown.after.y + grown.after.height).toBeLessThanOrEqual(
    viewport.height + 1,
  );
});

test("the editor window stays inside a 390px phone viewport with margins", async ({
  page,
}) => {
  const viewport = { width: 390, height: 844 };
  await page.setViewportSize(viewport);
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });

  // The default size yields to the narrow viewport instead of overflowing it.
  const box = await page.locator(".ed-window").boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width + 1);
  expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height + 1);
  // ... and dragging the corner cannot push it back out.
  const grip = page.locator(".ed-resize--se");
  const gripBox = await grip.boundingBox();
  expect(gripBox).not.toBeNull();
  await page.mouse.move(
    gripBox!.x + gripBox!.width / 2,
    gripBox!.y + gripBox!.height / 2,
  );
  await page.mouse.down();
  await page.mouse.move(viewport.width + 400, viewport.height + 400, { steps: 10 });
  await page.mouse.up();
  const grown = await page.locator(".ed-window").boundingBox();
  expect(grown).not.toBeNull();
  expect(grown!.x + grown!.width).toBeLessThanOrEqual(viewport.width + 1);
  expect(grown!.y + grown!.height).toBeLessThanOrEqual(viewport.height + 1);

  // Below the tablet breakpoint the rail lives in the topbar drawer, and the
  // parked icon must still restore the window from there.
  await page.locator(".ed-window-min").click();
  await expect(page.locator(".ed-window")).toBeHidden();
  await page.locator(".rail-toggle").click();
  await expect(page.locator(".rail-btn--editor")).toBeVisible();
  await page.locator(".rail-btn--editor").click();
  await expect(page.locator(".ed-window")).toBeVisible();
});

test("editor save conflict opens a bounded modal with pinned header and three choices", async ({
  page,
}) => {
  const viewport = { width: 1440, height: 900 };
  const dialog = await openConflict(page);
  const dialogBox = await dialog.boundingBox();
  expect(dialogBox, "dialog has a box").not.toBeNull();
  expect(dialogBox!.width, "dialog is ~480px wide").toBeLessThanOrEqual(560);
  expect(dialogBox!.width, "dialog is ~480px wide").toBeGreaterThanOrEqual(400);
  // The modal never exceeds the viewport.
  expect(dialogBox!.y).toBeGreaterThanOrEqual(0);
  expect(dialogBox!.y + dialogBox!.height).toBeLessThanOrEqual(viewport.height);

  // Header stays fixed while the body scrolls.
  const header = dialog.locator(".n-card-header");
  const body = dialog.locator(".n-card-content");
  // Wait out the modal enter transition so the header position is settled.
  await expect
    .poll(async () => {
      const first = (await header.boundingBox())!.y;
      await page.waitForTimeout(150);
      return first - (await header.boundingBox())!.y;
    })
    .toBe(0);
  const before = await header.boundingBox();
  await body.evaluate((element) => {
    element.scrollTop = element.scrollHeight;
  });
  const after = await header.boundingBox();
  expect(after!.y).toBeCloseTo(before!.y, 0);

  // The three conflict choices, left to right: Reload, Save as copy, Overwrite.
  const reload = dialog.getByRole("button", { name: "Reload", exact: true });
  const saveAs = dialog.getByRole("button", { name: "Save as copy", exact: true });
  const overwrite = dialog.getByRole("button", { name: "Overwrite", exact: true });
  await expect(reload).toBeVisible();
  await expect(saveAs).toBeVisible();
  await expect(overwrite).toBeVisible();
  const reloadBox = await reload.boundingBox();
  const saveAsBox = await saveAs.boundingBox();
  const overwriteBox = await overwrite.boundingBox();
  expect(reloadBox!.x + reloadBox!.width).toBeLessThanOrEqual(saveAsBox!.x);
  expect(saveAsBox!.x + saveAsBox!.width).toBeLessThanOrEqual(overwriteBox!.x);
  // Every action stays inside the viewport (the acceptance proof).
  for (const box of [reloadBox, saveAsBox, overwriteBox]) {
    expect(box!.y + box!.height).toBeLessThanOrEqual(viewport.height);
  }
});

test("the conflict modal stays inside a short viewport", async ({ page }) => {
  const viewport = { width: 1280, height: 640 };
  await page.setViewportSize(viewport);
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  const label = page.locator(".fm-label", { hasText: "app.yaml" }).first();
  await expect(label).toBeVisible();
  await label.dblclick();
  const editor = page.locator(".cm-content");
  await expect(editor).toBeVisible({ timeout: 15_000 });
  await editor.click();
  await page.keyboard.type("# changed");
  await page.keyboard.press("ControlOrMeta+s");

  const dialog = page.locator(".n-modal.n-card");
  await expect(dialog).toBeVisible({ timeout: 10_000 });
  const dialogBox = await dialog.boundingBox();
  expect(dialogBox, "dialog has a box").not.toBeNull();
  expect(dialogBox!.y).toBeGreaterThanOrEqual(0);
  expect(dialogBox!.y + dialogBox!.height).toBeLessThanOrEqual(viewport.height);
  await expect(dialog.getByRole("button", { name: "Overwrite" })).toBeVisible();
});

test("switching to a binary tab and back keeps the editor mounted", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  // Open the ready tab first.
  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });

  // Open a binary tab (415) — this used to unmount every ready host. The
  // floating window overlays the browser, so minimize it first.
  await minimizeEditor(page);
  await openViaMenu(page, "logo.png");
  await expect(page.getByText("Binary file")).toBeVisible();

  // Back to the ready tab: the CodeMirror surface must still be there.
  await page.getByRole("tab", { name: /app\.yaml/ }).click();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 5_000 });
  await expect(page.getByText("Binary file")).toHaveCount(0);
});

test("Ctrl/Cmd+S is a no-op on a clean tab", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  const editor = page.locator(".cm-content");
  await expect(editor).toBeVisible({ timeout: 15_000 });
  await editor.click();
  // No edit: the tab is clean, so Ctrl+S must not fire the (mocked 409) PUT.
  await page.keyboard.press("ControlOrMeta+s");
  await page.waitForTimeout(500);
  await expect(page.locator(".ed-dot")).toHaveCount(0);
  await expect(page.locator(".n-modal.n-card")).toHaveCount(0);
});

test("the editor tablist exposes tabs, a panel and arrow-key navigation", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  await minimizeEditor(page);
  await openViaMenu(page, "logo.png");
  await expect(page.getByText("Binary file")).toBeVisible();

  // The browser also exposes a tablist (JUS-123), so scope to the editor.
  const tablist = page.getByRole("tablist", { name: "Editor" });
  await expect(tablist).toBeVisible();
  const tabs = tablist.getByRole("tab");
  await expect(tabs).toHaveCount(2);

  // The active tab owns a resolvable panel with role=tabpanel.
  const active = tablist.getByRole("tab", { selected: true });
  const panelId = await active.getAttribute("aria-controls");
  expect(panelId).toBeTruthy();
  await expect(page.locator(`#${panelId}`)).toHaveAttribute("role", "tabpanel");

  // Roving focus: ArrowLeft moves selection to the previous tab.
  await active.focus();
  await page.keyboard.press("ArrowLeft");
  await expect(tablist.getByRole("tab", { selected: true })).toHaveText(/app\.yaml/);
});

test("a ready tab renders no error state and keeps tab/panel ids unique", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  // A second ready tab: with the old unguarded v-else each one added a
  // duplicate "Could not open <name>" panel sharing the host's id.
  await minimizeEditor(page);
  await page.locator(".fm-label", { hasText: "notes.md" }).first().dblclick();
  await expect(page.locator(".ed-host:visible .cm-content")).toBeVisible({
    timeout: 15_000,
  });

  await expect(page.locator(".ed-state")).toHaveCount(0);
  await expect(page.getByText("Could not open")).toHaveCount(0);

  // The whole document (visible and v-show-hidden) must hold one panel id per
  // ready tab and no duplicate: the old v-else added a second `ed-panel-<id>`
  // for the active ready tab.
  const ids = await page.evaluate(() =>
    [...document.querySelectorAll("[id^='ed-panel-'], [id^='ed-tab-']")].map(
      (element) => element.id,
    ),
  );
  const panelIds = ids.filter((id) => id.startsWith("ed-panel-"));
  const tabIds = ids.filter((id) => id.startsWith("ed-tab-"));
  expect(tabIds.length).toBe(2);
  expect(panelIds.length).toBe(2);
  expect(new Set(ids).size).toBe(ids.length);
});

test("arrow keys keep switching between two ready tabs", async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  await minimizeEditor(page);
  await page.locator(".fm-label", { hasText: "notes.md" }).first().dblclick();
  await expect(page.locator(".ed-host:visible .cm-content")).toBeVisible({
    timeout: 15_000,
  });

  // Scope to the editor tablist: the browser tab bar (JUS-123) is a second
  // tablist on the page.
  const editorTabs = page.getByRole("tablist", { name: "Editor" });
  await expect(editorTabs.getByRole("tab")).toHaveCount(2);
  // Activate the first tab: the click focuses its button, but the session focus
  // must not be yanked into the editor, or the arrow keys below drive the
  // editor cursor instead of the tablist.
  await editorTabs.getByRole("tab", { name: /app\.yaml/ }).click();
  await expect(editorTabs.getByRole("tab", { selected: true })).toHaveText(/app\.yaml/);
  await page.keyboard.press("ArrowRight");
  await expect(editorTabs.getByRole("tab", { selected: true })).toHaveText(/notes\.md/);
  expect(
    await page.evaluate(() => document.activeElement?.getAttribute("role")),
  ).toBe("tab");
  await page.keyboard.press("ArrowLeft");
  await expect(editorTabs.getByRole("tab", { selected: true })).toHaveText(/app\.yaml/);

  // The documented tab-cycling binding works from inside the editor (a browser
  // never delivers Ctrl+Tab to the page, so the sheet advertises Mod+Alt+Page).
  await page.locator(".ed-host:visible .cm-content").click();
  await page.keyboard.press("ControlOrMeta+Alt+PageDown");
  await expect(editorTabs.getByRole("tab", { selected: true })).toHaveText(/notes\.md/);
  await page.keyboard.press("ControlOrMeta+Alt+PageUp");
  await expect(editorTabs.getByRole("tab", { selected: true })).toHaveText(/app\.yaml/);
});

test("the status bar stays on one line at the default window width", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });

  const foot = page.locator(".ed-foot");
  await expect(foot).toBeVisible();
  expect(await foot.evaluate((el) => getComputedStyle(el).flexWrap)).toBe("nowrap");

  const metrics = await foot.evaluate((el) => {
    const style = getComputedStyle(el);
    const lineHeight =
      Number.parseFloat(style.lineHeight) ||
      Number.parseFloat(style.fontSize) * 1.2;
    const padding =
      Number.parseFloat(style.paddingTop) + Number.parseFloat(style.paddingBottom);
    return {
      height: el.getBoundingClientRect().height,
      oneLine: lineHeight + padding,
      scrollWidth: el.scrollWidth,
      clientWidth: el.clientWidth,
    };
  });
  // One line only: the height is one line box plus its top padding, and no
  // item overflows horizontally.
  expect(metrics.height).toBeLessThanOrEqual(metrics.oneLine + 2);
  expect(metrics.scrollWidth).toBeLessThanOrEqual(metrics.clientWidth + 1);

  // The long part truncates; the full path stays available as the title.
  const path = page.locator(".ed-path");
  expect(await path.getAttribute("title")).toContain("app.yaml");
  expect(await path.evaluate((el) => getComputedStyle(el).textOverflow)).toBe(
    "ellipsis",
  );
  expect(await path.evaluate((el) => getComputedStyle(el).whiteSpace)).toBe(
    "nowrap",
  );
});

test("find, the replace binding and the toolbar open the custom find bar", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  const content = page.locator(".cm-content");
  await expect(content).toBeVisible({ timeout: 15_000 });
  await content.click();

  const mac = await page.evaluate(() => navigator.platform.includes("Mac"));
  const bar = page.locator(".ed-find");
  const query = bar.locator("input").first();

  // JUS-231: the toolbar opens find collapsed, focused on the query, and the
  // default CodeMirror panel must not appear.
  await page.getByRole("button", { name: "Find / replace" }).click();
  await expect(bar).toBeVisible();
  await expect(page.locator(".cm-panels")).toHaveCount(0);
  await expect(bar.locator("input")).toHaveCount(1);
  await expect(query).toBeFocused();

  // Find keeps the remembered collapsed state.
  await page.keyboard.press("Escape");
  await expect(bar).toBeHidden();
  await content.click();
  await page.keyboard.press(mac ? "Meta+f" : "Control+f");
  await expect(bar.locator("input")).toHaveCount(1);
  await expect(query).toBeFocused();

  // macOS uses Cmd+Option+F (Cmd+H hides the app); other platforms Ctrl+H.
  // The replace binding expands the row and focuses its field.
  await page.keyboard.press("Escape");
  await expect(bar).toBeHidden();
  await content.click();
  await page.keyboard.press(mac ? "Meta+Alt+f" : "Control+h");
  await expect(bar).toBeVisible();
  const replace = bar.locator("input").nth(1);
  await expect(replace).toBeVisible();
  await expect(replace).toBeFocused();

  // The expanded row is remembered for this tab; find does not force it open
  // or closed, it restores the tab's state and focuses the query.
  await page.keyboard.press("Escape");
  await content.click();
  await page.keyboard.press(mac ? "Meta+f" : "Control+f");
  await expect(bar.locator("input")).toHaveCount(2);
  await expect(query).toBeFocused();
});

test("the conflict modal is an alertdialog that confirms overwrite and reload", async ({
  page,
}) => {
  const dialog = await openConflict(page);
  await expect(dialog).toHaveAttribute("role", "alertdialog");

  // Reload discards unsaved edits, so it asks first.
  await dialog.getByRole("button", { name: "Reload", exact: true }).click();
  const reloadPop = page.locator(".n-popconfirm");
  await expect(reloadPop).toContainText("discards your unsaved edits");
  await reloadPop.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.locator(".n-popconfirm")).toBeHidden();

  // Overwrite is the danger action and asks first too.
  const overwrite = dialog.getByRole("button", { name: "Overwrite", exact: true });
  await expect(overwrite).toHaveClass(/n-button--error-type/);
  await overwrite.click();
  await expect(page.locator(".n-popconfirm")).toContainText("Overwrite replaces");
});

test("leaving the files route with a dirty tab keeps the editor open", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  const editor = page.locator(".cm-content");
  await expect(editor).toBeVisible({ timeout: 15_000 });
  await editor.click();
  await page.keyboard.type("# changed");
  await expect(page.locator(".ed-dot")).toHaveCount(1);

  // The window is app-level now: parking it and navigating away prompts
  // nothing, keeps the tab and its unsaved buffer, and the rail badge still
  // counts it. (The window overlays the sidebar at this size, so park first.)
  await page.locator(".ed-window-min").click();
  await expect(page.locator(".rail-btn--editor")).toBeVisible();

  await page.locator("a.nav-item", { hasText: "Servers" }).first().click();
  await expect(page).toHaveURL(/\/servers$/);
  await expect(page.locator(".n-modal.n-card")).toHaveCount(0);
  await expect(page.locator(".rail-btn--editor")).toBeVisible();
  await expect(page.locator(".rail-btn--editor .rail-badge")).toHaveText("1");

  // Coming back restores the window with the dirty buffer intact.
  await page.goBack();
  await expect(page).toHaveURL(/\/files/);
  await page.locator(".rail-btn--editor").click();
  await expect(page.locator(".ed-window")).toBeVisible();
  await expect(page.locator(".cm-content")).toBeVisible();
  await expect(page.locator(".ed-dot")).toHaveCount(1);
});

/** dragWindow drags the floating editor by a point on its title bar. */
async function dragWindow(
  page: Page,
  target: { x: number; y: number },
): Promise<void> {
  const bar = page.locator(".ed-window-bar");
  const box = await bar.boundingBox();
  // Grab the bar's left side (the title), never the minimize/close controls.
  const fromX = box!.x + 40;
  const fromY = box!.y + box!.height / 2;
  await page.mouse.move(fromX, fromY);
  await page.mouse.down();
  await page.mouse.move(target.x, target.y, { steps: 10 });
  await page.mouse.up();
}

test("the editor window drags by its title bar and clamps to the viewport", async ({
  page,
}) => {
  const viewport = { width: 1440, height: 900 };
  await page.setViewportSize(viewport);
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });

  const window_ = page.locator(".ed-window");
  const before = await window_.boundingBox();
  expect(before).not.toBeNull();

  // Drag up and to the left: the position actually changes.
  await dragWindow(page, { x: before!.x - 180, y: before!.y - 140 });
  const moved = await window_.boundingBox();
  expect(moved!.x).toBeLessThan(before!.x);
  expect(moved!.y).toBeLessThan(before!.y);

  // Drag far past the top-left corner: it clamps on screen, not off it.
  await dragWindow(page, { x: -500, y: -500 });
  const clampedTopLeft = await window_.boundingBox();
  expect(clampedTopLeft!.x).toBeGreaterThanOrEqual(0);
  expect(clampedTopLeft!.y).toBeGreaterThanOrEqual(0);

  // Drag far past the bottom-right corner: it stays fully inside the viewport.
  await dragWindow(page, { x: viewport.width + 800, y: viewport.height + 800 });
  const clampedBottomRight = await window_.boundingBox();
  expect(clampedBottomRight!.x + clampedBottomRight!.width).toBeLessThanOrEqual(
    viewport.width + 1,
  );
  expect(clampedBottomRight!.y + clampedBottomRight!.height).toBeLessThanOrEqual(
    viewport.height + 1,
  );
});

test("the editor title bar keeps its controls pinned at every width", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);
  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });

  for (const width of [1280, 480, 390]) {
    await page.setViewportSize({ width, height: 800 });
    const geometry = await page.evaluate(() => {
      const box = (selector: string) => {
        const rect = document.querySelector(selector)?.getBoundingClientRect();
        return { left: rect?.left ?? -1, right: rect?.right ?? -1 };
      };
      const bar = document.querySelector(".ed-window-bar") as HTMLElement;
      const path = document.querySelector(".ed-window-path") as HTMLElement;
      return {
        window: box(".ed-window"),
        bar: box(".ed-window-bar"),
        title: box(".floating-window-title"),
        path: box(".ed-window-path"),
        min: box(".ed-window-min"),
        close: box(".ed-window-close"),
        paddingRight: Number.parseFloat(getComputedStyle(bar).paddingRight),
        barOverflow: bar.scrollWidth - bar.clientWidth,
        pathOverflow: path.scrollWidth - path.clientWidth,
        sharedBar: bar.classList.contains("floating-window-bar"),
      };
    });

    // The shared frame owns the bar, the controls sit at its right edge and
    // the title/context are ellipsized instead of pushing them out.
    expect(geometry.sharedBar).toBe(true);
    expect(geometry.close.right).toBeLessThanOrEqual(geometry.window.right + 0.5);
    expect(
      Math.abs(geometry.close.right - (geometry.bar.right - geometry.paddingRight)),
    ).toBeLessThanOrEqual(1);
    expect(geometry.min.right).toBeLessThanOrEqual(geometry.close.right);
    expect(geometry.title.right).toBeLessThanOrEqual(geometry.min.left + 0.5);
    expect(geometry.path.right).toBeLessThanOrEqual(geometry.min.left + 0.5);
    expect(geometry.barOverflow).toBeLessThanOrEqual(1);
    expect(geometry.pathOverflow).toBeGreaterThanOrEqual(0);
  }
});

/** persistedGeometry reads the editor window record from localStorage. */
async function persistedGeometry(page: Page): Promise<{
  width: number;
  height: number;
  x: number | null;
  y: number | null;
} | null> {
  return page.evaluate(() => {
    for (let index = 0; index < window.localStorage.length; index += 1) {
      const key = window.localStorage.key(index);
      if (key?.startsWith("gotham.files.editorWindow.v1.")) {
        return JSON.parse(window.localStorage.getItem(key) ?? "null") as {
          width: number;
          height: number;
          x: number | null;
          y: number | null;
        };
      }
    }
    return null;
  });
}

/** reopenEditor opens the file after a reload (clean tabs do not survive one). */
async function reopenEditor(page: Page): Promise<void> {
  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
}

test("a 1366x800 resize persists, clamps the drag and survives reload", async ({
  page,
}) => {
  const viewport = { width: 1366, height: 800 };
  await page.setViewportSize(viewport);
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });

  const window_ = page.locator(".ed-window");
  const initial = await window_.boundingBox();
  expect(initial).not.toBeNull();
  // The fitted default fills the viewport with side and chrome margins.
  expect(Math.round(initial!.x)).toBe(24);
  expect(Math.round(initial!.y)).toBe(88);
  expect(Math.round(initial!.width)).toBe(1318);
  expect(Math.round(initial!.height)).toBe(696);
  // Opening alone persists nothing: merely showing the window at a smaller
  // viewport never rewrites the saved size.
  expect(await persistedGeometry(page)).toBeNull();

  // Keyboard resize seeds from the displayed 1318 and persists the result.
  const grip = page.locator(".ed-resize--se");
  await grip.focus();
  await page.keyboard.press("Shift+ArrowLeft");
  await page.keyboard.press("Shift+ArrowLeft");
  await page.keyboard.press("Shift+ArrowLeft");
  const keyboardBox = await window_.boundingBox();
  expect(Math.round(keyboardBox!.width)).toBe(1318 - 192);
  expect(await persistedGeometry(page)).toMatchObject({
    width: 1318 - 192,
    height: 696,
  });

  // Reload restores the resized box exactly.
  await page.reload();
  await reopenEditor(page);
  const reloadedBox = await window_.boundingBox();
  expect(Math.round(reloadedBox!.width)).toBe(1318 - 192);
  expect(Math.round(reloadedBox!.height)).toBe(696);

  // A title-bar drag clamps against the displayed 696px height (y max 104)
  // and persists the position.
  const bar = page.locator(".ed-window-bar");
  const barBox = await bar.boundingBox();
  await page.mouse.move(barBox!.x + 40, barBox!.y + barBox!.height / 2);
  await page.mouse.down();
  await page.mouse.move(barBox!.x + 40, barBox!.y + barBox!.height / 2 + 50, {
    steps: 10,
  });
  await page.mouse.up();
  const draggedBox = await window_.boundingBox();
  expect(Math.round(draggedBox!.y)).toBe(104);
  expect(await persistedGeometry(page)).toMatchObject({ y: 104 });
  await page.reload();
  await reopenEditor(page);
  const draggedReloaded = await window_.boundingBox();
  expect(Math.round(draggedReloaded!.x)).toBe(Math.round(draggedBox!.x));
  expect(Math.round(draggedReloaded!.y)).toBe(104);

  // A corner drag seeds from the displayed size and persists the result.
  const gripBox = await page.locator(".ed-resize--se").boundingBox();
  expect(gripBox).not.toBeNull();
  const gripCenterX = gripBox!.x + gripBox!.width / 2;
  const gripCenterY = gripBox!.y + gripBox!.height / 2;
  await page.mouse.move(gripCenterX, gripCenterY);
  await page.mouse.down();
  await page.mouse.move(gripCenterX - 150, gripCenterY - 100, { steps: 10 });
  await page.mouse.up();
  const resizedBox = await window_.boundingBox();
  expect(Math.round(resizedBox!.width)).toBe(1126 - 150);
  expect(Math.round(resizedBox!.height)).toBe(696 - 100);
  expect(await persistedGeometry(page)).toMatchObject({
    width: 1126 - 150,
    height: 696 - 100,
  });
  await page.reload();
  await reopenEditor(page);
  const resizedReloaded = await window_.boundingBox();
  expect(Math.round(resizedReloaded!.width)).toBe(1126 - 150);
  expect(Math.round(resizedReloaded!.height)).toBe(696 - 100);
});

/**
 * floatingShadow resolves the shared elevation tokens in the browser and
 * reports the window's computed shadow. The token is resolved through a probe
 * element so the assertion compares computed values rather than the raw
 * custom-property text.
 */
async function floatingShadow(
  page: Page,
  selector: string,
): Promise<{ actual: string; expected: string; raised: string }> {
  return page.evaluate((target) => {
    const element = document.querySelector(target);
    if (!(element instanceof HTMLElement)) {
      throw new Error(`${target} is missing`);
    }
    const resolve = (name: string): string => {
      const raw = getComputedStyle(document.documentElement)
        .getPropertyValue(name)
        .trim();
      if (!raw) {
        throw new Error(`${name} is missing`);
      }
      const probe = document.createElement("div");
      probe.style.boxShadow = raw;
      document.body.append(probe);
      const value = getComputedStyle(probe).boxShadow;
      probe.remove();
      return value;
    };
    return {
      actual: getComputedStyle(element).boxShadow,
      expected: resolve("--elev-floating"),
      raised: resolve("--elev-raised"),
    };
  }, selector);
}

/** updateEditorSettings patches the live editor preferences store. */
async function updateEditorSettings(
  page: Page,
  patch: Record<string, unknown>,
): Promise<void> {
  await page.evaluate((value) => {
    const app = (document.querySelector("#app") as unknown as {
      __vue_app__: {
        config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
      };
    }).__vue_app__;
    const store = app.config.globalProperties.$pinia._s.get(
      "filesEditorSettings",
    ) as { update: (_patch: Record<string, unknown>) => void };
    store.update(value);
  }, patch);
}

test("the editor window floats on the soft elevation shadow in both themes", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  const editorWindow = page.locator(".ed-window");
  await expect(editorWindow).toBeVisible();

  // Dark editor theme: the window paints the dedicated floating elevation,
  // not the compact raised shadow the smaller popovers use.
  const dark = await floatingShadow(page, ".ed-window");
  expect(dark.expected).not.toBe("none");
  expect(dark.actual).toBe(dark.expected);
  expect(dark.actual).not.toBe(dark.raised);

  const editorBackground = () =>
    page
      .locator(".ed-host:visible .cm-editor")
      .evaluate((element) => getComputedStyle(element as HTMLElement).backgroundColor);
  await expect.poll(editorBackground).toBe("rgb(30, 31, 34)");
  const darkBox = await editorWindow.boundingBox();

  // The light editor theme only repaints the code surface: the lift and the
  // window geometry stay identical, so the shadow never shifts the layout.
  await updateEditorSettings(page, { theme: "light" });
  await expect.poll(editorBackground).toBe("rgb(242, 243, 245)");
  const light = await floatingShadow(page, ".ed-window");
  expect(light.actual).toBe(light.expected);
  const lightBox = await editorWindow.boundingBox();
  expect(Math.round(lightBox!.x)).toBe(Math.round(darkBox!.x));
  expect(Math.round(lightBox!.y)).toBe(Math.round(darkBox!.y));
  expect(Math.round(lightBox!.width)).toBe(Math.round(darkBox!.width));
  expect(Math.round(lightBox!.height)).toBe(Math.round(darkBox!.height));

  // A 390px phone keeps the same lift, the window inside the viewport and no
  // horizontal document overflow.
  await page.setViewportSize({ width: 390, height: 844 });
  const phone = await floatingShadow(page, ".ed-window");
  expect(phone.actual).toBe(phone.expected);
  const phoneFit = await page.evaluate(() => {
    const element = document.querySelector(".ed-window");
    if (!(element instanceof HTMLElement)) {
      throw new Error(".ed-window is missing");
    }
    const box = element.getBoundingClientRect();
    return {
      left: box.left,
      right: box.right,
      top: box.top,
      bottom: box.bottom,
      width: window.innerWidth,
      height: window.innerHeight,
      overflow: document.documentElement.scrollWidth - window.innerWidth,
    };
  });
  expect(phoneFit.left).toBeGreaterThanOrEqual(0);
  expect(phoneFit.right).toBeLessThanOrEqual(phoneFit.width);
  expect(phoneFit.top).toBeGreaterThanOrEqual(0);
  expect(phoneFit.bottom).toBeLessThanOrEqual(phoneFit.height);
  expect(phoneFit.overflow).toBeLessThanOrEqual(0);
});

test("the Close control closes every tab, guarding dirty buffers", async ({
  page,
}) => {  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${serverId}/files`);

  // Two open tabs, the active one dirty.
  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  await minimizeEditor(page);
  await page.locator(".fm-label", { hasText: "notes.md" }).first().dblclick();
  await expect(page.locator(".ed-host:visible .cm-content")).toBeVisible({
    timeout: 15_000,
  });
  await page.locator(".ed-host:visible .cm-content").click();
  await page.keyboard.type("# changed");
  await expect(page.locator(".ed-dot")).toHaveCount(1);

  // Close-all asks first and lists the unsaved file by path.
  await page.locator(".ed-window-close").click();
  const dialog = page.locator(".n-modal.files-editor-close-modal");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("Close all files?");
  await expect(dialog.locator(".ed-unsaved")).toContainText("notes.md");

  // Cancel keeps the whole editor and its buffer.
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(page.locator(".ed-window")).toBeVisible();
  await expect(page.locator(".ed-dot")).toHaveCount(1);

  // Discard and close removes the editor and its rail icon entirely.
  await page.locator(".ed-window-close").click();
  await dialog
    .getByRole("button", { name: "Discard and close", exact: true })
    .click();
  await expect(page.locator(".ed-window")).toHaveCount(0);
  await expect(page.locator(".rail-btn--editor")).toHaveCount(0);

  // A clean buffer closes at once, with no confirmation dialog.
  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  await page.locator(".ed-window-close").click();
  await expect(page.locator(".files-editor-close-modal")).toHaveCount(0);
  await expect(page.locator(".ed-window")).toHaveCount(0);
});

/**
 * TOOLBAR_LABELS lists the editor toolbar actions in DOM (and therefore Tab)
 * order, per locale. The Settings trigger lives in a child component; its
 * accessible name is the one asserted here.
 */
const TOOLBAR_LABELS: Record<string, string[]> = {
  en: ["Wrap", "Find / replace", "Go to line", "Settings", "History", "Save all", "Save"],
  vi: ["Ngắt dòng", "Tìm / thay thế", "Đi tới dòng", "Cài đặt", "Lịch sử", "Lưu tất cả", "Lưu"],
};

interface ToolbarCase {
  viewport: { width: number; height: number };
  /** Persisted editor window width; omitted keeps the fitted default. */
  windowWidth?: number;
  locale: "en" | "vi";
  explorer: boolean;
  /** Number of mocked revisions, so the History tooltip carries a count. */
  historyVersions?: number;
}

/**
 * TOOLBAR_CASES covers the widths JUS-286 measured: the fitted default on a
 * 1366px screen, the reported ~830px window, the 480px minimum, and the two
 * phone viewports, each with the explorer open and closed in both locales.
 */
const TOOLBAR_CASES: ToolbarCase[] = [
  { viewport: { width: 1366, height: 844 }, locale: "en", explorer: true },
  { viewport: { width: 1366, height: 844 }, locale: "vi", explorer: true },
  { viewport: { width: 1366, height: 844 }, windowWidth: 830, locale: "en", explorer: true },
  { viewport: { width: 1366, height: 844 }, windowWidth: 830, locale: "vi", explorer: true },
  { viewport: { width: 1366, height: 844 }, windowWidth: 830, locale: "en", explorer: false },
  { viewport: { width: 1366, height: 844 }, windowWidth: 830, locale: "vi", explorer: false },
  { viewport: { width: 1366, height: 844 }, windowWidth: 480, locale: "en", explorer: true },
  { viewport: { width: 1366, height: 844 }, windowWidth: 480, locale: "vi", explorer: true },
  { viewport: { width: 1366, height: 844 }, windowWidth: 480, locale: "en", explorer: false },
  { viewport: { width: 1366, height: 844 }, windowWidth: 480, locale: "vi", explorer: false },
  // Straddle the 320px editor-column breakpoint: 570px of window leaves a
  // 308px column (labels hidden), 600px leaves 338px (labels visible).
  { viewport: { width: 1366, height: 844 }, windowWidth: 570, locale: "en", explorer: true },
  { viewport: { width: 1366, height: 844 }, windowWidth: 600, locale: "en", explorer: true },
];

/** MOBILE_TOOLBAR_CASES are the fitted window on the two phone viewports. */
const MOBILE_TOOLBAR_CASES: ToolbarCase[] = [
  { viewport: { width: 480, height: 844 }, locale: "en", explorer: true },
  { viewport: { width: 480, height: 844 }, locale: "vi", explorer: true },
  { viewport: { width: 480, height: 844 }, locale: "en", explorer: false },
  { viewport: { width: 480, height: 844 }, locale: "vi", explorer: false },
  { viewport: { width: 390, height: 844 }, locale: "en", explorer: true },
  { viewport: { width: 390, height: 844 }, locale: "vi", explorer: true },
  { viewport: { width: 390, height: 844 }, locale: "en", explorer: false },
  { viewport: { width: 390, height: 844 }, locale: "vi", explorer: false },
];

/** seedToolbarPreferences pins the locale and the persisted window geometry. */
async function seedToolbarPreferences(
  page: Page,
  locale: string,
  width?: number,
): Promise<void> {
  await page.addInitScript(
    (value: { locale: string; width: number | null }) => {
      window.localStorage.setItem("gotham-locale", value.locale);
      const key = "gotham.files.editorWindow.v1.u-1";
      if (value.width === null) {
        window.localStorage.removeItem(key);
      } else {
        window.localStorage.setItem(
          key,
          JSON.stringify({
            version: 1,
            width: value.width,
            height: 696,
            x: null,
            y: null,
            minimized: false,
          }),
        );
      }
    },
    { locale, width: width ?? null },
  );
}

/** openToolbarCase opens app.yaml at the case geometry and explorer state. */
async function openToolbarCase(page: Page, testCase: ToolbarCase): Promise<void> {
  await page.setViewportSize(testCase.viewport);
  await seedToolbarPreferences(page, testCase.locale, testCase.windowWidth);
  await mockApi(page, { historyVersions: testCase.historyVersions });
  await page.goto(`${baseURL}/servers/${serverId}/files`);
  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  const toggle = page.locator(".ed-window-explorer");
  const open = (await toggle.getAttribute("aria-pressed")) === "true";
  if (open !== testCase.explorer) {
    await toggle.click();
  }
  await expect(toggle).toHaveAttribute("aria-pressed", String(testCase.explorer));
  // Let the wrap/container-query layout settle before measuring.
  await expect(page.locator(".ed-toolbar")).toBeVisible();
}

/**
 * toolbarGeometry reports, for every toolbar button: its box, its own
 * content overflow, whether its visible label is hidden, and whether its
 * centre point is hit-testable. It also reports the editor column width (the
 * container-query input), the toolbar's content right edge and the right edge
 * of the Save group.
 */
async function toolbarGeometry(page: Page): Promise<{
  windowRight: number;
  editorColumnWidth: number;
  toolbarRight: number;
  actionsGroupRight: number | null;
  toolbar: { clientWidth: number; scrollWidth: number };
  actions: {
    name: string;
    ariaLabel: string | null;
    left: number;
    right: number;
    contentOverflow: number;
    labelWidth: number;
    labelHidden: boolean;
    hitTestable: boolean;
  }[];
}> {
  return page.evaluate(() => {
    const windowElement = document.querySelector(".ed-window");
    const toolbar = document.querySelector(".ed-toolbar");
    const column = document.querySelector(".ed-right");
    if (!(windowElement instanceof HTMLElement) || !(toolbar instanceof HTMLElement)) {
      throw new Error("the editor window or toolbar is missing");
    }
    const windowRight = windowElement.getBoundingClientRect().right;
    const actions = [
      ...toolbar.querySelectorAll<HTMLElement>(".n-button"),
    ].map((button) => {
      const box = button.getBoundingClientRect();
      const hit = document.elementFromPoint(
        box.left + box.width / 2,
        box.top + box.height / 2,
      );
      const content = button.querySelector<HTMLElement>(".n-button__content");
      const labelWidth = content?.getBoundingClientRect().width ?? 0;
      return {
        // The accessible name is the aria-label; an icon-only control has no
        // text content to fall back to (JUS-286 fix round 1).
        name: button.getAttribute("aria-label") ?? "",
        ariaLabel: button.getAttribute("aria-label"),
        left: box.left,
        right: box.right,
        contentOverflow: button.scrollWidth - button.clientWidth,
        labelWidth,
        labelHidden: labelWidth < 1,
        hitTestable: hit === button || button.contains(hit),
      };
    });
    const group = toolbar.querySelector<HTMLElement>(".ed-toolbar-actions");
    return {
      windowRight,
      editorColumnWidth: column?.getBoundingClientRect().width ?? 0,
      toolbarRight: toolbar.getBoundingClientRect().right,
      actionsGroupRight: group?.getBoundingClientRect().right ?? null,
      toolbar: {
        clientWidth: toolbar.clientWidth,
        scrollWidth: toolbar.scrollWidth,
      },
      actions,
    };
  });
}

/** EDITOR_COLUMN_BREAKPOINT mirrors the @container query in FilesEditor.vue. */
const EDITOR_COLUMN_BREAKPOINT = 320;

/** assertToolbarActionsFit is the JUS-286 acceptance proof for one case. */
async function assertToolbarActionsFit(page: Page, testCase: ToolbarCase): Promise<void> {
  const label = `${testCase.viewport.width}px viewport, ${testCase.locale}, ${
    testCase.windowWidth ? `${testCase.windowWidth}px window, ` : ""
  }explorer ${testCase.explorer ? "open" : "closed"}`;
  const geometry = await toolbarGeometry(page);

  // The toolbar itself never overflows its column: no clipped row.
  expect(
    geometry.toolbar.scrollWidth,
    `${label}: toolbar scrollWidth`,
  ).toBeLessThanOrEqual(geometry.toolbar.clientWidth + 1);

  // Every action exists, in Tab order, with a non-empty aria-label. The name
  // comes from the attribute, not the (sometimes hidden) text content.
  for (const action of geometry.actions) {
    expect(action.ariaLabel, `${label}: action aria-label`).toBeTruthy();
  }
  expect(
    geometry.actions.map((action) => action.name),
    `${label}: actions`,
  ).toEqual(TOOLBAR_LABELS[testCase.locale]);

  // The labels are hidden exactly when the editor column is at or below the
  // container-query breakpoint; above it they are visible. This pins the
  // breakpoint value, not just the icon-only rendering.
  const labelsHidden = geometry.editorColumnWidth <= EDITOR_COLUMN_BREAKPOINT;
  for (const action of geometry.actions) {
    expect(
      action.labelHidden,
      `${label}: ${action.name} label hidden (column ${geometry.editorColumnWidth}px)`,
    ).toBe(labelsHidden);
  }

  // The Save group rides the right edge of its row: its right edge equals the
  // toolbar content edge. Dropping margin-left:auto breaks this.
  expect(geometry.actionsGroupRight, `${label}: save group`).not.toBeNull();
  expect(
    Math.abs(geometry.actionsGroupRight! - geometry.toolbarRight),
    `${label}: save group right-aligned`,
  ).toBeLessThanOrEqual(1);

  // No action's right edge escapes the window frame, and none is clipped or
  // covered: elementFromPoint at its centre resolves to the action itself.
  for (const action of geometry.actions) {
    expect(action.right, `${label}: ${action.name} right edge`).toBeLessThanOrEqual(
      geometry.windowRight + 0.5,
    );
    expect(action.left, `${label}: ${action.name} left edge`).toBeGreaterThanOrEqual(0);
    expect(
      action.contentOverflow,
      `${label}: ${action.name} content clipped`,
    ).toBeLessThanOrEqual(1);
    expect(action.hitTestable, `${label}: ${action.name} hit-testable`).toBe(true);
  }
}

test("every editor toolbar action stays reachable at narrow window widths", async ({
  page,
}) => {
  test.setTimeout(120_000);
  for (const testCase of TOOLBAR_CASES) {
    await openToolbarCase(page, testCase);
    await assertToolbarActionsFit(page, testCase);
  }
});

test("every editor toolbar action stays reachable on phone viewports", async ({
  page,
}) => {
  test.setTimeout(90_000);
  for (const testCase of MOBILE_TOOLBAR_CASES) {
    await openToolbarCase(page, testCase);
    await assertToolbarActionsFit(page, testCase);
  }
});

/**
 * ToolbarTabStop is one Tab stop inside the editor toolbar as the browser
 * resolved it: accessible name, focus-visible state and computed focus ring.
 */
interface ToolbarTabStop {
  name: string;
  focusVisible: boolean;
  outlineStyle: string;
  outlineWidth: number;
}

/**
 * nextToolbarTabStop presses Tab once and reports the newly focused toolbar
 * button's name and visible focus ring (the global :focus-visible outline).
 */
async function nextToolbarTabStop(page: Page): Promise<ToolbarTabStop> {
  await page.keyboard.press("Tab");
  return page.evaluate(() => {
    const element = document.activeElement;
    if (!(element instanceof HTMLElement)) {
      throw new Error("Tab did not focus an element");
    }
    const style = getComputedStyle(element);
    return {
      name: element.getAttribute("aria-label") ?? "",
      focusVisible: element.matches(":focus-visible"),
      outlineStyle: style.outlineStyle,
      outlineWidth: Number.parseFloat(style.outlineWidth) || 0,
    };
  });
}

test("icon-only toolbar buttons keep tooltips, Tab order and a visible focus ring", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const testCase: ToolbarCase = {
    viewport: { width: 390, height: 844 },
    locale: "en",
    explorer: true,
  };
  await openToolbarCase(page, testCase);

  // A dirty buffer enables Save and Save all, so all seven actions are
  // focusable and hoverable (the disabled pair would swallow both).
  await page.locator(".cm-content").click();
  await page.keyboard.type("#");
  await expect(page.locator(".ed-dot")).toHaveCount(1);
  const geometry = await toolbarGeometry(page);
  expect(geometry.editorColumnWidth).toBeLessThanOrEqual(EDITOR_COLUMN_BREAKPOINT);
  expect(geometry.actions.every((action) => action.labelHidden)).toBe(true);

  // The labels are gone, so each icon needs its JUS-156 tooltip. Hover and
  // keyboard focus both open it, with the same copy as the aria-label.
  const tooltip = page.locator(".app-tooltip-content");
  const saveAll = page.locator(".ed-toolbar .n-button").nth(5);
  await saveAll.hover();
  await expect(tooltip).toHaveText("Save all");
  await page.mouse.move(5, 5);
  await expect(tooltip).toBeHidden();

  await page.locator(".ed-tab-name").focus();
  for (const [index, name] of TOOLBAR_LABELS[testCase.locale].entries()) {
    const stop = await nextToolbarTabStop(page);
    expect(stop.name, `Tab stop ${index + 1}`).toBe(name);
    expect(stop.focusVisible, `Tab stop ${index + 1} focus-visible`).toBe(true);
    expect(stop.outlineStyle, `Tab stop ${index + 1} outline`).toBe("solid");
    expect(stop.outlineWidth, `Tab stop ${index + 1} outline width`).toBeGreaterThanOrEqual(2);
    // The keyboard-focused icon shows its tooltip too.
    await expect(tooltip, `Tab stop ${index + 1} tooltip`).toHaveText(name);
  }

  // Wrapping the trigger in a tooltip must not break the settings popover or
  // its focus return, whichever way the popover closes.
  const settingsButton = page.locator(".ed-toolbar .n-button[aria-label='Settings']");
  await settingsButton.click();
  await expect(page.locator(".ed-settings")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.locator(".ed-settings")).toBeHidden();
  await expect(settingsButton).toBeFocused();

  await settingsButton.click();
  await expect(page.locator(".ed-settings")).toBeVisible();
  // Click the toolbar's own padding: outside the popover, inside the window.
  await page.locator(".ed-toolbar").click({ position: { x: 2, y: 2 } });
  await expect(page.locator(".ed-settings")).toBeHidden();
  await expect(settingsButton).toBeFocused();
});

test("the History tooltip keeps the revision count when the label is hidden", async ({
  page,
}) => {
  test.setTimeout(120_000);
  const cases: ToolbarCase[] = [
    {
      viewport: { width: 1366, height: 844 },
      windowWidth: 480,
      locale: "en",
      explorer: true,
      historyVersions: 2,
    },
    {
      viewport: { width: 1366, height: 844 },
      windowWidth: 480,
      locale: "vi",
      explorer: true,
      historyVersions: 2,
    },
  ];
  for (const testCase of cases) {
    await openToolbarCase(page, testCase);
    const historyName = TOOLBAR_LABELS[testCase.locale][4];
    const history = page.locator(`.ed-toolbar .n-button[aria-label="${historyName}"]`);

    // The narrow layout hides the label and the count badge; the count must
    // survive in the tooltip (and its aria-describedby description).
    expect((await toolbarGeometry(page)).actions[4].labelHidden).toBe(true);
    await history.hover();
    await expect(page.locator(".app-tooltip-content")).toHaveText(
      testCase.locale === "en" ? "History (2)" : "Lịch sử (2)",
    );
    await expect(history).toHaveAttribute(
      "aria-describedby",
      /app-tooltip-\d+/,
    );
  }
});
