import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, window:readonly */
// Real-browser proof for the PE-5 environment surface: the committed webdist
// is served statically with the control-plane API mocked, a session is seeded
// in localStorage, and real Chromium measures the real Naive UI DOM. No
// backend is involved. Same harness as projects-layout.spec.ts.

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

const projectId = "11111111-1111-4111-8111-111111111111";
const environmentId = "22222222-2222-4222-8222-222222222222";
const serverId = "33333333-3333-4333-8333-333333333333";
const appId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const serviceId = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const databaseId = "cccccccc-cccc-4ccc-8ccc-cccccccccccc";

const projects = [
  {
    id: projectId,
    name: "storefront",
    description: "Online shop and its backing stores",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-02T00:00:00Z",
    environment_count: 1,
    resource_counts: { applications: 1, services: 1, databases: 1 },
  },
];

const detail = {
  project: projects[0],
  environments: [
    {
      id: environmentId,
      project_id: projectId,
      name: "production",
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-01T00:00:00Z",
      resource_counts: { applications: 1, services: 1, databases: 1 },
    },
  ],
};

const resources = {
  environment: detail.environments[0],
  project: projects[0],
  applications: [
    {
      id: appId,
      name: "storefront",
      environment_id: environmentId,
      environment_name: "production",
      project_id: projectId,
      project_name: "storefront",
      provider: "public",
      repo: "medusajs/medusa",
      clone_url: "https://github.com/medusajs/medusa.git",
      branch: "main",
      build_pack: "",
      base_domain: "",
      base_domain_disabled: false,
      port: 3000,
      host_port: 0,
      server_id: serverId,
      server_name: "prod-01",
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-02T00:00:00Z",
    },
  ],
  services: [
    {
      id: serviceId,
      name: "plausible",
      status: "running",
      server_id: serverId,
      server_name: "prod-01",
      environment_id: environmentId,
      environment_name: "production",
      project_id: projectId,
      project_name: "storefront",
      compose_project: "gotham-bbbbbbbb",
      env: {},
      domains: [{ service: "web", domain: "stats.example.com", port: 8000 }],
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-02T00:00:00Z",
    },
  ],
  databases: [
    {
      id: databaseId,
      name: "pg-orders",
      environment_id: environmentId,
      environment_name: "production",
      project_id: projectId,
      project_name: "storefront",
      engine: "postgres",
      version: "16-alpine",
      status: "running",
      server_id: serverId,
      server_name: "prod-01",
      public_port: 0,
      volume: "gotham-db-cccccccc",
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-02T00:00:00Z",
    },
  ],
};

const servers = [
  {
    id: serverId,
    name: "prod-01",
    ip: "10.0.0.1",
    status: "ready",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  },
];

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

const envPath = `${projectId}/environments/${environmentId}`;

/** mockApi seeds the session and answers the calls the environment page makes. */
async function mockApi(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
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
    await route.fulfill({ json: { servers } });
  });
  await page.route("**/api/v1/projects", async (route) => {
    await route.fulfill({ json: { projects } });
  });
  await page.route("**/api/v1/projects/*", async (route) => {
    await route.fulfill({ json: detail });
  });
  await page.route("**/api/v1/environments/*/resources", async (route) => {
    await route.fulfill({ json: resources });
  });
  await page.route("**/api/v1/applications/*/deployments*", async (route) => {
    await route.fulfill({
      json: {
        deployments: [
          {
            id: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
            application_id: appId,
            kind: "deploy",
            state: "running",
            attempt: 1,
            created_at: "2026-10-02T00:00:00Z",
            updated_at: "2026-10-02T00:00:00Z",
          },
        ],
      },
    });
  });
}

test("environment page shows breadcrumb, tabs and nested rows at 1280px", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  await page.locator(".environment-page .title").waitFor();
  await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText("Projects");
  await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText("production");
  await expect(page.locator(".environment-page .title")).toHaveText("production");
  // Type tabs carry live counts.
  await expect(page.locator(".tabs")).toContainText("All");
  await expect(page.locator(".tabs")).toContainText("Applications");
  // Every kind renders one row with its detail.
  await expect(page.locator(".resource-table")).toContainText("storefront");
  await expect(page.locator(".resource-table")).toContainText("plausible");
  await expect(page.locator(".resource-table")).toContainText("pg-orders");
  // Rows link to the nested detail pages, not the removed flat routes.
  for (const id of [appId, serviceId, databaseId]) {
    const link = page.locator(`.resource-table a[href$="/${id}"]`);
    await expect(link.first()).toBeVisible();
  }
  await expect(page.getByRole("button", { name: "Add resource" })).toBeVisible();
});

test("type tabs filter the unified table", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  await page.locator(".resource-table").waitFor();
  await page.locator(".tabs").getByRole("tab", { name: /Services/ }).click();
  await expect(page.locator(".resource-table")).toContainText("plausible");
  await expect(page.locator(".resource-table")).not.toContainText("pg-orders");
  await expect(page.locator(".resource-table")).not.toContainText("medusajs");
  await page.locator(".tabs").getByRole("tab", { name: /Databases/ }).click();
  await expect(page.locator(".resource-table")).toContainText("pg-orders");
  await expect(page.locator(".resource-table")).not.toContainText("plausible");
});

test("preview switch refetches with previews=1", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  await page.locator(".resource-table").waitFor();
  const previewRequest = page.waitForRequest((request) =>
    request.url().includes("/resources?previews=1"),
  );
  await page.locator(".preview-switch .n-switch").click();
  await previewRequest;
});

test("add resource dialog lists the three kinds at both widths", async ({
  page,
}) => {
  for (const width of [1280, 480]) {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/projects/${envPath}`);
    await page.locator(".resource-table").waitFor();
    await page.getByRole("button", { name: "Add resource" }).click();
    const dialog = page.locator(".n-modal");
    await expect(dialog).toContainText("Application");
    await expect(dialog).toContainText("Service");
    await expect(dialog).toContainText("Database");
    await expect(dialog).toContainText("storefront / production");
    // The kind cards stay inside the viewport at phone width.
    for (const kind of ["Application", "Service", "Database"]) {
      const card = dialog.locator(".kind-card", { hasText: kind });
      await expect(card).toBeVisible();
      const box = await card.boundingBox();
      expect(box, `${kind} has a box`).not.toBeNull();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(width);
    }
    await page.keyboard.press("Escape");
  }
});

test("resource rows stay inside the viewport at 480px", async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  await page.locator(".resource-table").waitFor();
  // Narrow rows stack into cards: every Open needs no horizontal scroll.
  const opens = page.locator(".resource-table tbody tr a");
  expect(await opens.count()).toBe(3);
  for (let index = 0; index < 3; index += 1) {
    const box = await opens.nth(index).boundingBox();
    expect(box, `row ${index} has a box`).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(480);
  }
});
