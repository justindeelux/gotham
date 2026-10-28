import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { mkdir } from "node:fs/promises";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, document:readonly, window:readonly */

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
    role: "member",
  },
  accessToken: "test-access",
  refreshToken: "test-refresh",
};

const application = {
  id: "app-1",
  name: "Shop",
  environment_id: "env-1",
  environment_name: "production",
  project_id: "proj-1",
  project_name: "Acme",
  provider: "github",
  repo: "acme/shop",
  clone_url: "https://github.com/acme/shop.git",
  source_type: "git_public",
  github_app_id: "",
  branch: "main",
  build_pack: "dockerfile",
  image_ref: "",
  has_registry_credential: false,
  primary_domain: "shop.example.com",
  server_id: "node-1",
  server_name: "gotham-prod-01",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

const domains = [
  {
    id: "dom-1",
    application_id: "app-1",
    domain: "shop.example.com",
    is_primary: true,
    disabled: false,
    port: 3000,
    service: "",
    protocol: "http",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    id: "dom-2",
    application_id: "app-1",
    domain: "www.shop.example.com",
    is_primary: false,
    disabled: false,
    port: 8080,
    service: "",
    protocol: "https",
    created_at: "2026-01-02T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
  },
  {
    id: "dom-3",
    application_id: "app-1",
    domain: "staging.shop.example.com",
    is_primary: false,
    disabled: false,
    port: 8080,
    service: "",
    protocol: "https",
    created_at: "2026-01-03T00:00:00Z",
    updated_at: "2026-01-03T00:00:00Z",
  },
];

const certificates = [
  {
    id: "cert-1",
    application_id: "app-1",
    domain: "shop.example.com",
    enabled: true,
    challenge: "http-01",
    wildcard: false,
    status: "present",
    not_after: new Date(Date.now() + 45 * 24 * 60 * 60 * 1000).toISOString(),
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
    if (method === "GET" && path === "/version") {
      await route.fulfill(json(200, { version: "v0.2.1-dev" }));
      return;
    }
    if (method === "GET" && path === "/applications/app-1") {
      await route.fulfill(json(200, { application }));
      return;
    }
    if (method === "GET" && path === "/applications/app-1/domains") {
      await route.fulfill(json(200, { domains }));
      return;
    }
    const monitorMatch = path.match(
      /^\/applications\/app-1\/domains\/([^/]+)\/monitoring$/,
    );
    if (method === "GET" && monitorMatch) {
      const domain = domains.find((row) => row.id === monitorMatch[1]);
      const points = Array.from({ length: 6 }, (_, index) => ({
        at: new Date(Date.now() - (6 - index) * 600_000).toISOString(),
        outcome: index === 3 ? "failed" : "healthy",
        covered_seconds: 600,
      }));
      await route.fulfill(
        json(200, {
          website_check: {
            domain_id: domain?.id ?? "dom-1",
            application_id: "app-1",
            url: `https://${domain?.domain ?? "shop.example.com"}/`,
            probe_location: "control_plane",
            enabled: true,
            path: "/",
            expected_min: 200,
            expected_max: 399,
            timeout_seconds: 5,
            status: "healthy",
            coverage: "fresh",
            last_checked_at: new Date().toISOString(),
            last_status: 200,
            last_latency_ms: 42,
            windows: [
              {
                key: "24h",
                range_seconds: 86400,
                observed_seconds: 80000,
                healthy_seconds: 79900,
                failed_seconds: 100,
                coverage: 0.93,
                availability: 0.998,
                samples: 6,
                average_latency_ms: 42,
                insufficient_coverage: false,
                points,
              },
              {
                key: "7d",
                range_seconds: 604800,
                observed_seconds: 20000,
                healthy_seconds: 19900,
                failed_seconds: 100,
                coverage: 0.03,
                samples: 6,
                insufficient_coverage: true,
                points,
              },
            ],
          },
        }),
      );
      return;
    }
    if (method === "GET" && path === "/applications/app-1/deployments") {
      await route.fulfill(json(200, { deployments: [] }));
      return;
    }
    if (method === "GET" && path === "/applications/app-1/env") {
      await route.fulfill(json(200, { env: [] }));
      return;
    }
    if (method === "GET" && path === "/applications/app-1/storages") {
      await route.fulfill(json(200, { storage: [] }));
      return;
    }
    if (method === "GET" && path === "/proxy/dns-providers") {
      await route.fulfill(json(200, { providers: [] }));
      return;
    }
    if (method === "GET" && path === "/proxy/certificates") {
      await route.fulfill(json(200, { certificates }));
      return;
    }
    if (method === "GET" && path === "/projects/proj-1") {
      await route.fulfill(json(200, { project: { id: "proj-1", name: "Acme" } }));
      return;
    }
    if (path.includes("/previews")) {
      await route.fulfill(json(200, { previews: [] }));
      return;
    }
    await route.fulfill(json(200, {}));
  });
}

