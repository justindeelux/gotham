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
const stagingId = "33333333-3333-4333-8333-333333333333";
const unknownId = "99999999-9999-4999-8999-999999999999";
const serverId = "33333333-3333-4333-8333-333333333333";
const appId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const previewAppId = "dddddddd-dddd-4ddd-8ddd-dddddddddddd";
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
    {
      id: stagingId,
      project_id: projectId,
      name: "staging",
      created_at: "2026-10-01T00:00:00Z",
      updated_at: "2026-10-01T00:00:00Z",
      resource_counts: { applications: 0, services: 0, databases: 0 },
    },
  ],
};

const previewRow = {
  id: previewAppId,
  name: "storefront-pr-7",
  environment_id: environmentId,
  environment_name: "production",
  project_id: projectId,
  project_name: "storefront",
  provider: "public",
  repo: "medusajs/medusa",
  clone_url: "https://github.com/medusajs/medusa.git",
  branch: "pr-7",
  build_pack: "",
  base_domain: "",
  base_domain_disabled: false,
  port: 3000,
  host_port: 0,
  server_id: serverId,
  server_name: "prod-01",
  is_preview: true,
  preview_of: appId,
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-02T00:00:00Z",
};

/** resourcesFor answers the envelope for one environment, previews on flag. */
function resourcesFor(envId: string, withPreviews: boolean) {
  const environment = detail.environments.find((item) => item.id === envId) ?? detail.environments[0];
  const full = envId === environmentId;
  return {
    environment: { ...environment, project_id: projectId },
    project: projects[0],
    applications: full
      ? [
          { ...resources.applications[0], is_preview: false },
          ...(withPreviews ? [previewRow] : []),
        ]
      : [],
    services: full ? resources.services : [],
    databases: full ? resources.databases : [],
  };
}

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
      is_preview: false,
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
  await page.route("**/api/v1/environments/*/resources*", async (route) => {
    const url = new URL(route.request().url());
    const match = url.pathname.match(/\/environments\/([^/]+)\/resources/);
    const envId = match?.[1] ?? "";
    if (envId === unknownId) {
      await route.fulfill({ status: 404, json: { message: "not found" } });
      return;
    }
    await route.fulfill({
      json: resourcesFor(envId, url.searchParams.get("previews") === "1"),
    });
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

test("preview switch nests previews under their base with a tag", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  await page.locator(".resource-table").waitFor();
  await expect(page.locator(".resource-table")).not.toContainText("storefront-pr-7");
  await page.locator(".preview-switch .n-switch").click();
  const previewRow = page.locator(".resource-table tr.preview-row");
  await expect(previewRow).toContainText("storefront-pr-7");
  await expect(previewRow).toContainText("Preview");
  // Nested directly under the base application row.
  const rows = page.locator(".resource-table tbody tr");
  const names = await rows.evaluateAll((elements) =>
    elements.map((element) => element.querySelector(".resource-name")?.textContent ?? ""),
  );
  expect(names.indexOf("storefront-pr-7")).toBe(names.indexOf("storefront") + 1);
  // Previews stay out of the tab counts.
  await expect(page.locator(".tabs")).toContainText("All");
  const allTab = page.locator(".tabs").getByRole("tab", { name: /All/ });
  await expect(allTab).toContainText("3");
});

test("sibling switcher navigates to staging", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  await page.locator(".resource-table").waitFor();
  await page.locator(".sibling-switch .n-select").click();
  await page.locator(".n-base-select-option").filter({ hasText: "staging" }).click();
  await expect(page).toHaveURL(new RegExp(`/environments/${stagingId}$`));
  await expect(page.locator(".environment-page .title")).toHaveText("staging");
  await expect(page.locator(".environment-page")).toContainText("Nothing here yet");
});

test("unknown environment renders the 404 state, not a retry loop", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${projectId}/environments/${unknownId}`);
  await expect(
    page.getByText("This environment does not exist (or belongs to another team)."),
  ).toBeVisible();
  await expect(page.locator(".environment-page")).not.toContainText("Retry");
});

test("wrong project in the URL replaces it with the canonical one", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${projectId}-wrong/environments/${environmentId}`);
  await page.locator(".resource-table").waitFor();
  // The response ids win over the typed URL.
  await expect(page).toHaveURL(new RegExp(`/projects/${projectId}/environments/${environmentId}$`));
  await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText("storefront");
});

