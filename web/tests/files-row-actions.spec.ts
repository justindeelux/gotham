import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, document:readonly, HTMLElement:readonly, window:readonly */
// JUS-133 discoverability proof: the committed webdist is served statically
// with the control-plane file API mocked in real Chromium. Measured claims:
//   - the ⋮ row-menu button is visible with no hover (touch reachable), while
//     the other inline row actions stay hover/focus-revealed on pointer devices;
//   - the inline actions reveal on keyboard focus, so a keyboard user reaches
//     them without a pointer;
//   - on a touch device (hover: none) the whole action set is visible and the
//     ⋮ opens the row menu;
//   - every icon-only control (toolbar + row actions) has an accessible name
//     (aria-label) and a tooltip (title), in English and Vietnamese;
//   - a disabled row action (a file's Download without the content role) stays
//     dimmed when revealed instead of being forced fully opaque.

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

/** listing builds a folder, an editable file, an archive and a special row. */
function listing() {
  return [
    { name: "app.yaml", kind: "file", size: 2458, mode: 0o100644, modified_at: "2026-10-12T09:02:00Z", etag: '"a"' },
    { name: "backup.tar.gz", kind: "file", size: 4096, mode: 0o100644, modified_at: "2026-10-12T09:10:00Z", etag: '"b"' },
    { name: "config", kind: "directory", size: 0, mode: 0o040755, modified_at: "2026-10-12T09:14:00Z", etag: '"c"' },
    { name: "run.sock", kind: "special", size: 0, mode: 0o140777, modified_at: "2026-10-12T09:19:00Z", etag: '"d"' },
  ];
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

/** OpenOptions tunes the seeded team role and UI locale per test. */
interface OpenOptions {
  entries?: ReturnType<typeof listing>;
  /** Team role; anything but owner/admin hides the content (download) gate. */
  role?: string;
  /** UI locale stored before boot (defaults to English). */
  locale?: string;
}

/** openFiles seeds a session, mocks the file API and lands on the browser. */
async function openFiles(page: Page, options: OpenOptions = {}): Promise<void> {
  const entries = options.entries ?? listing();
  const role = options.role ?? "owner";
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.addInitScript(
    ({ sessionValue, storedLocale }) => {
      window.localStorage.setItem("gotham.auth.session", sessionValue);
      if (storedLocale !== "") {
        window.localStorage.setItem("gotham-locale", storedLocale);
      }
    },
    { sessionValue: JSON.stringify(session), storedLocale: options.locale ?? "" },
  );
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: session.user });
  });
  await page.route("**/api/v1/teams", async (route) => {
    await route.fulfill({ json: { teams: [{ ...team, role }] } });
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
    await route.fulfill({ json: { entries, truncated: false, cursor: "" } });
  });

  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label b").first()).toBeVisible();
}

/** rowFor returns the browser row whose name matches. */
function rowFor(page: Page, name: string): Locator {
  return page
    .locator(".fm-table .fm-row", { has: page.locator(`b:text-is("${name}")`) })
    .first();
}

