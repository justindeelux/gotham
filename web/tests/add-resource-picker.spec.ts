import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, window:readonly */
// Real-browser proof for the JUS-71/72/76 fix round 2: every picker tile
// renders an explicit, pairwise distinct brand mark, and opening a wizard
// hides the picker modal (no card bleeds through behind the wizard) until
// the wizard closes. Same static-webdist + mocked-API harness as
// environment-layout.spec.ts; assertions measure the rendered DOM
// (bounding boxes, visibility), never source strings.

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

const projects = [
  {
    id: projectId,
    name: "storefront",
    description: "Online shop and its backing stores",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-02T00:00:00Z",
    environment_count: 1,
    resource_counts: { applications: 0, services: 0, databases: 0 },
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
      updated_at: "2026-10-02T00:00:00Z",
      resource_counts: { applications: 0, services: 0, databases: 0 },
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
    updated_at: "2026-10-02T00:00:00Z",
  },
];

const templates = [
  {
    slug: "wordpress",
    name: "WordPress",
    icon: "wordpress",
    description: "WordPress with a MySQL database.",
  },
  {
    slug: "nextcloud",
    name: "Nextcloud",
    icon: "nextcloud",
    description: "Nextcloud with PostgreSQL and Redis.",
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

/** mockApi seeds the session and answers the calls the picker makes. */
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
  await page.route("**/api/v1/environments/*/resources*", async (route) => {
    await route.fulfill({
      json: {
        environment: { ...detail.environments[0], project_id: projectId },
        project: projects[0],
        applications: [],
        services: [],
        databases: [],
      },
    });
  });
  await page.route("**/api/v1/templates", async (route) => {
    await route.fulfill({ json: { templates } });
  });
  await page.route("**/api/v1/providers", async (route) => {
    await route.fulfill({ json: { providers: [] } });
  });
  await page.route("**/api/v1/providers/github-app", async (route) => {
    await route.fulfill({ json: { apps: [] } });
  });
}

/** openPicker loads the environment page and opens the Add resource modal. */
async function openPicker(page: Page) {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  // Empty resources render an Add action both in the header and the empty
  // state; either opens the same picker modal.
  await page.getByRole("button", { name: "Add resource" }).first().click();
  const picker = page.locator(".n-modal.app-modal");
  await expect(picker.getByRole("heading", { name: "Application" })).toBeVisible();
  return picker;
}

test("picker tiles render pairwise distinct brand marks", async ({ page }) => {
  const picker = await openPicker(page);
  // 7 source cards + 2 template cards + 5 engine cards.
  const marks = picker.locator("button.res-card .brand-icon text");
  expect(await marks.count()).toBe(14);
  const texts: string[] = [];
  for (let index = 0; index < 14; index += 1) {
    const mark = marks.nth(index);
    const box = await mark.boundingBox();
    expect(box, `mark ${index} renders a box`).not.toBeNull();
    expect(box!.width, `mark ${index} has width`).toBeGreaterThan(0);
    texts.push(((await mark.textContent()) ?? "").trim());
  }
  expect(texts.every((text) => text.length > 0)).toBe(true);
  expect(new Set(texts).size).toBe(texts.length);
});

test("an open wizard hides the picker until Change closes it", async ({ page }) => {
  const picker = await openPicker(page);
  const cards = picker.locator("button.res-card");
  await expect(cards.first()).toBeVisible();
  await picker.locator("button.res-card", { hasText: "Dockerfile" }).click();
  const wizard = page.locator(".n-modal.wizard-modal");
  await expect(
    wizard.getByRole("heading", { name: "Create application", exact: true }),
  ).toBeVisible();
  // The picker modal leaves the layout: no visible card can intersect
  // the wizard box. toBeHidden retries through the modal leave transition.
  await expect(picker).toBeHidden();
  await expect(cards.first()).toBeHidden();
  const wizardBox = await wizard.boundingBox();
  expect(wizardBox, "wizard has a box").not.toBeNull();
  // Change closes the wizard and restores the mounted picker.
  await wizard.locator(".preselected button").click();
  await expect(wizard).toBeHidden();
  await expect(cards.first()).toBeVisible();
});
