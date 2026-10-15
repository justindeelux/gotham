import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, document:readonly, HTMLElement:readonly, window:readonly, HTMLButtonElement:readonly, getComputedStyle:readonly */
// JUS-133 discoverability proof: the committed webdist is served statically
// with the control-plane file API mocked in real Chromium. Measured claims:
//   - the ⋮ row-menu button is visible with no hover (touch reachable), while
//     the other inline row actions stay hover/focus-revealed on pointer devices;
//   - the inline actions reveal on keyboard focus, so a keyboard user reaches
//     them without a pointer;
//   - on a touch device (hover: none) the whole action set is visible and the
//     ⋮ opens the row menu;
//   - every icon-only control (toolbar + row actions) has an accessible name
//     (aria-label) and a tooltip (the shared AppTooltip), in English and
//     Vietnamese;
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

/** moreFor / menuFor locate one row's ⋮ trigger and its (teleported) menu. */
function moreFor(page: Page, name: string): Locator {
  return page.getByRole("button", { name: `More actions for ${name}` });
}
function menuFor(page: Page, name: string): Locator {
  return page.getByRole("menu", { name: `More actions for ${name}` });
}

/** expectMenuOpen asserts the ⋮ popup is visible AND aria-expanded is true. */
async function expectMenuOpen(page: Page, name: string): Promise<void> {
  await expect(menuFor(page, name)).toBeVisible();
  await expect(moreFor(page, name)).toHaveAttribute("aria-expanded", "true");
}

/** expectMenuClosed asserts no row menu is open AND the trigger is collapsed. */
async function expectMenuClosed(page: Page, name: string): Promise<void> {
  await expect(page.getByRole("menu")).toHaveCount(0);
  await expect(moreFor(page, name)).toHaveAttribute("aria-expanded", "false");
}

/**
 * expectMenuAgrees asserts the ⋮ popup and aria-expanded never disagree —
 * whichever state a route leaves them in (menu visible <=> aria-expanded true).
 */
async function expectMenuAgrees(page: Page, name: string): Promise<void> {
  await expect
    .poll(async () => {
      const menu = menuFor(page, name);
      const open = (await menu.count()) > 0 && (await menu.first().isVisible());
      const expanded = await moreFor(page, name).getAttribute("aria-expanded");
      return `${open ? "open" : "closed"}:${expanded ?? "none"}`;
    })
    .toMatch(/^(open:true|closed:false)$/);
}

/** expectDescribes asserts ONLY the roving row is described by `entry`, and that
 * the single shared node carries that entry's mode and absolute modified time. */
async function expectDescribes(page: Page, entry: string): Promise<void> {
  const row = page.locator(`.fm-row[data-entry="${entry}"]`);
  await expect(row.locator(".fm-label")).toHaveAttribute(
    "aria-describedby",
    "fm-row-details",
  );
  // Negative half: exactly one row references the node, so every other row's
  // label carries no aria-describedby.
  await expect(
    page.locator('.fm-row .fm-label[aria-describedby="fm-row-details"]'),
  ).toHaveCount(1);
  const mode = await row.locator(".fm-mode").getAttribute("title");
  const date = await row.locator(".fm-date").getAttribute("title");
  await expect(page.locator("#fm-row-details")).toHaveText(
    `Mode ${mode}, modified ${date}`,
  );
}

/** escapeRegex makes one tooltip label safe to anchor in a RegExp. */
function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * tooltipFor locates the ONE tooltip whose copy is exactly `label`. naive keeps
 * every built tooltip body in the DOM (display:none) once created, so scoping by
 * exact text is what keeps the assertions unambiguous.
 */
function tooltipFor(page: Page, label: string): Locator {
  return page.locator(".n-tooltip", {
    hasText: new RegExp(`^\\s*${escapeRegex(label)}\\s*$`),
  });
}

/** expectTooltipOnHover proves the control's `label` opens as a hover tooltip. */
async function expectTooltipOnHover(
  page: Page,
  control: Locator,
  label: string,
): Promise<void> {
  const tooltip = tooltipFor(page, label);
  await control.hover();
  await expect(tooltip, `hover tooltip for "${label}"`).toBeVisible();
  await page.mouse.move(0, 0);
  await expect(tooltip).toBeHidden();
}