test.describe("icon-only controls are accessible", () => {
  test("the ⋮ is visible at rest; inline actions reveal on hover, then focus", async ({
    page,
  }) => {
    await openFiles(page);
    const row = rowFor(page, "app.yaml");
    const inline = row.locator(".fm-action");
    const more = page.getByRole("button", { name: "More actions for app.yaml" });
    const rowMenu = page.getByRole("menu", { name: "More actions for app.yaml" });

    // No hover: the inline actions are hidden.
    await expect(inline.first()).toHaveCSS("opacity", "0");

    // Hovering the row reveals the inline actions.
    await row.hover();
    await expect(inline.first()).toHaveCSS("opacity", "1");

    // Moving the pointer away hides them again.
    await page.locator(".fm-card-title").hover();
    await expect(inline.first()).toHaveCSS("opacity", "0");

    // Keyboard focus reveals them too, with no pointer involved.
    await row.locator(".fm-label").focus();
    await expect(inline.first()).toHaveCSS("opacity", "1");

    // Focusing the ⋮ also keeps the row's inline actions revealed (focus-within).
    await more.focus();
    await expect(inline.first()).toHaveCSS("opacity", "1");

    // The ⋮ is reachable without a hover: a real click opens the row menu. (A
    // bare `opacity: 1` here cannot fail — the ⋮ is never dimmed — so the
    // assertion is the disclosure it actually drives.)
    await more.click();
    await expect(rowMenu).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(rowMenu).toBeHidden();
  });

  test("every toolbar and row icon-only control has an aria-label and a title", async ({
    page,
  }) => {
    await openFiles(page);
    // Select the whole listing so every bulk action renders (enabled), then
    // shrink to the icon-only toolbar layout.
    await page.locator(".fm-head .n-checkbox").click();
    const toolbar = page.locator(".fm-toolbar");

    // Row actions are always icon-only; each pairs an accessible name with a
    // tooltip.
    const fileRow = rowFor(page, "app.yaml");
    for (const label of [
      "Download app.yaml",
      "Rename app.yaml",
      "Delete app.yaml",
      "More actions for app.yaml",
    ]) {
      const button = fileRow.getByRole("button", { name: label, exact: true });
      await expect(button, label).toHaveAttribute("aria-label", label);
      await expect(button, label).toHaveAttribute("title", label);
    }
    const archiveRow = rowFor(page, "backup.tar.gz");
    for (const label of ["Extract here…", "More actions for backup.tar.gz"]) {
      const button = archiveRow.getByRole("button", { name: label, exact: true });
      await expect(button, label).toHaveAttribute("aria-label", label);
      await expect(button, label).toHaveAttribute("title", label);
    }
    const folderRow = rowFor(page, "config");
    for (const label of [
      "Open config",
      "Rename config",
      "Delete config",
      "More actions for config",
    ]) {
      const button = folderRow.getByRole("button", { name: label, exact: true });
      await expect(button, label).toHaveAttribute("aria-label", label);
      await expect(button, label).toHaveAttribute("title", label);
    }

    // The toolbar drops its labels on small screens; each icon-only button must
    // keep both an accessible name and a tooltip.
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
      const button = toolbar.getByRole("button", { name, exact: true });
      await expect(button, name).toHaveAttribute("aria-label", name);
      await expect(button, name).toHaveAttribute("title", name);
    }
  });

  test("a special row offers no row menu", async ({ page }) => {
    await openFiles(page);
    await expect(
      page.getByRole("button", { name: "More actions for run.sock" }),
    ).toHaveCount(0);
  });

  test("a disabled row action stays dimmed when the row is revealed", async ({
    page,
  }) => {
    // A member cannot read content, so a file's Download action is disabled.
    await openFiles(page, { role: "member" });
    const row = rowFor(page, "app.yaml");
    const download = row.getByRole("button", {
      name: "Download app.yaml",
      exact: true,
    });
    await expect(download).toBeDisabled();

    // naive renders a disabled button at its --n-opacity-disabled value; the
    // fix must keep exactly that when the row reveals the action. Read the
    // value first so the assertion pins the dim rather than an arbitrary half.
    const dim = await download.evaluate((element) =>
      window
        .getComputedStyle(element)
        .getPropertyValue("--n-opacity-disabled")
        .trim(),
    );
    expect(dim).not.toBe("");
    expect(Number(dim)).toBeLessThan(1);

    // Hidden at rest, exactly like the enabled inline actions...
    await expect(download).toHaveCSS("opacity", "0");

    // ...and dimmed to naive's disabled value once the reveal transition
    // settles. (Before the fix the reveal rule forced it to opacity 1.)
    await row.hover();
    await expect(download).toHaveCSS("opacity", dim);
  });

  test("row and toolbar labels are localized (vi)", async ({ page }) => {
    await openFiles(page, { locale: "vi" });

    const fileRow = rowFor(page, "app.yaml");
    for (const label of [
      "Tải app.yaml",
      "Đổi tên app.yaml",
      "Xóa app.yaml",
      "Thêm thao tác cho app.yaml",
    ]) {
      const button = fileRow.getByRole("button", { name: label, exact: true });
      await expect(button, label).toHaveAttribute("aria-label", label);
      await expect(button, label).toHaveAttribute("title", label);
    }
    await expect(
      rowFor(page, "config").getByRole("button", { name: "Mở config", exact: true }),
    ).toHaveAttribute("title", "Mở config");

    // The toolbar's icon-only labels are localized too.
    await page.locator(".fm-head .n-checkbox").click();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator(".fm-btn-label").first()).toBeHidden();
    for (const name of [
      "Tạo mới",
      "Tải lên",
      "Tải về",
      "Sao chép",
      "Cắt",
      "Nén…",
      "Xóa",
      "Bỏ chọn",
    ]) {
      const button = page
        .locator(".fm-toolbar")
        .getByRole("button", { name, exact: true });
      await expect(button, name).toHaveAttribute("aria-label", name);
      await expect(button, name).toHaveAttribute("title", name);
    }
  });
});

