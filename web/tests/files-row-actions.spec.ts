import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, document:readonly, HTMLElement:readonly, window:readonly, HTMLButtonElement:readonly, getComputedStyle:readonly */

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

interface OpenOptions {
  entries?: ReturnType<typeof listing>;
  role?: string;
  locale?: string;
}

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

function rowFor(page: Page, name: string): Locator {
  return page
    .locator(".fm-table .fm-row", { has: page.locator(`b:text-is("${name}")`) })
    .first();
}

function moreFor(page: Page, name: string): Locator {
  return page.getByRole("button", { name: `More actions for ${name}` });
}
function menuFor(page: Page, name: string): Locator {
  return page.getByRole("menu", { name: `More actions for ${name}` });
}

async function expectMenuOpen(page: Page, name: string): Promise<void> {
  await expect(menuFor(page, name)).toBeVisible();
  await expect(moreFor(page, name)).toHaveAttribute("aria-expanded", "true");
}

async function expectMenuClosed(page: Page, name: string): Promise<void> {
  await expect(page.getByRole("menu")).toHaveCount(0);
  await expect(moreFor(page, name)).toHaveAttribute("aria-expanded", "false");
}

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

async function expectDescribes(page: Page, entry: string): Promise<void> {
  const row = page.locator(`.fm-row[data-entry="${entry}"]`);
  await expect(row.locator(".fm-label")).toHaveAttribute(
    "aria-describedby",
    "fm-row-details",
  );
  await expect(
    page.locator('.fm-row .fm-label[aria-describedby="fm-row-details"]'),
  ).toHaveCount(1);
  const mode = await row.locator(".fm-mode").getAttribute("title");
  const date = await row.locator(".fm-date").getAttribute("title");
  await expect(page.locator("#fm-row-details")).toHaveText(
    `Mode ${mode}, modified ${date}`,
  );
}

async function expectNoTooltipOnHover(
  page: Page,
  control: Locator,
  context: string,
): Promise<void> {
  const before = await page.locator(".n-tooltip:visible").count();
  await control.hover();
  await page.waitForTimeout(650);
  const after = await page.locator(".n-tooltip:visible").count();
  expect(after, `no hover tooltip for ${context}`).toBeLessThanOrEqual(before);
  await expect(control, `no native title for ${context}`).not.toHaveAttribute(
    "title",
    /.+/,
  );
  await page.mouse.move(0, 0);
}

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

async function expectNoTooltipOnFocus(
  page: Page,
  control: Locator,
  context: string,
): Promise<void> {
  const before = await page.locator(".n-tooltip:visible").count();
  await focusViaKeyboard(page, control);
  await page.waitForTimeout(650);
  const after = await page.locator(".n-tooltip:visible").count();
  expect(after, `no focus tooltip for ${context}`).toBeLessThanOrEqual(before);
  await page.keyboard.press("Escape");
}

