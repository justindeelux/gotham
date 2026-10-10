import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { mkdir } from "node:fs/promises";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, document, window:readonly */
// JUS-88 follow-up layout proof: hints render below their fields, the
// certificate table cells stack with a gap, the sidebar footer keeps
// left-aligned content with a right-aligned chevron, and the sidebar and
// language menus breathe. Every claim is a measured box on the committed
// webdist served statically with the API mocked; element screenshots land in
// .playwright-mcp/ (gitignored) for human review.

const testsDir = dirname(fileURLToPath(import.meta.url));
const webdistRoot = resolve(testsDir, "../../internal/server/webdist");
const shotsDir = resolve(testsDir, "../../.playwright-mcp");

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
    display_name: "Ada Lovelace",
    has_password: true,
    is_platform_admin: false,
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

const apps = [
  { id: "app-1", name: "Shop", base_domain: "shop.example.com" },
  { id: "app-2", name: "Blog", base_domain: "blog.example.com" },
];

const providers = [
  {
    id: "prov-1",
    provider: "cloudflare",
    name: "Production Cloudflare",
    zones: ["example.com"],
    enabled: true,
    credentials_set: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
  },
];

const certificates = [
  {
    id: "cert-1",
    application_id: "app-1",
    domain: "shop.example.com",
    enabled: true,
    challenge: "http-01",
    dns_provider_id: "",
    wildcard: false,
    status: "present",
    not_after: new Date(Date.now() + 45 * 24 * 60 * 60 * 1000).toISOString(),
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
  },
  {
    id: "cert-2",
    application_id: "app-2",
    domain: "blog.example.com",
    enabled: true,
    challenge: "dns-01",
    dns_provider_id: "prov-1",
    wildcard: true,
    status: "unknown",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
  },
];

const redirects = [
  {
    id: "redir-1",
    application_id: "app-1",
    source_domain: "go.example.com",
    target_domain: "shop.example.com",
    code: 302,
    preserve_path: true,
    enabled: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
  },
];

function json(status: number, body: unknown) {
  return { status, contentType: "application/json", body: JSON.stringify(body) };
}

let server: Server;
let baseURL = "";

test.beforeAll(async () => {
  await mkdir(shotsDir, { recursive: true });
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

/** mockApi seeds the session and answers every call the Domains page makes. */
async function mockApi(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const method = request.method();
    const path = request.url().split("/api/v1")[1]!.split("?")[0]!;
    if (method === "GET" && path === "/auth/me") {
      await route.fulfill(json(200, session.user));
      return;
    }
    if (method === "GET" && path === "/auth/config") {
      await route.fulfill(json(200, { registrationOpen: false }));
      return;
    }
    if (method === "GET" && path === "/teams") {
      await route.fulfill(json(200, { teams: [team] }));
      return;
    }
    if (method === "GET" && path === "/version") {
      await route.fulfill(json(200, { version: "v0.2.1-dev" }));
      return;
    }
    if (method === "GET" && path === "/applications") {
      await route.fulfill(json(200, { applications: apps }));
      return;
    }
    if (method === "GET" && path === "/servers") {
      await route.fulfill(json(200, { servers: [] }));
      return;
    }
    if (method === "GET" && path === "/proxy/dns-providers") {
      await route.fulfill(json(200, { providers }));
      return;
    }
    if (method === "GET" && path === "/proxy/certificates") {
      await route.fulfill(json(200, { certificates }));
      return;
    }
    if (method === "GET" && path === "/proxy/redirects") {
      await route.fulfill(json(200, { redirects }));
      return;
    }
    await route.fulfill(json(404, { message: `unmocked ${method} ${path}` }));
  });
}

/** openCertModal opens the Add certificate dialog from the Domains page. */
async function openCertModal(page: Page): Promise<Locator> {
  await page.getByRole("button", { name: "Add certificate" }).first().click();
  const dialog = page.locator(".n-modal.n-card").filter({ hasText: "Add certificate" }).first();
  await expect(dialog).toBeVisible();
  await expect
    .poll(async () => (await dialog.boundingBox())?.width ?? 0, { timeout: 5000 })
    .toBeGreaterThan(0);
  return dialog;
}

/** boxOf returns the bounding box, failing loudly when the element has none. */
async function boxOf(locator: Locator, label: string) {
  const box = await locator.boundingBox();
  expect(box, `${label} has a box`).not.toBeNull();
  return box!;
}