// JUS-140: keyboard/touch reachability of the row menu state, the mode and
// modified details, focus when the selection bar hides, and the Paste button's
// accessible name (label-in-name). Every assertion fails before its fix.
test.describe("row action accessibility nits (JUS-140)", () => {
  test("the row ⋮ exposes its menu state (haspopup + expanded)", async ({
    page,
  }) => {
    await openFiles(page);
    const more = page.getByRole("button", { name: "More actions for app.yaml" });
    const rowMenu = page.getByRole("menu", { name: "More actions for app.yaml" });

    await expect(more).toHaveAttribute("aria-haspopup", "menu");
    await expect(more).toHaveAttribute("aria-expanded", "false");

    // A real click opens the menu and flips the state.
    await more.click();
    await expect(rowMenu).toBeVisible();
    await expect(more).toHaveAttribute("aria-expanded", "true");

    // Escape closes it on the dropdown's own route, resetting the state.
    await page.keyboard.press("Escape");
    await expect(rowMenu).toBeHidden();
    await expect(more).toHaveAttribute("aria-expanded", "false");

    // Click-away closes it too, on the dropdown's own route.
    await more.click();
    await expect(more).toHaveAttribute("aria-expanded", "true");
    await page.locator(".fm-card-title").click();
    await expect(rowMenu).toBeHidden();
    await expect(more).toHaveAttribute("aria-expanded", "false");

    // The same row menu opened by right-click (the context-menu route) is the
    // ⋮'s controlled content, so its state reflects it.
    const folderMore = page.getByRole("button", {
      name: "More actions for config",
    });
    await rowFor(page, "config").click({ button: "right" });
    await expect(page.getByRole("menu", { name: "More actions for config" })).toBeVisible();
    await expect(folderMore).toHaveAttribute("aria-expanded", "true");
    await page.keyboard.press("Escape");
    await expect(folderMore).toHaveAttribute("aria-expanded", "false");
  });

  test("the roving row's accessible description carries the mode and modified time", async ({
    page,
  }) => {
    await openFiles(page);
    // One shared, visually hidden description node — never one per row.
    await expect(page.locator("#fm-row-details")).toHaveCount(1);

    /** entryOf returns the data-entry of the row/tile a control belongs to. */
    const entryOf = (locator: Locator): Promise<string> =>
      locator.evaluate(
        (el) =>
          (el.closest("[data-entry]") as HTMLElement | null)?.dataset.entry ?? "",
      );
    /** expectedDetails is the localized description for one entry. */
    const expectedDetails = async (entry: string): Promise<string> => {
      const row = page.locator(`.fm-row[data-entry="${entry}"]`);
      const mode = await row.locator(".fm-mode").getAttribute("title");
      const date = await row.locator(".fm-date").getAttribute("title");
      return `Mode ${mode}, modified ${date}`;
    };

    const roving = page.locator('.fm-table .fm-label[tabindex="0"]');
    await expect(roving).toHaveCount(1);
    await expect(roving).toHaveAttribute("aria-describedby", "fm-row-details");
    const first = await entryOf(roving);
    await expect(roving).toHaveAccessibleDescription(await expectedDetails(first));
    // The Mode/Modified cells keep their hover tooltips (mouse users).
    await expect(
      page.locator(`.fm-row[data-entry="${first}"] .fm-mode`),
    ).toHaveAttribute("title", /./);

    // Roving moves to the next row: the one shared node follows it (still exactly
    // one tab stop — JUS-139) and the previous row loses the reference.
    await roving.focus();
    await page.keyboard.press("ArrowDown");
    const moved = page.locator('.fm-table .fm-label[tabindex="0"]');
    await expect(moved).toHaveCount(1);
    const second = await entryOf(moved);
    expect(second).not.toBe(first);
    await expect(moved).toHaveAttribute("aria-describedby", "fm-row-details");
    await expect(moved).toHaveAccessibleDescription(await expectedDetails(second));
    await expect(
      page.locator(`.fm-row[data-entry="${first}"] .fm-label`),
    ).not.toHaveAttribute("aria-describedby", "fm-row-details");
    await expect(page.locator('.fm-label[tabindex="0"]')).toHaveCount(1);

    // The grid tile carries the same single-node description (mode is not a
    // visible cell there).
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator('.fm-tile[tabindex="0"]')).toHaveCount(1);
    const tile = page.locator('.fm-tile[tabindex="0"]');
    await expect(tile).toHaveAttribute("aria-describedby", "fm-row-details");
    await expect(tile).toHaveAccessibleDescription(/Mode |Quyền /);
  });

  test("an open ⋮ is not stale after its row is filtered out and back", async ({
    page,
  }) => {
    await openFiles(page);
    const more = page.getByRole("button", { name: "More actions for app.yaml" });
    await more.click();
    await expect(more).toHaveAttribute("aria-expanded", "true");

    // Type in the folder filter: the row unmounts while its menu is open, and no
    // dropdown close event fires for an unmounted trigger. Clearing the filter
    // renders the row again — its ⋮ must not come back expanded.
    const filter = page.locator(".fm-search input");
    await filter.fill("config");
    await expect(rowFor(page, "app.yaml")).toHaveCount(0);
    await filter.fill("");
    await expect(rowFor(page, "app.yaml")).toBeVisible();

    await expect(page.getByRole("menu")).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "More actions for app.yaml" }),
    ).toHaveAttribute("aria-expanded", "false");
  });

  test("an open ⋮ is not inherited by a same-named row in another folder", async ({
    page,
  }) => {
    await openFiles(page);
    await page.getByRole("button", { name: "More actions for app.yaml" }).click();
    await expect(
      page.getByRole("button", { name: "More actions for app.yaml" }),
    ).toHaveAttribute("aria-expanded", "true");

    // Keyboard-open the `config` folder so no pointerdown closes the dropdown:
    // the next listing also holds an `app.yaml`, which must not inherit the
    // previous row's expanded state.
    await rowFor(page, "config").locator(".fm-label").focus();
    await page.keyboard.press("Enter");
    await expect(rowFor(page, "app.yaml")).toBeVisible();
    await expect(
      page.getByRole("button", { name: "More actions for app.yaml" }),
    ).toHaveAttribute("aria-expanded", "false");
  });

  test("the selection bar hiding never drops focus to <body>", async ({ page }) => {
    await openFiles(page);
    const checkbox = rowFor(page, "app.yaml").locator(".fm-select .n-checkbox");

    // A real click selects the row, so the Clear control (which holds focus when
    // clicked) is shown inside the bar.
    await checkbox.click();
    const clear = page.getByRole("button", { name: "Clear", exact: true });
    await expect(clear).toBeVisible();
    await clear.click();

    // The bar hides; focus must land on the roving row, not the body.
    await expect(page.locator("body")).not.toBeFocused();
    await expect(page.locator('.fm-label[tabindex="0"]')).toBeFocused();

    // Unchecking the last row (the checkbox route) hides the bar too, but the
    // hand-off only fires when focus was inside the bar, so it must not steal
    // focus here: focus stays in the row the checkbox belongs to, never <body>.
    await checkbox.click();
    await expect(clear).toBeVisible();
    await checkbox.click();
    await expect(page.locator("body")).not.toBeFocused();
    expect(
      await page.evaluate(
        () => document.activeElement?.closest('[data-entry="app.yaml"]') !== null,
      ),
    ).toBe(true);
  });

  test("the Paste button's accessible name contains its visible label", async ({
    page,
  }) => {
    await openFiles(page);
    // Fill the clipboard so the Paste button (label "Paste (1)") renders.
    await rowFor(page, "app.yaml").locator(".fm-select .n-checkbox").click();
    await page.getByRole("button", { name: "Copy", exact: true }).click();

    const paste = page.getByRole("button", { name: /^Paste \(1\)/ });
    await expect(paste).toBeVisible();
    await expect(paste).toHaveAttribute("aria-label", /^Paste \(1\)/);
    // Label-in-name: the accessible name contains the visible label text. (Here
    // the button is also disabled by the same-folder problem, whose text follows
    // the visible label in the name.)
    await expect(paste).toHaveAccessibleName(/Paste \(1\)/);
  });
});