async function expectIconOnlyNames(page: Page, area: string): Promise<void> {
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
    await expectNoTooltipOnHover(page, button, `${area} button ${index}`);
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

    await expect(inline.first()).toHaveCSS("opacity", "0");

    await row.hover();
    await expect(inline.first()).toHaveCSS("opacity", "1");

    await page.locator(".fm-topbar").hover();
    await expect(inline.first()).toHaveCSS("opacity", "0");

    await row.locator(".fm-label").focus();
    await expect(inline.first()).toHaveCSS("opacity", "1");

    await more.focus();
    await expect(inline.first()).toHaveCSS("opacity", "1");

    await more.click();
    await expect(rowMenu).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(rowMenu).toBeHidden();
  });

  test("every icon-only button in the three areas keeps an aria-label and no tooltip", async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await openFiles(page);

    await expectIconOnlyNames(page, ".fm-table .fm-actions");
    await expectNoTooltipOnFocus(
      page,
      rowFor(page, "app.yaml").getByRole("button", {
        name: "Download app.yaml",
      }),
      "row Download",
    );

    await page.locator(".fm-head .n-checkbox").click();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator(".fm-btn-label").first()).toBeHidden();
    await expectIconOnlyNames(page, ".fm-toolbar");
    await expectIconOnlyNames(page, ".fm-selection-actions");
    await expectNoTooltipOnFocus(
      page,
      page.getByRole("button", { name: "Upload", exact: true }),
      "toolbar Upload",
    );

    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator(".fm-grid .fm-tile").first()).toBeVisible();
    await expectIconOnlyNames(page, ".fm-grid .fm-tile-actions");
    await expectIconOnlyNames(page, ".fm-selection-actions");
    await page
      .locator(".fm-grid-bulk .fm-selection-actions")
      .getByRole("button", { name: "Clear", exact: true })
      .click();
    await expect(page.locator(".fm-tile.is-selected")).toHaveCount(0);
    await expectIconOnlyNames(page, ".fm-toolbar .fm-toolbar-scroll");
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
    await openFiles(page, { role: "member" });
    const row = rowFor(page, "app.yaml");
    const download = row.getByRole("button", {
      name: "Download app.yaml",
      exact: true,
    });
    await expect(download).toBeDisabled();

    const dim = await download.evaluate((element) =>
      window
        .getComputedStyle(element)
        .getPropertyValue("--n-opacity-disabled")
        .trim(),
    );
    expect(dim).not.toBe("");
    expect(Number(dim)).toBeLessThan(1);

    await expect(download).toHaveCSS("opacity", "0");

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
      await expectNoTooltipOnHover(page, button, label);
    }
    await expectNoTooltipOnHover(
      page,
      rowFor(page, "config").getByRole("button", { name: "Mở config", exact: true }),
      "Mở config",
    );

    await page.locator(".fm-head .n-checkbox").click();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator(".fm-btn-label").first()).toBeHidden();
    for (const name of [
      "Tạo mới",
      "Tải lên",
      "Thao tác tệp",
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
      await expectNoTooltipOnHover(page, button, name);
    }
  });
});

test.describe("no-tooltip behaviour (JUS-210)", () => {
  test("the Files chrome icon-only controls show no tooltip and no bare title", async ({
    page,
  }) => {
    await openFiles(page);
    await expectNoTooltipOnHover(
      page,
      page.getByRole("button", { name: "List view" }),
      "List view",
    );
    await expectNoTooltipOnHover(
      page,
      page.getByRole("button", { name: "Grid view" }),
      "Grid view",
    );
    await expectNoTooltipOnHover(
      page,
      page.getByRole("button", { name: "New tab" }),
      "New tab",
    );
    await expectNoTooltipOnHover(
      page,
      page.getByRole("button", { name: "Refresh", exact: true }),
      "Refresh",
    );
    await rowFor(page, "config").locator(".fm-label").dblclick();
    await expectNoTooltipOnHover(
      page,
      page.getByRole("button", { name: "Go to parent folder" }),
      "Go to parent folder",
    );
  });

  test("no icon-only button in the Files browser relies on a bare title", async ({
    page,
  }) => {
    await openFiles(page);
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

    await page.locator(".fm-head .n-checkbox").click();
    await page.setViewportSize({ width: 390, height: 844 });
    await expect(page.locator(".fm-btn-label").first()).toBeHidden();
    expect(await scan()).toEqual([]);
  });

  test("a blocked row action keeps its action name and stays reachable", async ({
    page,
  }) => {
    await openFiles(page, { role: "member" });
    const download = rowFor(page, "app.yaml").getByRole("button", {
      name: "Download app.yaml",
    });
    await expect(download).toHaveAttribute("aria-disabled", "true");
    await expect(download).toHaveAttribute("aria-label", "Download app.yaml");
    await expectNoTooltipOnHover(page, download, "blocked row Download");
    await expect(download).not.toHaveAttribute("aria-describedby", /.+/);
    await expectNoTooltipOnFocus(page, download, "blocked row Download");
    await expect(download).toBeFocused();
  });

  test("a blocked bulk action keeps its action name and still looks disabled", async ({
    page,
  }) => {
    await openFiles(page);
    await rowFor(page, "config").locator(".fm-select .n-checkbox").click();
    const download = page
      .locator(".fm-selection-actions")
      .getByRole("button", { name: "Download", exact: true });
    await expect(download).toHaveAttribute("aria-disabled", "true");
    await expect(download).toHaveAttribute("aria-label", "Download");
    await expectNoTooltipOnHover(page, download, "blocked bulk Download");
    expect(
      await download.evaluate((element) =>
        Number(getComputedStyle(element).opacity),
      ),
    ).toBeLessThan(1);
    await download.focus();
    await expect(download).toBeFocused();
  });

  test("the pointer-events scoping leaves other naive tooltips hoverable", async ({
    page,
  }) => {
    await openFiles(page);
    const rail = page.locator(".rail-btn").first();
    await rail.hover();
    const foreign = page
      .locator(".n-tooltip:not(:has(.app-tooltip-content))")
      .last();
    await expect(foreign).toBeVisible();
    expect(
      await foreign.evaluate(
        (element) => getComputedStyle(element).pointerEvents,
      ),
    ).not.toBe("none");

    const download = rowFor(page, "app.yaml").getByRole("button", {
      name: "Download app.yaml",
    });
    await expectNoTooltipOnHover(page, download, "row Download");
  });

  test("a blocked control exposes no reason node and is not double-announced", async ({
    page,
  }) => {
    await openFiles(page, { role: "member" });
    const download = rowFor(page, "app.yaml").getByRole("button", {
      name: "Download app.yaml",
    });
    await expect(download).toHaveAttribute("aria-disabled", "true");
    await expect(download).not.toHaveAttribute("aria-describedby", /.+/);

    await expect(moreFor(page, "app.yaml")).not.toHaveAttribute(
      "aria-describedby",
      /.+/,
    );
  });

  test("closing a dialog with Escape leaves no tooltip on focus return", async ({
    page,
  }) => {
    await openFiles(page);
    const rename = rowFor(page, "config").getByRole("button", {
      name: "Rename config",
    });
    await rename.click();
    await page.mouse.move(0, 0);
    await expect(page.getByText("Rename entry")).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(page.getByText("Rename entry")).toBeHidden();

    await expect(rename).toBeFocused();
    await expect(page.locator(".n-tooltip:visible")).toHaveCount(0);
  });

});

