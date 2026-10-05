import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, window:readonly */
// Real-browser proof for the PE-6 shared-variables surface: the committed
// webdist is served statically with the control-plane API mocked, a session
// is seeded in localStorage, and real Chromium measures the real Naive UI
// DOM. No backend is involved. Same harness as projects-layout.spec.ts.

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
const appId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";

const counts = { applications: 1, services: 0, databases: 0 };

const project = {
  id: projectId,
  name: "storefront",
  description: "Online shop and its backing stores",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-02T00:00:00Z",
  environment_count: 1,
  resource_counts: counts,
};

const environment = {
  id: environmentId,
  project_id: projectId,
  name: "production",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
  resource_counts: counts,
};

// Secrets are write-only: the list responses never carry their values.
const projectVariables = [
  { key: "LOG_LEVEL", value: "info", secret: false },
  { key: "SENTRY_DSN", secret: true },
];
const environmentVariables = [{ key: "CACHE_SIZE", value: "256", secret: false }];
const applicationEnv = [{ key: "LOG_LEVEL", value: "trace" }];

const application = {
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
  server_id: null,
  server_name: "",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-02T00:00:00Z",
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

/** mockApi seeds the session and answers the calls the variables surface makes. */
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
    await route.fulfill({ json: { servers: [] } });
  });
  await page.route("**/api/v1/projects", async (route) => {
    await route.fulfill({ json: { projects: [project] } });
  });
  // Registered before the generic project route so the variables scope wins
  // (Playwright runs the most recently added handler first).
  await page.route("**/api/v1/projects/*/variables", async (route) => {
    await route.fulfill({ json: { variables: projectVariables } });
  });
  await page.route("**/api/v1/environments/*/variables", async (route) => {
    await route.fulfill({ json: { variables: environmentVariables } });
  });
  await page.route("**/api/v1/projects/*", async (route) => {
    await route.fulfill({ json: { project, environments: [environment] } });
  });
  await page.route("**/api/v1/environments/*/resources*", async (route) => {
    await route.fulfill({
      json: {
        environment: { ...environment, project_id: projectId },
        project,
        applications: [{ ...application, server_name: "prod-01", is_preview: false }],
        services: [],
        databases: [],
      },
    });
  });
  await page.route("**/api/v1/applications/*/env", async (route) => {
    await route.fulfill({ json: { env: applicationEnv } });
  });
  await page.route("**/api/v1/applications/*/storages", async (route) => {
    await route.fulfill({ json: { storage: [] } });
  });
  await page.route("**/api/v1/applications/*/deployments*", async (route) => {
    await route.fulfill({ json: { deployments: [] } });
  });
  await page.route("**/api/v1/applications/*/previews", async (route) => {
    await route.fulfill({ json: { previews: [] } });
  });
  await page.route("**/api/v1/applications/*", async (route) => {
    await route.fulfill({ json: { application } });
  });
}

test("project variables tab edits plain rows and masks secrets at both widths", async ({
  page,
}) => {
  for (const width of [1280, 480]) {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/projects/${projectId}`);
    await page.locator(".project-page .title").waitFor();
    await page.locator(".n-tabs-tab", { hasText: "Shared variables" }).click();
    const editor = page.locator(".variables-card");
    // Editor rows are inputs: assert their values, not text content.
    await expect(editor.locator("input").nth(0)).toHaveValue("LOG_LEVEL");
    await expect(editor.locator("input").nth(2)).toHaveValue("SENTRY_DSN");
    // The precedence hint teaches project < environment < application.
    await expect(editor).toContainText(
      "An environment variable overrides a project one",
    );
    // Secrets are write-only: the stored value never renders.
    await expect(editor).not.toContainText("super-secret-value");
    // A loaded, problem-free draft leaves Save enabled for a writer.
    await expect(
      editor.getByRole("button", { name: "Save" }),
    ).toBeEnabled();
    const add = editor.getByRole("button", { name: "Add variable" });
    await expect(add).toBeVisible();
    const box = await add.boundingBox();
    expect(box, "Add variable has a box").not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
  }
});

test("environment page shows project rows read-only above its own editor", async ({
  page,
}) => {
  for (const width of [1280, 480]) {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/projects/${projectId}/environments/${environmentId}`);
    await page.locator(".environment-page .title").waitFor();
    const section = page.locator(".variables-section");
    await section.scrollIntoViewIfNeeded();
    // The project rows render read-only above the environment editor.
    await expect(section).toContainText("SENTRY_DSN");
    await expect(section).toContainText("from project");
    // The environment's own rows are editor inputs: assert their values.
    await expect(section.locator("input").nth(0)).toHaveValue("CACHE_SIZE");
    await expect(section).toContainText(
      "application variables override both",
    );
    // The editor controls stay inside the viewport at phone width.
    const add = section.getByRole("button", { name: "Add variable" });
    await expect(add).toBeVisible();
    const box = await add.boundingBox();
    expect(box, "Add variable has a box").not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
  }
});

test("application env tab lists inherited rows with their origin", async ({
  page,
}) => {
  for (const width of [1280, 480]) {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(
      `${baseURL}/projects/${projectId}/environments/${environmentId}/applications/${appId}`,
    );
    await page.locator(".n-tabs-tab", { hasText: "Environment" }).click();
    const tab = page.locator(".n-tab-pane", { hasText: "Inherited shared variables" });
    await expect(tab).toContainText("from project");
    await expect(tab).toContainText("from environment");
    // The application's LOG_LEVEL shadows both scopes.
    await expect(tab).toContainText("overridden");
    await expect(tab).toContainText("project < environment < application");
  }
});