/**
 * focusViaKeyboard moves focus onto `control` with a REAL Tab from the element
 * just before it, so the focus is keyboard-originated (JUS-156 F4): a
 * programmatic .focus() must NOT open the tooltip, a Tab must.
 */
async function focusViaKeyboard(
  page: Page,
  control: Locator,
): Promise<void> {
  await control.evaluate((element) => {
    const focusables = Array.from(
      document.querySelectorAll<HTMLElement>(
        'a[href], button, input, select, textarea, [tabindex]',
      ),
    );
    const index = focusables.indexOf(element as HTMLElement);
    const previous = index > 0 ? focusables[index - 1] : null;
    (previous ?? document.body).focus();
  });
  for (let attempt = 0; attempt < 6; attempt += 1) {
    await page.keyboard.press("Tab");
    if (await control.evaluate((element) => element === document.activeElement)) {
      return;
    }
  }
  throw new Error("focusViaKeyboard: control never received focus");
}

/** expectTooltipOnFocus proves `label` opens for a keyboard-originated focus. */
async function expectTooltipOnFocus(
  page: Page,
  control: Locator,
  label: string,
): Promise<void> {
  const tooltip = tooltipFor(page, label);
  await focusViaKeyboard(page, control);
  await expect(tooltip, `focus tooltip for "${label}"`).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(tooltip).toBeHidden();
}

/**
 * expectIconOnlyTooltips checks EVERY button inside one area: each button with
 * no visible text (an icon-only control) must keep a non-empty aria-label and
 * open that label as a hover tooltip. A visible-label button is skipped.
 */
async function expectIconOnlyTooltips(page: Page, area: string): Promise<void> {
  const buttons = page.locator(`${area} button`);
  const count = await buttons.count();
  expect(count, `${area} renders controls`).toBeGreaterThan(0);
  let checked = 0;
  for (let index = 0; index < count; index += 1) {
    const button = buttons.nth(index);
    const iconOnly = await button.evaluate(
      (element) => (element as HTMLElement).innerText.trim() === "",
    );
    if (!iconOnly) {
      continue;
    }
    checked += 1;
    const label = await button.getAttribute("aria-label");
    expect(label, `${area} button ${index} keeps an accessible name`).toBeTruthy();
    await expectTooltipOnHover(page, button, label as string);
  }
  expect(checked, `${area} has icon-only controls`).toBeGreaterThan(0);
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

  test("every icon-only button in the three areas keeps an aria-label and a tooltip", async ({
    page,
  }) => {
    await openFiles(page);

    // Row actions are icon-only at every width; the desktop row is checked
    // before the layout shrinks.
    await expectIconOnlyTooltips(page, ".fm-table .fm-actions");
    await expectTooltipOnFocus(
      page,
      rowFor(page, "app.yaml").getByRole("button", {
        name: "Download app.yaml",
      }),
      "Download app.yaml",
    );

    // Select the whole listing so every bulk action renders, then shrink to the
    // icon-only layout: the toolbar and the bulk bar both drop their labels.
    await page.locator(".fm-head .n-checkbox").click();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator(".fm-btn-label").first()).toBeHidden();
    await expectIconOnlyTooltips(page, ".fm-toolbar");
    await expectIconOnlyTooltips(page, ".fm-selection-actions");
    await expectTooltipOnFocus(
      page,
      page.getByRole("button", { name: "Upload", exact: true }),
      "Upload",
    );

    // The grid: tile ⋮ menus and the toolbar-hosted selection bar.
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();
    await expectIconOnlyTooltips(page, ".fm-grid .fm-tile-actions");
    await expectIconOnlyTooltips(page, ".fm-toolbar");
    await expectIconOnlyTooltips(page, ".fm-selection-actions");
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
      await expectTooltipOnHover(page, button, label);
    }
    await expectTooltipOnHover(
      page,
      rowFor(page, "config").getByRole("button", { name: "Mở config", exact: true }),
      "Mở config",
    );

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
        .locator(".files-browser")
        .getByRole("button", { name, exact: true });
      await expect(button, name).toHaveAttribute("aria-label", name);
      await expectTooltipOnHover(page, button, name);
    }
  });
});