test.describe("row action accessibility nits (JUS-140)", () => {
  test("the row ⋮ exposes its menu state (haspopup + expanded)", async ({
    page,
  }) => {
    await openFiles(page);
    const more = page.getByRole("button", { name: "More actions for app.yaml" });
    const rowMenu = page.getByRole("menu", { name: "More actions for app.yaml" });

    await expect(more).toHaveAttribute("aria-haspopup", "menu");
    await expect(more).toHaveAttribute("aria-expanded", "false");

    await more.click();
    await expect(rowMenu).toBeVisible();
    await expect(more).toHaveAttribute("aria-expanded", "true");

    await page.keyboard.press("Escape");
    await expect(rowMenu).toBeHidden();
    await expect(more).toHaveAttribute("aria-expanded", "false");

    await more.click();
    await expect(more).toHaveAttribute("aria-expanded", "true");
    await page.locator(".fm-topbar").click();
    await expect(rowMenu).toBeHidden();
    await expect(more).toHaveAttribute("aria-expanded", "false");

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
    await expect(page.locator("#fm-row-details")).toHaveCount(1);

    const rowOrder = (): Promise<string[]> =>
      page
        .locator(".fm-row[data-entry]")
        .evaluateAll((els) =>
          els.map((el) => (el as HTMLElement).dataset.entry ?? ""),
        );
    const rovingName = (): Promise<string> =>
      page
        .locator('.fm-table .fm-label[tabindex="0"]')
        .evaluate(
          (el) =>
            (el.closest("[data-entry]") as HTMLElement | null)?.dataset
              .entry ?? "",
        );

    await expect(rowFor(page, "app.yaml").locator(".fm-mode")).toHaveAttribute(
      "title",
      /./,
    );

    await rowFor(page, "app.yaml").click();
    await expectDescribes(page, "app.yaml");

    await rowFor(page, "app.yaml").locator(".fm-label").focus();
    await page.keyboard.press("ArrowDown");
    await expectDescribes(page, await rovingName());

    const order = await rowOrder();
    await page.keyboard.press("Home");
    await expectDescribes(page, order[0]);
    await page.keyboard.press("End");
    await expectDescribes(page, order[order.length - 1]);

    await rowFor(page, "config").click();
    await rowFor(page, "backup.tar.gz").click({ modifiers: ["Shift"] });
    await expectDescribes(page, "backup.tar.gz");

    await expect(page.locator('.fm-label[tabindex="0"]')).toHaveCount(1);

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

    await more.click();
    await expectMenuOpen(page, "app.yaml");

    await page.keyboard.press("Escape");
    await expectMenuClosed(page, "app.yaml");
    await more.click();
    await expectMenuOpen(page, "app.yaml");
    await page.locator(".fm-topbar").click();
    await expectMenuClosed(page, "app.yaml");

    await more.click();
    await expectMenuOpen(page, "app.yaml");
    await menuFor(page, "app.yaml")
      .getByRole("menuitem", { name: "Copy", exact: true })
      .click();
    await expectMenuClosed(page, "app.yaml");

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

    await rowFor(page, "app.yaml").click({ button: "right" });
    await expect(menuFor(page, "app.yaml")).toBeVisible();
    await rowFor(page, "config").locator(".fm-label").focus();
    await page.keyboard.press("Enter");
    await expect(rowFor(page, "app.yaml")).toBeVisible();
    await expect(page.getByRole("menu")).toHaveCount(0);

    await rowFor(page, "app.yaml").click({ button: "right" });
    await expect(menuFor(page, "app.yaml")).toBeVisible();
    await page.getByRole("button", { name: "Grid view" }).focus();
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-grid")).toBeVisible();
    await expect(page.getByRole("menu")).toHaveCount(0);

    await page.getByRole("button", { name: "Clear", exact: true }).focus();
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-tile.is-selected")).toHaveCount(0);
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

    await rowFor(page, "app.yaml").click({ button: "right" });
    await expect(menuFor(page, "app.yaml")).toBeVisible();
    await page.locator(".fm-search input").fill("app");
    await expect(page.getByRole("menu")).toHaveCount(0);
    await page.locator(".fm-search input").fill("");

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

    const clear = page.getByRole("button", { name: "Clear", exact: true });
    await clear.focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(0);
    await expect(page.locator("body")).not.toBeFocused();
    await expect(page.locator('.fm-label[tabindex="0"]')).toBeFocused();
  });

  test("a non-click Clear hands focus from the bar to the roving row", async ({ page }) => {
    await openFiles(page);
    await rowFor(page, "app.yaml").locator(".fm-label").click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);

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

    await rowFor(page, "app.yaml").locator(".fm-label").click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);
    await page.getByRole("button", { name: "Clear", exact: true }).focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(0);
    await expect(page.locator("body")).not.toBeFocused();
    await expect(page.locator('.fm-label[tabindex="0"]')).toBeFocused();

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

    await checkbox.click();
    const clear = page.getByRole("button", { name: "Clear", exact: true });
    await expect(clear).toBeVisible();
    await clear.click();

    await expect(page.locator("body")).not.toBeFocused();
    await expect(page.locator('.fm-label[tabindex="0"]')).toBeFocused();

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

  test("the Paste menu item's accessible name contains its visible label", async ({
    page,
  }) => {
    await openFiles(page);
    await rowFor(page, "app.yaml").locator(".fm-select .n-checkbox").click();
    await page.getByRole("button", { name: "Copy", exact: true }).click();

    await page
      .locator(".fm-toolbar")
      .getByRole("button", { name: "File operations", exact: true })
      .click();
    const menu = page.getByRole("menu", { name: "File operations" });
    const paste = menu.getByRole("menuitem", { name: /^Paste \(1\)/ });
    await expect(paste).toBeVisible();
    await expect(paste).toHaveAccessibleName(/Paste \(1\)/);
  });
});

test.describe("touch devices", () => {
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

  test("a tap opens the menu and leaves no tooltip", async ({ page }) => {
    await openFiles(page);
    expect(
      await page.evaluate(() => window.matchMedia("(hover: none)").matches),
    ).toBe(true);

    await moreFor(page, "app.yaml").tap();
    await expect(
      page.getByRole("menu", { name: "More actions for app.yaml" }),
    ).toBeVisible();
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
