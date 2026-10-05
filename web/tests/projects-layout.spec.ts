import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, getComputedStyle, window:readonly */
// Real-browser proof for the PE-4 projects surface: the committed webdist is
// served statically with the control-plane API mocked, a session is seeded
// in localStorage, and real Chromium measures the real Naive UI DOM. No
// backend is involved. Same harness as profile-layout.spec.ts.

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

const counts = { applications: 3, services: 1, databases: 2 };

const projects = [
  {
    id: "11111111-1111-4111-8111-111111111111",
    name: "storefront",
    description: "Online shop and its backing stores",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-02T00:00:00Z",
    environment_count: 2,
    resource_counts: counts,
  },
  {
    id: "33333333-3333-4333-8333-333333333333",
    name: "internal-tools",
    description: "Docs site, wiki, CI helpers",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    environment_count: 1,
    resource_counts: { applications: 2, services: 2, databases: 0 },
  },
];

const detail = {
  project: projects[0],
  environments: [
    {
      id: "22222222-2222-4222-8222-222222222222",
      project_id: projects[0].id,
      name: "production",
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-01T00:00:00Z",
      resource_counts: counts,
    },
  ],
};

// PE-6: one plain variable plus one write-only secret (value never returned).
const projectVariables = [
  { key: "NODE_ENV", value: "production", secret: false },
  { key: "SENTRY_DSN", secret: true },
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

/** mockApi seeds the session and answers the calls the projects surface makes. */
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
  await page.route("**/api/v1/projects", async (route) => {
    await route.fulfill({ json: { projects } });
  });
  await page.route("**/api/v1/projects/*", async (route) => {
    await route.fulfill({ json: detail });
  });
  await page.route("**/api/v1/projects/*/variables", async (route) => {
    if (route.request().method() === "PUT") {
      await route.fulfill({ json: { variables: projectVariables } });
      return;
    }
    await route.fulfill({ json: { variables: projectVariables } });
  });
}

/** gridColumns counts the rendered grid columns of the project cards. */
async function gridColumns(page: Page): Promise<number> {
  return page.locator(".project-grid").evaluate((grid) => {
    const columns = getComputedStyle(grid).gridTemplateColumns.split(" ");
    return columns.filter((part) => part.length > 0).length;
  });
}

test("projects grid shows three columns at 1280px", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects`);
  await page.locator(".project-grid").waitFor();
  expect(await gridColumns(page)).toBe(3);
  await expect(page.locator(".projects-page h1")).toHaveText("Projects");
  await expect(
    page.getByRole("button", { name: "New project" }).first(),
  ).toBeVisible();
  await expect(page.locator(".project-grid")).toContainText("storefront");
});

test("projects grid collapses to one column at 480px", async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects`);
  await page.locator(".project-grid").waitFor();
  expect(await gridColumns(page)).toBe(1);
  // The search and the create button stay usable at phone width.
  await expect(page.getByPlaceholder("Search projects")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "New project" }).first(),
  ).toBeVisible();
});

test("project detail shows the breadcrumb and environments at both widths", async ({
  page,
}) => {
  for (const width of [1280, 480]) {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/projects/${projects[0].id}`);
    await page.locator(".project-page .title").waitFor();
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText(
      "Projects",
    );
    await expect(page.locator(".project-page .title")).toHaveText("storefront");
    await expect(page.locator(".env-table")).toContainText("production");
    // The shared-variables tab is the PE-6 editor with its precedence hint:
    // plain values edit in place, secrets stay masked and write-only.
    await page.locator(".n-tabs-tab", { hasText: "Shared variables" }).click();
    await expect(page.locator(".project-page")).toContainText(
      "An environment variable overrides a project one",
    );
    // Editor rows are inputs: assert their values, not text content.
    const editor = page.locator(".project-page .variables-card");
    await expect(editor.locator("input").nth(0)).toHaveValue("NODE_ENV");
    await expect(editor.locator("input").nth(2)).toHaveValue("SENTRY_DSN");
    await expect(
      page.getByRole("button", { name: "Add variable" }),
    ).toBeVisible();
  }
});

test("environment actions stay inside the viewport at 480px", async ({
  page,
}) => {
  await page.setViewportSize({ width: 480, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${projects[0].id}`);
  await page.locator(".env-table").waitFor();
  // Narrow rows stack into cards: Rename and Delete need no horizontal
  // scroll to be tapped.
  for (const name of ["Rename", "Delete"]) {
    const button = page
      .locator(".env-table tbody tr", { hasText: "production" })
      .getByRole("button", { name, exact: true });
    await expect(button).toBeVisible();
    const box = await button.boundingBox();
    expect(box, `${name} has a box`).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(480);
  }
});