async function openDomainsTab(page: Page): Promise<Locator> {
  await page.goto(`${baseURL}/projects/proj-1/environments/env-1/applications/app-1`);
  const tab = page.locator(".n-tabs-tab").filter({ hasText: "Domains" });
  await tab.click();
  const card = page
    .locator(".n-card")
    .filter({
      has: page.locator(".n-card-header__main", { hasText: "Domains" }),
    })
    .first();
  await expect(card).toBeVisible();
  return card;
}

async function boxOf(locator: Locator, label: string) {
  const box = await locator.boundingBox();
  expect(box, `${label} has a box`).not.toBeNull();
  return box!;
}

async function openWebsiteCard(page: Page): Promise<Locator> {
  const card = page
    .locator(".n-card")
    .filter({
      has: page.locator(".n-card-header__main", { hasText: "Website checks" }),
    })
    .first();
  await expect(card).toBeVisible();
  return card;
}

for (const width of [390, 480, 1280, 1366]) {
  test(`table rows stack without overlap at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    const card = await openDomainsTab(page);
    await card.screenshot({ path: resolve(shotsDir, `jus236-domains-${width}.png`) });
    const rows = card.locator(".domain-row");
    await expect(rows).toHaveCount(3);
    const first = await boxOf(rows.nth(0), "first domain row");
    const second = await boxOf(rows.nth(1), "second domain row");
    expect(second.y, "second row below first").toBeGreaterThanOrEqual(first.y + first.height - 0.5);
    expect(first.x, "rows share the left edge").toBeLessThanOrEqual(second.x + 0.5);
  });

  test(`row actions stay inside their row at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    const card = await openDomainsTab(page);
    const rows = card.locator(".domain-row");
    await expect(rows).toHaveCount(3);
    for (const index of [0, 1, 2]) {
      const row = await boxOf(rows.nth(index), `row ${index}`);
      const actions = await boxOf(
        rows.nth(index).locator(".domain-actions"),
        `row ${index} actions`,
      );
      expect(actions.x + actions.width, `row ${index} actions flush right`).toBeLessThanOrEqual(
        row.x + row.width + 0.5,
      );
      expect(actions.x, `row ${index} actions inside the row`).toBeGreaterThanOrEqual(row.x - 0.5);
    }
  });

  test(`the add modal exposes host, port, service and protocol at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    const card = await openDomainsTab(page);
    await card.locator(".domain-add").click();
    const modal = page.locator(".domain-form-modal");
    await expect(modal).toBeVisible();
    const host = modal.locator('input[aria-label="Domain hostname"]');
    const port = modal.locator('input[aria-label="Target container port"]');
    await expect(host).toBeVisible();
    await expect(port).toBeVisible();
    await expect(modal.locator('.domain-protocol')).toBeVisible();
    expect(await boxOf(host, "host field")).not.toBeNull();
    expect(await boxOf(port, "port field")).not.toBeNull();
  });

  test(`the table scrolls inside the card instead of overflowing the page at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    const card = await openDomainsTab(page);
    await expect(card.locator(".domain-table")).toBeVisible();
    const fitsViewport = await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth + 1,
    );
    expect(fitsViewport, "page does not scroll horizontally").toBe(true);
    const cardBox = await boxOf(card, "domains card");
    const tableBox = await boxOf(card.locator(".domain-table"), "domains table");
    expect(tableBox.x + tableBox.width, "table stays inside the card").toBeLessThanOrEqual(
      cardBox.x + cardBox.width + 1,
    );
  });

  test(`website checks fit the page at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await openDomainsTab(page);
    const card = await openWebsiteCard(page);
    await expect(card.locator(".check-row")).toHaveCount(3);
    await expect(card.locator(".spark").first()).toBeVisible();
    const fitsViewport = await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth + 1,
    );
    expect(fitsViewport, "page does not scroll horizontally").toBe(true);
  });

  test(`rows expose their routing target at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    const card = await openDomainsTab(page);
    const primary = card.locator(".domain-row").filter({ hasText: "shop.example.com" }).first();
    await expect(primary).toBeVisible();
    await expect(primary).toContainText("3000");
    await expect(primary).toContainText("http");
    await expect(
      primary.getByRole("button", { name: "Edit domain shop.example.com" }),
    ).toBeVisible();
    await expect(
      primary.getByRole("button", { name: "Certificate for shop.example.com" }),
    ).toBeVisible();
  });
}

test("website check settings modal exposes every control", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await openDomainsTab(page);
  const card = await openWebsiteCard(page);
  await card.getByRole("button", { name: "Configure" }).first().click();
  const modal = page.locator(".website-check-modal");
  await expect(modal).toBeVisible();
  await expect(modal.getByText("Lowest expected status")).toBeVisible();
  await expect(modal.getByText("Highest expected status")).toBeVisible();
  await expect(modal.getByText("Timeout (seconds)")).toBeVisible();
  await expect(modal.getByText("Path")).toBeVisible();
  const box = await modal.boundingBox();
  const viewport = page.viewportSize()!;
  expect(box!.height).toBeLessThanOrEqual(viewport.height);
});
