import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { mkdir } from "node:fs/promises";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, document, window:readonly */
// JUS-89 layout proof: the multi-domain editor on the application detail
// page renders the primary card unchanged plus the aliases card, alias rows
// never overlap, the add hint sits below its field, row actions stay
// right-aligned, and rows keep the 8px token gap. Every claim is a measured
// box on the committed webdist served statically with the API mocked;
// element screenshots land in .playwright-mcp/ (gitignored) for human
// review.

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
  base_domain: "shop.example.com",
  base_domain_disabled: false,
  port: 3000,
  host_port: 0,
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
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  },
  {
    id: "dom-2",
    application_id: "app-1",
    domain: "www.shop.example.com",
    is_primary: false,
    disabled: false,
    created_at: "2026-01-02T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
  },
  {
    id: "dom-3",
    application_id: "app-1",
    domain: "staging.shop.example.com",
    is_primary: false,
    disabled: false,
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

/** mockApi seeds the session and answers every call the detail page makes. */
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
    if (method === "GET" && path === "/applications/app-1") {
      await route.fulfill(json(200, { application }));
      return;
    }
    if (method === "GET" && path === "/applications/app-1/domains") {
      await route.fulfill(json(200, { domains }));
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

/** openDomainsTab renders the domain editor of the seeded application. */
async function openDomainsTab(page: Page): Promise<Locator> {
  await page.goto(`${baseURL}/projects/proj-1/environments/env-1/applications/app-1`);
  const tab = page.locator(".n-tabs-tab").filter({ hasText: "Domains" });
  await tab.click();
  const card = page.locator(".n-card").filter({ hasText: "Additional domains" }).first();
  await expect(card).toBeVisible();
  return card;
}

/** boxOf returns the bounding box, failing loudly when the element has none. */
async function boxOf(locator: Locator, label: string) {
  const box = await locator.boundingBox();
  expect(box, `${label} has a box`).not.toBeNull();
  return box!;
}

for (const width of [1280, 480]) {
  test(`alias rows stack without overlap at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    const card = await openDomainsTab(page);
    await card.screenshot({ path: resolve(shotsDir, `jus89-aliases-${width}.png`) });
    const rows = card.locator(".alias-row");
    await expect(rows).toHaveCount(2);
    const first = await boxOf(rows.nth(0), "first alias row");
    const second = await boxOf(rows.nth(1), "second alias row");
    // Rows stack vertically with the 8px token gap (sub-pixel tolerance).
    expect(second.y, "second row below first").toBeGreaterThanOrEqual(first.y + first.height + 8 - 0.5);
    expect(first.x, "rows share the left edge").toBeLessThanOrEqual(second.x + 0.5);
  });

  test(`alias actions stay right-aligned at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    const card = await openDomainsTab(page);
    const rows = card.locator(".alias-row");
    await expect(rows).toHaveCount(2);
    for (const index of [0, 1]) {
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

  test(`add hint sits below its field at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    const card = await openDomainsTab(page);
    const input = card.getByPlaceholder("www.example.com");
    await expect(input).toBeVisible();
    const note = card.getByText(
      "Removing the primary promotes the oldest remaining domain.",
    );
    await expect(note).toBeVisible();
    const inputBox = await boxOf(input, "alias input");
    const noteBox = await boxOf(note, "alias note");
    expect(noteBox.y, "note below the add field").toBeGreaterThanOrEqual(inputBox.y + inputBox.height - 0.5);
  });

  test(`primary editor keeps its single-domain shape at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/projects/proj-1/environments/env-1/applications/app-1`);
    await page.locator(".n-tabs-tab").filter({ hasText: "Domains" }).click();
    const primary = page.locator(".n-card").filter({ hasText: "Application domain" }).first();
    await expect(primary.locator("input").first()).toHaveValue("shop.example.com");
    await expect(primary.getByRole("button", { name: "Save domain" })).toBeVisible();
  });
}
