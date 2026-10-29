import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

import { waitForLayoutSettled } from "./support";

/* global URL, document, getComputedStyle, window:readonly */

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

const now = new Date().toISOString();
const databaseId = "11111111-1111-1111-1111-111111111111";
const applicationId = "22222222-2222-2222-2222-222222222222";
const serviceId = "33333333-3333-3333-3333-333333333333";
const projectId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const environmentId = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const providerId = "44444444-4444-4444-4444-444444444444";
const localProviderId = "55555555-5555-5555-5555-555555555555";

function session(role: "owner" | "member") {
  return {
    user: {
      id: "u-1",
      email: "ada@gotham.dev",
      created_at: "2026-03-04T12:00:00Z",
      display_name: "Ada",
      has_password: true,
      role,
    },
    accessToken: "test-access",
    refreshToken: "test-refresh",
  };
}

const database = {
  id: databaseId,
  name: "shop-db",
  environment_id: environmentId,
  environment_name: "production",
  project_id: projectId,
  project_name: "shop",
  engine: "postgres",
  version: "16",
  status: "running",
  server_id: "99999999-9999-9999-9999-999999999999",
  server_name: "node",
  container_id: "container-1",
  public_port: 5432,
  volume: "volume-1",
  created_at: now,
  updated_at: now,
};

const application = {
  id: applicationId,
  name: "shop-web",
  environment_id: environmentId,
  environment_name: "production",
  project_id: projectId,
  project_name: "shop",
  provider: "github",
  repo: "acme/shop",
  clone_url: "https://github.com/acme/shop.git",
  source_type: "github_app",
  github_app_id: "",
  branch: "main",
  build_pack: "nixpacks",
  image_ref: "",
  has_registry_credential: false,
  primary_domain: "shop.example.com",
  server_id: database.server_id,
  server_name: "node",
  pending_changes: [],
  created_at: now,
  updated_at: now,
};

const service = {
  id: serviceId,
  name: "observability",
  status: "running",
  server_id: database.server_id,
  server_name: "node",
  environment_id: environmentId,
  environment_name: "production",
  project_id: projectId,
  project_name: "shop",
  compose_project: "observability",
  template: "uptime-kuma",
  env: {},
  domains: [],
  created_at: now,
  updated_at: now,
};

const backups = [
  {
    id: "66666666-6666-6666-6666-666666666666",
    resource_type: "database",
    resource_id: databaseId,
    provider_id: providerId,
    provider_name: "Offsite",
    type: "scheduled",
    status: "completed",
    size: 4096,
    location: "s3://backups/shop-db.dump",
    created_at: now,
    finished_at: now,
  },
  {
    id: "77777777-7777-7777-7777-777777777777",
    resource_type: "application",
    resource_id: applicationId,
    provider_id: localProviderId,
    provider_name: "Local disk",
    type: "manual",
    status: "failed",
    size: 0,
    error: "archive failed",
    created_at: now,
    finished_at: now,
  },
  {
    id: "88888888-8888-8888-8888-888888888888",
    resource_type: "service",
    resource_id: serviceId,
    provider_id: localProviderId,
    provider_name: "Local disk",
    type: "manual",
    status: "running",
    size: 0,
    created_at: now,
  },
];

const schedules = [
  {
    id: "99999999-9999-9999-9999-999999999999",
    resource_type: "database",
    resource_id: databaseId,
    cron: "0 2 * * *",
    provider_id: providerId,
    enabled: true,
    next_run_at: new Date(Date.now() + 3600_000).toISOString(),
    max_run_seconds: 7200,
    created_at: now,
    updated_at: now,
  },
];

const providers = [
  {
    id: localProviderId,
    name: "Local disk",
    kind: "local",
    builtin: true,
    has_credentials: false,
    created_at: now,
    updated_at: now,
  },
  {
    id: providerId,
    name: "Offsite",
    kind: "s3",
    endpoint: "https://s3.example.test",
    region: "auto",
    bucket: "backups",
    prefix: "logs/",
    builtin: false,
    has_credentials: true,
    created_at: now,
    updated_at: now,
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
  baseURL = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

test.afterAll(async () => {
  await new Promise<void>((done) => {
    server.close(() => done());
  });
});

async function openBackups(
  page: Page,
  role: "owner" | "member" = "owner",
): Promise<void> {
  const state = session(role);
  await page.addInitScript((value) => {
    document.defaultView?.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(state));
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: { user: state.user } });
  });
  await page.route("**/api/v1/version", async (route) => {
    await route.fulfill({ json: { version: "v0.2.1-dev" } });
  });
  await page.route(/\/api\/v1\/backups(\?.*)?$/, async (route) => {
    await route.fulfill({ json: { backups } });
  });
  await page.route(/\/api\/v1\/backups\/schedules(\?.*)?$/, async (route) => {
    await route.fulfill({ json: { schedules } });
  });
  await page.route("**/api/v1/backups/providers", async (route) => {
    await route.fulfill({ json: { providers } });
  });
  await page.route(/\/api\/v1\/databases(\?.*)?$/, async (route) => {
    await route.fulfill({ json: { databases: [database] } });
  });
  await page.route(/\/api\/v1\/applications(\?.*)?$/, async (route) => {
    await route.fulfill({ json: { applications: [application] } });
  });
  await page.route(/\/api\/v1\/services(\?.*)?$/, async (route) => {
    await route.fulfill({ json: { services: [service] } });
  });
  await page.goto(`${baseURL}/backups`);
  await page.getByRole("heading", { name: "Backups", level: 1 }).waitFor();
  await waitForLayoutSettled(page);
}