test.describe("touch devices", () => {
  // hasTouch makes Chromium report `(hover: none)` / `(pointer: coarse)`.
  test.use({ hasTouch: true });

  test("the whole action set is visible and the ⋮ opens the row menu", async ({
    page,
  }) => {
    await openFiles(page);
    expect(await page.evaluate(() => window.matchMedia("(hover: none)").matches)).toBe(true);

    const row = rowFor(page, "app.yaml");
    await expect(row.locator(".fm-action").first()).toHaveCSS("opacity", "1");

    const more = page.getByRole("button", { name: "More actions for app.yaml" });
    await expect(more).toHaveCSS("opacity", "1");
    await more.click();
    await expect(
      page.getByRole("menu", { name: "More actions for app.yaml" }),
    ).toBeVisible();
    await expect(page.getByRole("menuitem", { name: "Edit in editor" })).toBeVisible();
  });

  test("a disabled row action stays dimmed under hover:none", async ({ page }) => {
    await openFiles(page, { role: "member" });
    const download = rowFor(page, "app.yaml").getByRole("button", {
      name: "Download app.yaml",
      exact: true,
    });
    const dim = await download.evaluate((element) =>
      window
        .getComputedStyle(element)
        .getPropertyValue("--n-opacity-disabled")
        .trim(),
    );
    expect(Number(dim)).toBeGreaterThan(0);
    expect(Number(dim)).toBeLessThan(1);
    await expect(download).toHaveCSS("opacity", dim);
  });
});