// JUS-156 review round 1: the tooltip contract beyond a plain hover — the
// chrome controls (F2), the disabled-reason copy (F1), Escape dismissal and
// hoverability (F3), no over-triggering on focus-return or touch (F4), and the
// assistive-tech exposure (F5).
test.describe("tooltip behaviour (JUS-156 round 1)", () => {
  test("the Files chrome icon-only controls show a tooltip, not a bare title", async ({
    page,
  }) => {
    await openFiles(page);
    await expectTooltipOnHover(
      page,
      page.getByRole("button", { name: "List view" }),
      "List view",
    );
    await expectTooltipOnHover(
      page,
      page.getByRole("button", { name: "Grid view" }),
      "Grid view",
    );
    await expectTooltipOnHover(
      page,
      page.getByRole("button", { name: "New tab" }),
      "New tab",
    );
    await expectTooltipOnHover(
      page,
      page.getByRole("button", { name: "Refresh", exact: true }),
      "Refresh",
    );
    // The parent "up" control is disabled at the root; open a folder first.
    await rowFor(page, "config").locator(".fm-label").dblclick();
    await expectTooltipOnHover(
      page,
      page.getByRole("button", { name: "Go to parent folder" }),
      "Go to parent folder",
    );
  });

  test("no icon-only button in the Files browser relies on a bare title", async ({
    page,
  }) => {
    await openFiles(page);
    // The tooltip is the shared AppTooltip now, so a RENDERED icon-only button
    // must carry an aria-label and must NOT fall back to a native title.
    const scan = (): Promise<
      Array<{ aria: string | null; title: string | null }>
    > =>
      page.locator(".files-browser button").evaluateAll((elements) =>
        elements
          .filter((element) => {
            const el = element as HTMLElement;
            const style = getComputedStyle(el);
            if (style.visibility === "hidden" || style.display === "none") {
              return false;
            }
            if (el.getClientRects().length === 0) {
              return false;
            }
            return el.innerText.trim() === "";
          })
          .map((element) => ({
            aria: element.getAttribute("aria-label"),
            title: element.getAttribute("title"),
          }))
          .filter((button) => button.title !== null || !button.aria),
      );
    expect(await scan()).toEqual([]);

    // Same scan with the toolbar/bulk labels collapsed to icon-only.
    await page.locator(".fm-head .n-checkbox").click();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator(".fm-btn-label").first()).toBeHidden();
    expect(await scan()).toEqual([]);
  });

  test("a blocked row action's tooltip explains why and stays reachable", async ({
    page,
  }) => {
    // A member cannot read content, so a file's Download is blocked.
    await openFiles(page, { role: "member" });
    const download = rowFor(page, "app.yaml").getByRole("button", {
      name: "Download app.yaml",
    });
    await expect(download).toHaveAttribute("aria-disabled", "true");
    await expectTooltipOnHover(
      page,
      download,
      "Content requires the owner or admin role.",
    );
    // Focusable, so a keyboard user reaches the reason too.
    await expectTooltipOnFocus(
      page,
      download,
      "Content requires the owner or admin role.",
    );
  });

  test("a blocked bulk action's tooltip explains why", async ({ page }) => {
    await openFiles(page);
    // Select only the folder: nothing downloadable is selected.
    await rowFor(page, "config").locator(".fm-select .n-checkbox").click();
    const download = page
      .locator(".fm-selection-actions")
      .getByRole("button", { name: "Download", exact: true });
    await expect(download).toHaveAttribute("aria-disabled", "true");
    await expectTooltipOnHover(page, download, "Select one or more entries first.");
  });

  test("Escape dismisses an open tooltip and the tooltip stays hoverable", async ({
    page,
  }) => {
    await openFiles(page);
    const download = rowFor(page, "app.yaml").getByRole("button", {
      name: "Download app.yaml",
    });
    const tooltip = tooltipFor(page, "Download app.yaml");

    // Escape dismisses an open tooltip (WCAG 1.4.13, JUS-156 F3).
    await download.hover();
    await expect(tooltip).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(tooltip).toBeHidden();

    // The pointer can move onto the tooltip without dismissing it (hoverable).
    await page.mouse.move(0, 0);
    await download.hover();
    await expect(tooltip).toBeVisible();
    await tooltip.locator(".app-tooltip-content").hover();
    await expect(tooltip).toBeVisible();
    await page.mouse.move(0, 0);
    await expect(tooltip).toBeHidden();
  });

  test("closing a dialog with Escape does not pop the tooltip on focus return", async ({
    page,
  }) => {
    await openFiles(page);
    const rename = rowFor(page, "config").getByRole("button", {
      name: "Rename config",
    });
    await rename.click();
    // Park the pointer away from the trigger so the pointer's own hover can't
    // be what shows the tooltip — this isolates the focus-return path.
    await page.mouse.move(0, 0);
    await expect(page.getByText("Rename entry")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByText("Rename entry")).toBeHidden();

    // Focus returns to the Rename trigger; a programmatic return is not a
    // keyboard-originated focus, so the tooltip must NOT reopen (JUS-156 F4).
    await expect(rename).toBeFocused();
    await expect(page.locator(".n-tooltip:visible")).toHaveCount(0);
  });

  test("the tooltip is exposed to AT only when it adds copy beyond the name", async ({
    page,
  }) => {
    await openFiles(page, { role: "member" });
    const download = rowFor(page, "app.yaml").getByRole("button", {
      name: "Download app.yaml",
    });
    const described = await download.getAttribute("aria-describedby");
    expect(described, "a reason tooltip is described").toBeTruthy();
    await download.hover();
    const tip = page.locator(`#${described}`);
    await expect(tip).toHaveRole("tooltip");
    await expect(tip).toHaveText("Content requires the owner or admin role.");

    // A tooltip whose text equals the accessible name adds nothing, so the
    // control is not double-announced.
    await expect(moreFor(page, "app.yaml")).not.toHaveAttribute(
      "aria-describedby",
      /.+/,
    );
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

  test("the roving description follows clicks, taps, arrows, Home/End and Shift+click", async ({
    page,
  }) => {
    await openFiles(page);
    // One shared, visually hidden description node — never one per row.
    await expect(page.locator("#fm-row-details")).toHaveCount(1);

    /** rowOrder lists the rendered rows' names in listing order. */
    const rowOrder = (): Promise<string[]> =>
      page
        .locator(".fm-row[data-entry]")
        .evaluateAll((els) =>
          els.map((el) => (el as HTMLElement).dataset.entry ?? ""),
        );
    /** rovingName is the entry that owns the roving tab stop. */
    const rovingName = (): Promise<string> =>
      page
        .locator('.fm-table .fm-label[tabindex="0"]')
        .evaluate(
          (el) =>
            (el.closest("[data-entry]") as HTMLElement | null)?.dataset
              .entry ?? "",
        );

    // The Mode/Modified cells keep their hover tooltips (mouse users).
    await expect(rowFor(page, "app.yaml").locator(".fm-mode")).toHaveAttribute(
      "title",
      /./,
    );

    // A plain click moves the roving row (and the description) to the clicked
    // row — the previously-missing pointer/touch path.
    await rowFor(page, "app.yaml").click();
    await expectDescribes(page, "app.yaml");

    // Arrows keep the description on the focused row.
    await rowFor(page, "app.yaml").locator(".fm-label").focus();
    await page.keyboard.press("ArrowDown");
    await expectDescribes(page, await rovingName());

    // Home / End move the roving row too.
    const order = await rowOrder();
    await page.keyboard.press("Home");
    await expectDescribes(page, order[0]);
    await page.keyboard.press("End");
    await expectDescribes(page, order[order.length - 1]);

    // Shift+click moves the roving row to the clicked entry.
    await rowFor(page, "config").click();
    await rowFor(page, "backup.tar.gz").click({ modifiers: ["Shift"] });
    await expectDescribes(page, "backup.tar.gz");

    // Exactly one tab stop survives every route (JUS-139).
    await expect(page.locator('.fm-label[tabindex="0"]')).toHaveCount(1);

    // The grid tile carries the same single-node description (mode is not a
    // visible cell there), and again only the roving tile is described.
    await page.getByRole("button", { name: "Grid view" }).click();
    const tile = page.locator('.fm-tile[tabindex="0"]');
    await expect(tile).toHaveCount(1);
    await expect(tile).toHaveAttribute("aria-describedby", "fm-row-details");
    await expect(page.locator('.fm-tile[aria-describedby="fm-row-details"]')).toHaveCount(1);
    await expect(tile).toHaveAccessibleDescription(/Mode |Quyền /);
  });

  test("the ⋮ popup and aria-expanded agree on every route", async ({ page }) => {
    await openFiles(page);
    const more = moreFor(page, "app.yaml");

    // Baseline: a real click opens both.
    await more.click();
    await expectMenuOpen(page, "app.yaml");

    // Escape and click-away close both.
    await page.keyboard.press("Escape");
    await expectMenuClosed(page, "app.yaml");
    await more.click();
    await expectMenuOpen(page, "app.yaml");
    await page.locator(".fm-card-title").click();
    await expectMenuClosed(page, "app.yaml");

    // Selecting a menu item closes both.
    await more.click();
    await expectMenuOpen(page, "app.yaml");
    await menuFor(page, "app.yaml")
      .getByRole("menuitem", { name: "Copy", exact: true })
      .click();
    await expectMenuClosed(page, "app.yaml");

    // Filter typing / clearing and a sort change keep the row mounted, so the
    // menu stays open and aria-expanded stays true — the menu is never
    // force-closed while its row is still valid (the two always agree).
    await more.click();
    await expectMenuOpen(page, "app.yaml");
    await page.locator(".fm-search input").fill("app");
    await expectMenuAgrees(page, "app.yaml");
    await expectMenuOpen(page, "app.yaml");
    await page.locator(".fm-search input").fill("");
    await expect(rowFor(page, "app.yaml")).toBeVisible();
    await expectMenuOpen(page, "app.yaml");
    await page.getByRole("button", { name: "Size", exact: true }).focus();
    await page.keyboard.press(" ");
    // The sort really happened (Space activates the header) and the menu stayed.
    await expect(page.locator(".fm-head .fm-cell--size")).toHaveAttribute(
      "aria-sort",
      "ascending",
    );
    await expectMenuOpen(page, "app.yaml");
  });

  test("the ⋮ menu closes when its row leaves the rendered set", async ({ page }) => {
    await openFiles(page);
    await moreFor(page, "app.yaml").click();
    await expectMenuOpen(page, "app.yaml");

    // Filter the row out (it unmounts with its dropdown), then clear the filter:
    // the row renders again with no menu and a collapsed trigger.
    const filter = page.locator(".fm-search input");
    await filter.fill("config");
    await expect(rowFor(page, "app.yaml")).toHaveCount(0);
    await expect(page.getByRole("menu")).toHaveCount(0);
    await filter.fill("");
    await expect(rowFor(page, "app.yaml")).toBeVisible();
    await expectMenuClosed(page, "app.yaml");
  });

  test("a folder change closes the row menu (even when the row name is reused)", async ({
    page,
  }) => {
    await openFiles(page);
    await moreFor(page, "app.yaml").click();
    await expectMenuOpen(page, "app.yaml");

    // Keyboard-open `config`; the mock returns the same listing, so the
    // same-named `app.yaml` node is reused. A folder change is a different
    // directory, so the menu must close (a leftover menu would act on the new
    // file); the trigger and the popup close together.
    await rowFor(page, "config").locator(".fm-label").focus();
    await page.keyboard.press("Enter");
    await expect(rowFor(page, "app.yaml")).toBeVisible();
    await expectMenuClosed(page, "app.yaml");
    await expectMenuAgrees(page, "app.yaml");
  });

  test("a keyboard list↔grid view switch with the menu open closes it", async ({
    page,
  }) => {
    await openFiles(page);
    await moreFor(page, "app.yaml").click();
    await expectMenuOpen(page, "app.yaml");

    // Focus the Grid toggle and activate it with Space: no pointerdown, so the
    // dropdown is not closed by click-away — the row unmounts (and remounts on
    // return) without emitting a close.
    await page.getByRole("button", { name: "Grid view" }).focus();
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-grid")).toBeVisible();

    await page.getByRole("button", { name: "List view" }).focus();
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-table")).toBeVisible();
    await expect(rowFor(page, "app.yaml")).toBeVisible();
    await expectMenuClosed(page, "app.yaml");
    await expectMenuAgrees(page, "app.yaml");
  });

  test("a tab switch closes the row menu", async ({ page }) => {
    await openFiles(page);
    await moreFor(page, "app.yaml").click();
    await expectMenuOpen(page, "app.yaml");
    await page.getByRole("button", { name: "New tab" }).click();
    await expect(moreFor(page, "app.yaml")).toBeVisible();
    await expectMenuClosed(page, "app.yaml");
  });

  test("right-click and Shift+F10 keep the popup and aria in agreement", async ({
    page,
  }) => {
    await openFiles(page);
    // The context route shows the same menu, so the ⋮ reports expanded.
    await rowFor(page, "config").click({ button: "right" });
    await expectMenuOpen(page, "config");
    await page.keyboard.press("Escape");
    await expectMenuClosed(page, "config");

    await rowFor(page, "app.yaml").locator(".fm-label").focus();
    await page.keyboard.press("Shift+F10");
    await expectMenuOpen(page, "app.yaml");
    await page.keyboard.press("Escape");
    await expectMenuClosed(page, "app.yaml");
  });

  test("the context menu closes on a folder change, view switch and tab change", async ({
    page,
  }) => {
    await openFiles(page);

    // (a) folder change, keyboard: the context menu must not survive into the new
    // directory where its Rename/Delete would act on the wrong file.
    await rowFor(page, "app.yaml").click({ button: "right" });
    await expect(menuFor(page, "app.yaml")).toBeVisible();
    await rowFor(page, "config").locator(".fm-label").focus();
    await page.keyboard.press("Enter");
    await expect(rowFor(page, "app.yaml")).toBeVisible();
    await expect(page.getByRole("menu")).toHaveCount(0);

    // (b) list -> grid, keyboard: the list rows (and their context menu) unmount.
    await rowFor(page, "app.yaml").click({ button: "right" });
    await expect(menuFor(page, "app.yaml")).toBeVisible();
    await page.getByRole("button", { name: "Grid view" }).focus();
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-grid")).toBeVisible();
    await expect(page.getByRole("menu")).toHaveCount(0);

    // (c) tab change via the tablist keyboard.
    await page.getByRole("button", { name: "List view" }).focus();
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-table")).toBeVisible();
    await page.getByRole("button", { name: "New tab" }).click();
    await expect(page.getByRole("tab")).toHaveCount(2);
    await rowFor(page, "app.yaml").click({ button: "right" });
    await expect(menuFor(page, "app.yaml")).toBeVisible();
    await page.locator('[role="tab"][aria-selected="true"]').focus();
    await page.keyboard.press("ArrowLeft");
    await expect(page.getByRole("menu")).toHaveCount(0);
  });

  test("the Shift+F10 row menu closes on a folder change", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "app.yaml").locator(".fm-label").focus();
    await page.keyboard.press("Shift+F10");
    await expectMenuOpen(page, "app.yaml");

    await rowFor(page, "config").locator(".fm-label").focus();
    await page.keyboard.press("Enter");
    await expect(rowFor(page, "app.yaml")).toBeVisible();
    await expect(page.getByRole("menu")).toHaveCount(0);
    await expect(moreFor(page, "app.yaml")).toHaveAttribute(
      "aria-expanded",
      "false",
    );
  });

  test("a sort/filter keeps the ⋮ open but closes the context menu", async ({
    page,
  }) => {
    await openFiles(page);

    // The context menu closes on a filter change (its pin cannot survive a
    // reorder), while the ⋮ dropdown below keeps its documented keep-open.
    await rowFor(page, "app.yaml").click({ button: "right" });
    await expect(menuFor(page, "app.yaml")).toBeVisible();
    await page.locator(".fm-search input").fill("app");
    await expect(page.getByRole("menu")).toHaveCount(0);
    await page.locator(".fm-search input").fill("");

    // JUS-146: a right-click selects the row, so the sticky header row becomes
    // the bulk bar and the sort headers are replaced while a selection exists.
    // Closing the menu (Escape) then Clear bring the sort headers back, and a
    // sort change still closes any stale context menu through the same
    // layout-change watcher.
    await rowFor(page, "app.yaml").click({ button: "right" });
    await expect(menuFor(page, "app.yaml")).toBeVisible();
    await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "hidden");
    await page.keyboard.press("Escape");
    await expect(page.getByRole("menu")).toHaveCount(0);
    await page
      .locator(".fm-selection-actions")
      .getByRole("button", { name: "Clear", exact: true })
      .click();
    await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "visible");
    await page.getByRole("button", { name: "Size", exact: true }).focus();
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-head .fm-cell--size")).toHaveAttribute(
      "aria-sort",
      "ascending",
    );
    await expect(page.getByRole("menu")).toHaveCount(0);

    // The ⋮ dropdown stays open (and agrees) across a filter that keeps the row.
    await moreFor(page, "app.yaml").click();
    await expectMenuOpen(page, "app.yaml");
    await page.locator(".fm-search input").fill("app");
    await expectMenuOpen(page, "app.yaml");
  });

  test("deleting the open entry closes the menu and drops the trigger", async ({
    page,
  }) => {
    await openFiles(page);
    let deleted = false;
    await page.route(`**/api/v1/servers/${server1}/files/list*`, (route) => {
      const entries = deleted
        ? listing().filter((entry) => entry.name !== "app.yaml")
        : listing();
      return route.fulfill({ json: { entries, truncated: false, cursor: "" } });
    });
    await page.route(`**/api/v1/servers/${server1}/files/entry*`, (route) => {
      if (route.request().method() === "DELETE") {
        deleted = true;
        return route.fulfill({ status: 204, body: "" });
      }
      return route.fallback();
    });

    await moreFor(page, "app.yaml").click();
    await expectMenuOpen(page, "app.yaml");
    await menuFor(page, "app.yaml")
      .getByRole("menuitem", { name: "Delete", exact: true })
      .click();
    await expectMenuClosed(page, "app.yaml");

    const dialog = page.locator(".n-modal").filter({ hasText: "Delete file" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Move to recycle bin" }).click();
    await expect(moreFor(page, "app.yaml")).toHaveCount(0);
    await expect(page.getByRole("menu")).toHaveCount(0);
  });

  test("a row scrolled out of the window and back leaves no stale ⋮ state", async ({
    page,
  }) => {
    // A long, windowed listing so the open row can be unmounted by scrolling.
    const many = Array.from({ length: 5000 }, (_, index) => ({
      name: `file-${String(index).padStart(4, "0")}.txt`,
      kind: "file",
      size: index + 1,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: `"e${index}"`,
    }));
    await openFiles(page, { entries: many });
    await moreFor(page, "file-0000.txt").click();
    await expectMenuOpen(page, "file-0000.txt");

    // Release the focus pin with no pointer press (the ⋮ button owns focus), so
    // the row can be windowed out; its dropdown unmounts without a close event.
    await page.evaluate(() =>
      (document.activeElement as HTMLElement | null)?.blur(),
    );
    const wrap = page.locator(".table-wrap");
    await wrap.evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
    await expect
      .poll(() => page.locator('.fm-row[data-entry="file-0000.txt"]').count())
      .toBe(0);

    await wrap.evaluate((el) => {
      el.scrollTop = 0;
    });
    await expect(page.locator('.fm-row[data-entry="file-0000.txt"]')).toBeVisible();
    await expectMenuClosed(page, "file-0000.txt");
  });

  test("the header/bar swap never drops focus to <body> (keyboard)", async ({
    page,
  }) => {
    await openFiles(page);

    // Direction 1 (headers → bar): a real Tab reaches the Name sort button;
    // selecting a row (a right-click does not move focus) then replaces the
    // headers with the bar. JUS-146 hands the hidden button's focus to the
    // roving row instead of letting it fall to <body>.
    await page.locator(".fm-head .n-checkbox").focus();
    await page.keyboard.press("Tab");
    await expect(page.locator(".fm-head .fm-sort").first()).toBeFocused();
    await rowFor(page, "app.yaml").click({ button: "right" });
    await page.keyboard.press("Escape");
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);
    await expect(page.locator("body")).not.toBeFocused();
    expect(
      await page.evaluate(() =>
        Boolean(document.activeElement?.closest("[data-entry]")),
      ),
      "focus lands on the roving row, not <body>",
    ).toBe(true);

    // Direction 2 (bar → headers): Enter on the focused Clear control hides the
    // bar and hands focus to the roving row too (JUS-140).
    const clear = page.getByRole("button", { name: "Clear", exact: true });
    await clear.focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(0);
    await expect(page.locator("body")).not.toBeFocused();
    await expect(page.locator('.fm-label[tabindex="0"]')).toBeFocused();
  });

  test("a non-click Clear hands focus from the bar to the roving row", async ({ page }) => {
    await openFiles(page);
    // Select a row so the bulk bar (and its sort control) shows.
    await rowFor(page, "app.yaml").locator(".fm-label").click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);

    // Put focus on the bar's sort control, then clear the selection WITHOUT
    // moving focus (a programmatic click stands in for a keyboard-activated
    // control that keeps focus). JUS-148: focus must land on the roving row, not
    // <body>, even though the sort control is not the Clear button.
    await page.locator(".fm-bulk-sort").focus();
    await expect(page.locator(".fm-bulk-sort")).toBeFocused();
    await page.evaluate(() => {
      const buttons = document.querySelectorAll(
        ".fm-head-bulk .fm-selection-actions button",
      );
      (buttons[buttons.length - 1] as HTMLButtonElement).click();
    });
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(0);
    await expect(page.locator("body")).not.toBeFocused();
    await expect(page.locator('.fm-label[tabindex="0"]')).toBeFocused();
  });

  test("clearing a selection keeps focus off <body> in list and grid", async ({
    page,
  }) => {
    await openFiles(page);

    // LIST Clear (keyboard): the header bar hides and focus must land on the
    // roving row.
    await rowFor(page, "app.yaml").locator(".fm-label").click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);
    await page.getByRole("button", { name: "Clear", exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(0);
    await expect(page.locator("body")).not.toBeFocused();
    await expect(page.locator('.fm-label[tabindex="0"]')).toBeFocused();

    // LIST select-all → uncheck-all: focus stays on the header control.
    const selectAll = page.locator(".fm-head .n-checkbox");
    await selectAll.click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(3);
    await selectAll.click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(0);
    await expect(page.locator("body")).not.toBeFocused();
    expect(
      await page.evaluate(() =>
        Boolean(document.activeElement?.closest(".fm-head")),
      ),
      "focus stays on the header select-all",
    ).toBe(true);

    // GRID Clear: the grid's selection bar lives in the TOOLBAR, not the header
    // row, so the clear-direction hand-off must cover `.fm-selection-actions`
    // there too. Narrowing it to `.fm-head-bulk` dropped grid focus to <body>.
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();
    await page.locator(".fm-tile-wrap").first().click();
    await expect(page.locator(".fm-tile.is-selected")).toHaveCount(1);
    const gridClear = page.getByRole("button", { name: "Clear", exact: true });
    await expect(gridClear).toBeVisible();
    await gridClear.focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".fm-tile.is-selected")).toHaveCount(0);
    await expect(page.locator("body"), "grid Clear keeps focus off <body>").not.toBeFocused();
    expect(
      await page.evaluate(() =>
        Boolean(document.activeElement?.closest("[data-entry]")),
      ),
      "grid Clear hands focus to the roving tile",
    ).toBe(true);
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
      await page.evaluate(() =>
        Boolean(document.activeElement?.closest('[data-entry="app.yaml"]')),
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

  test("a tap moves the row description to the tapped row (JUS-140)", async ({
    page,
  }) => {
    await openFiles(page);
    await rowFor(page, "app.yaml").tap();
    await expectDescribes(page, "app.yaml");
    await expect(page.locator('.fm-label[tabindex="0"]')).toHaveCount(1);
  });

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

  test("a tap never leaves a stuck tooltip (JUS-156 F4)", async ({ page }) => {
    await openFiles(page);
    expect(
      await page.evaluate(() => window.matchMedia("(hover: none)").matches),
    ).toBe(true);

    await moreFor(page, "app.yaml").tap();
    await expect(
      page.getByRole("menu", { name: "More actions for app.yaml" }),
    ).toBeVisible();
    // A tap fires a synthetic mouseenter; it must not open a tooltip that then
    // sticks (there is no mouseleave on a touch device).
    await expect(page.locator(".n-tooltip:visible")).toHaveCount(0);
    await page.keyboard.press("Escape");
    await expect(page.locator(".n-tooltip:visible")).toHaveCount(0);
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