for (const width of [1280, 480]) {
  test(`certificate hint sits below its field at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/domains`);
    await expect(page.getByRole("heading", { name: "Domains & SSL", level: 1 })).toBeVisible();
    const dialog = await openCertModal(page);
    await dialog.screenshot({ path: resolve(shotsDir, `jus88-cert-modal-${width}.png`) });
    const field = dialog.locator(".field-provider-select");
    const hint = field.getByText("The shared HTTP-01 resolver needs no provider.");
    await expect(hint).toBeVisible();
    const control = field.locator(".n-select");
    const controlBox = await boxOf(control, "provider control");
    const hintBox = await boxOf(hint, "provider hint");
    expect(hintBox.y, "hint starts below the control").toBeGreaterThanOrEqual(
      controlBox.y + controlBox.height - 1,
    );
    expect(hintBox.x, "hint starts under the field, not beside it").toBeLessThan(
      controlBox.x + controlBox.width - 50,
    );
  });

  test(`certificate table cells stack with a gap at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/domains`);
    const row = page.getByRole("row").filter({ hasText: "shop.example.com" });
    await expect(row).toHaveCount(1);
    await row.screenshot({ path: resolve(shotsDir, `jus88-cert-row-${width}.png`) });
    const domainCell = row.locator("td").first();
    const name = domainCell.locator("span").nth(0);
    const sub = domainCell.locator("span").nth(1);
    const nameBox = await boxOf(name, "domain name");
    const subBox = await boxOf(sub, "domain sub");
    expect(subBox.y, "sub line starts below the name line").toBeGreaterThanOrEqual(
      nameBox.y + nameBox.height - 1,
    );
    expect(
      subBox.y - (nameBox.y + nameBox.height),
      "name/sub vertical gap",
    ).toBeGreaterThanOrEqual(2);
  });

  test(`sidebar footer keeps text left with the chevron right at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/domains`);
    if (width <= 1024) {
      await page.getByRole("button", { name: "Toggle navigation" }).click();
    }
    const foot = page.locator(".sidebar-foot");
    await expect(foot).toBeVisible();
    await foot.screenshot({ path: resolve(shotsDir, `jus88-sidebar-foot-${width}.png`) });
    const button = foot.locator("button.me-card");
    const buttonBox = await boxOf(button, "footer button");
    const meta = button.locator(".me-meta");
    const metaBox = await boxOf(meta, "footer text");
    expect(metaBox.x, "footer text hugs the left").toBeLessThanOrEqual(buttonBox.x + 56);
    const chevron = button.locator(".me-chevron");
    const chevronBox = await boxOf(chevron, "footer chevron");
    expect(
      buttonBox.x + buttonBox.width - (chevronBox.x + chevronBox.width),
      "chevron hugs the right edge",
    ).toBeLessThanOrEqual(16);
    expect(chevronBox.x, "chevron sits right of the text").toBeGreaterThanOrEqual(
      metaBox.x + metaBox.width,
    );
  });

  test(`sidebar nav items and language options breathe at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/domains`);
    await expect(page.getByRole("heading", { name: "Domains & SSL", level: 1 })).toBeVisible();
    if (width <= 1024) {
      await page.getByRole("button", { name: "Toggle navigation" }).click();
    }
    const items = page.locator("#app-nav .nav-item");
    await expect.poll(async () => items.count(), { timeout: 5000 }).toBeGreaterThan(2);
    await page.locator("#app-nav").screenshot({ path: resolve(shotsDir, `jus88-sidebar-${width}.png`) });
    const first = await boxOf(items.nth(0), "first nav item");
    const second = await boxOf(items.nth(1), "second nav item");
    expect(second.y - (first.y + first.height), "nav item gap").toBeGreaterThanOrEqual(4);
    if (width <= 1024) {
      await page.keyboard.press("Escape");
    }
    await page.getByRole("button", { name: "Language" }).click();
    const menu = page.locator(".n-dropdown-menu").first();
    await expect(menu).toBeVisible();
    await menu.screenshot({ path: resolve(shotsDir, `jus88-language-menu-${width}.png`) });
    const options = menu.locator(".n-dropdown-option");
    expect(await options.count()).toBeGreaterThanOrEqual(2);
    const firstOption = await boxOf(options.nth(0), "first language option");
    const secondOption = await boxOf(options.nth(1), "second language option");
    expect(
      secondOption.y - (firstOption.y + firstOption.height),
      "language option gap",
    ).toBeGreaterThanOrEqual(4);
  });

  test(`provider modal pairs fields without squeezing at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/domains`);
    await page.locator(".n-tabs-tab").filter({ hasText: "DNS providers" }).click();
    await page.getByRole("button", { name: "Add provider" }).first().click();
    const dialog = page.locator(".n-modal.n-card").filter({ hasText: "Add DNS provider" }).first();
    await expect(dialog).toBeVisible();
    await expect
      .poll(async () => (await dialog.boundingBox())?.width ?? 0, { timeout: 5000 })
      .toBeGreaterThan(0);
    await page.waitForTimeout(300);
    await dialog.screenshot({ path: resolve(shotsDir, `jus88-provider-modal-${width}.png`) });
    const overflow = await dialog.evaluate((element) => ({
      scrollWidth: element.scrollWidth,
      clientWidth: element.clientWidth,
    }));
    expect(
      overflow.scrollWidth,
      `provider modal inner overflow at ${width}`,
    ).toBeLessThanOrEqual(overflow.clientWidth + 1);
  });

  test(`redirects tab fits at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/domains`);
    const redirectsTab = page.locator(".n-tabs-tab").filter({ hasText: "Redirects" });
    await redirectsTab.click();
    await expect(redirectsTab).toHaveClass(/n-tabs-tab--active/);
    await expect(page.getByText("go.example.com")).toBeVisible();
    const card = page.locator(".n-card").filter({ hasText: "Add redirect" }).first();
    await expect(card).toBeVisible();
    await card.screenshot({ path: resolve(shotsDir, `jus88-redirects-${width}.png`) });
    const overflow = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
    }));
    expect(
      overflow.scrollWidth,
      `redirects sideways scroll at ${width}`,
    ).toBeLessThanOrEqual(overflow.clientWidth + 1);
  });
}