test("add resource dialog shows the picker groups at both widths", async ({
  page,
}) => {
  for (const width of [1280, 480]) {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/projects/${envPath}`);
    await page.locator(".resource-table").waitFor();
    await page.getByRole("button", { name: "Add resource" }).click();
    const dialog = page.locator(".n-modal");
    await expect(dialog.getByRole("heading", { name: "Application" })).toBeVisible();
    await expect(dialog.getByRole("heading", { name: "Service" })).toBeVisible();
    await expect(dialog.getByRole("heading", { name: "Database" })).toBeVisible();
    await expect(dialog).toContainText("storefront / production");
    // The brand cards stay inside the viewport horizontally at phone width.
    const cards = dialog.locator("button.res-card");
    expect(await cards.count()).toBeGreaterThan(1);
    const first = cards.first();
    await expect(first).toBeVisible();
    const box = await first.boundingBox();
    expect(box, "first card has a box").not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width);
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

test("JUS-69 add resource modal pins its header and scrolls only the body", async ({
  page,
}) => {
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 1280, height: 640 },
  ]) {
    await page.setViewportSize(viewport);
    await mockApi(page);
    await page.route("**/api/v1/templates", async (route) => {
      await route.fulfill({ json: { templates: [] } });
    });
    await page.goto(`${baseURL}/projects/${envPath}`);
    await page.locator(".resource-table").waitFor();
    await page.getByRole("button", { name: "Add resource" }).click();
    const dialog = page.locator(".n-modal.app-modal");
    await expect(dialog.getByRole("heading", { name: "Add resource", exact: true })).toBeVisible();
    // The card never exceeds the viewport even with the full picker inside.
    const cardBox = await dialog.boundingBox();
    expect(cardBox, "modal has a box").not.toBeNull();
    expect(cardBox!.y).toBeGreaterThanOrEqual(0);
    expect(cardBox!.y + cardBox!.height).toBeLessThanOrEqual(viewport.height);
    // Only the body scrolls: it overflows while the header stays put.
    const scroller = dialog.locator(".n-card-content");
    expect(await scroller.evaluate((element) => element.scrollHeight)).toBeGreaterThan(
      await scroller.evaluate((element) => element.clientHeight),
    );
    // Wait out the modal enter transition so the header position is settled.
    await expect
      .poll(async () => {
        const first = (await dialog.locator(".n-card-header").boundingBox())!.y;
        await page.waitForTimeout(150);
        return first - (await dialog.locator(".n-card-header").boundingBox())!.y;
      })
      .toBe(0);
    const headerBox = await dialog.locator(".n-card-header").boundingBox();
    await scroller.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    const headerAfter = await dialog.locator(".n-card-header").boundingBox();
    expect(headerAfter!.y).toBeCloseTo(headerBox!.y, 0);
    await page.keyboard.press("Escape");
  }
});

test("JUS-69 add server wizard keeps its footer visible at a 520px height", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 520 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers`);
  await page.locator(".servers-page").waitFor();
  await page.getByRole("button", { name: "Add server" }).first().click();
  const wizard = page.locator(".n-modal.wizard-modal");
  await expect(wizard.getByRole("heading", { name: "Add server", exact: true })).toBeVisible();
  // The 420px floor is gone: the card fits the short viewport instead of
  // clipping the footer, and the step body takes the scroll.
  const cardBox = await wizard.boundingBox();
  expect(cardBox, "wizard card has a box").not.toBeNull();
  expect(cardBox!.y + cardBox!.height).toBeLessThanOrEqual(520);
  for (const name of ["Cancel", "Create & continue"]) {
    const action = wizard.locator(".wizard-foot").getByRole("button", { name });
    await expect(action, `${name} visible`).toBeVisible();
    const box = await action.boundingBox();
    expect(box, `${name} has a box`).not.toBeNull();
    expect(box!.y + box!.height).toBeLessThanOrEqual(520);
  }
});

test("JUS-69 create application modal keeps its footer visible on Dockerfile and Compose steps", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 640 });
  await mockApi(page);
  await page.route("**/api/v1/templates", async (route) => {
    await route.fulfill({ json: { templates: [] } });
  });
  // The picker preselects the source, so each tall editor step opens from
  // its own card instead of switching a type selector in the wizard.
  for (const source of ["Dockerfile", "Docker Compose"]) {
    await page.goto(`${baseURL}/projects/${envPath}`);
    await page.locator(".resource-table").waitFor();
    await page.getByRole("button", { name: "Add resource" }).click();
    const picker = page.locator(".n-modal.app-modal");
    await expect(picker.getByRole("heading", { name: "Application" })).toBeVisible();
    await picker.locator("button.res-card").filter({ hasText: source }).first().click();
    const wizard = page.locator(".n-modal.wizard-modal");
    await expect(wizard.getByRole("heading", { name: "Create application", exact: true })).toBeVisible();
    // The tall editor step must not push the card or its footer out.
    const cardBox = await wizard.boundingBox();
    expect(cardBox, `${source} card has a box`).not.toBeNull();
    expect(cardBox!.y + cardBox!.height).toBeLessThanOrEqual(640);
    const cont = wizard.locator(".wizard-foot").getByRole("button", { name: "Continue" });
    await expect(cont, `${source} footer action visible`).toBeVisible();
    const footBox = await cont.boundingBox();
    expect(footBox, `${source} footer has a box`).not.toBeNull();
    expect(footBox!.y + footBox!.height).toBeLessThanOrEqual(640);
    // The step body is the element that scrolls; the card body itself never does.
    const stepBody = wizard.locator(".wizard-body");
    expect(
      await stepBody.evaluate((element) => element.scrollHeight),
      `${source} step body overflows`,
    ).toBeGreaterThan(await stepBody.evaluate((element) => element.clientHeight));
    const cardContent = wizard.locator(".n-card-content");
    expect(
      await cardContent.evaluate((element) => element.scrollHeight),
      `${source} card body does not scroll`,
    ).toBeLessThanOrEqual(await cardContent.evaluate((element) => element.clientHeight));
    await page.keyboard.press("Escape");
  }
});