test("lists every resource type with a working link", async ({ page }) => {
  await openBackups(page);
  await expect(page.locator(".backups-table tbody tr")).toHaveCount(3);
  await expect(page.locator(".backups-table")).toContainText("shop-db");
  await expect(page.locator(".backups-table")).toContainText("shop-web");
  await expect(page.locator(".backups-table")).toContainText("observability");
  await expect(
    page.locator(
      `.backups-table a[href="/projects/${projectId}/environments/${environmentId}/databases/${databaseId}"]`,
    ),
  ).toBeVisible();
  await expect(page.locator(".backups-table")).toContainText("Database");
  await expect(page.locator(".backups-table")).toContainText("Application");
  await expect(page.locator(".backups-table")).toContainText("Service");
});

test("filters runs by resource type and status", async ({ page }) => {
  await openBackups(page);
  await page.locator(".filters .n-select").nth(0).click();
  await page
    .locator(".n-base-select-option")
    .filter({ hasText: "Application" })
    .click();
  await expect(page.locator(".backups-table tbody tr")).toHaveCount(1);
  await expect(page.locator(".backups-table")).toContainText("shop-web");

  await page.locator(".filters .n-select").nth(1).click();
  await page.locator(".n-base-select-option").filter({ hasText: "failed" }).click();
  await expect(page.locator(".backups-table tbody tr")).toHaveCount(1);
  await expect(page.locator(".backups-table")).toContainText("failed");
});

test("keeps the table at 1366px without overflow", async ({ page }) => {
  await page.setViewportSize({ width: 1366, height: 900 });
  await openBackups(page);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);
  const display = await page
    .locator(".backups-table tbody tr")
    .first()
    .evaluate((row) => getComputedStyle(row).display);
  expect(display).not.toBe("block");
});

test("stacks runs as cards without overflow at 390px", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openBackups(page);
  await expect(page.locator(".backups-table tbody tr")).toHaveCount(3);
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow).toBeLessThanOrEqual(1);
  const display = await page
    .locator(".backups-table tbody tr")
    .first()
    .evaluate((row) => getComputedStyle(row).display);
  expect(display).toBe("block");
});

test("hides storage editing from a member", async ({ page }) => {
  await openBackups(page, "member");
  await expect(
    page.locator(".n-tabs-tab").filter({ hasText: "Storage" }),
  ).toHaveCount(0);
  await page.goto(`${baseURL}/backups?tab=storage`);
  await page.getByRole("heading", { name: "Backups", level: 1 }).waitFor();
  await expect(
    page.locator(".backups-table tbody tr"),
  ).toHaveCount(3);
  await expect(page.getByText("Storage providers")).toHaveCount(0);
});

test("keeps the provider prefix clearable and keys write-only", async ({
  page,
}) => {
  await openBackups(page);
  await page.locator(".n-tabs-tab").filter({ hasText: "Storage" }).click();
  const row = page.locator(".backup-row").filter({ hasText: "Offsite" });
  await row.getByRole("button", { name: "Edit" }).click();

  const prefix = page.getByLabel("Prefix").locator("input");
  await expect(prefix).toHaveValue("logs/");

  const patch = page.waitForRequest(
    (request) =>
      request.url().includes(`/api/v1/backups/providers/${providerId}`) &&
      request.method() === "PATCH",
  );
  await page.route(`**/api/v1/backups/providers/${providerId}`, async (route) => {
    await route.fulfill({ json: { provider: { ...providers[1], prefix: "" } } });
  });
  await prefix.fill("");
  await page.getByRole("button", { name: "Save" }).click();

  const body = (await patch).postDataJSON() as Record<string, unknown>;
  expect(body["prefix"]).toBe("");
  expect(body).not.toHaveProperty("access_key");
  expect(body).not.toHaveProperty("secret_key");
});
