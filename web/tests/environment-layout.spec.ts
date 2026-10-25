import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, window, getComputedStyle:readonly */

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
    role: "member",
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
  source_type: "git_private",
  branch: "pr-7",
  build_pack: "",
  primary_domain: "",
  server_id: serverId,
  server_name: "prod-01",
  is_preview: true,
  preview_of: appId,
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-02T00:00:00Z",
};

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
      source_type: "github_app",
      branch: "main",
      build_pack: "",
      primary_domain: "",
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
  await expect(page.locator(".tabs")).toContainText("All");
  await expect(page.locator(".tabs")).toContainText("Applications");
  await expect(page.locator(".resource-table")).toContainText("storefront");
  await expect(page.locator(".resource-table")).toContainText("plausible");
  await expect(page.locator(".resource-table")).toContainText("pg-orders");
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

test("application rows show the source brand avatar", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  await page.locator(".resource-table").waitFor();
  const appRow = page
    .locator(".resource-table tbody tr.resource-row")
    .filter({ hasText: "storefront" })
    .first();
  const icon = appRow.locator(".resource-cell .brand-icon");
  await expect(icon).toBeVisible();
  const box = await icon.boundingBox();
  expect(box, "avatar renders a box").not.toBeNull();
  expect(box!.width).toBe(28);
  expect(box!.height).toBe(28);
  const body = await appRow.locator(".resource-cell .resource-body").boundingBox();
  expect(body, "name block renders a box").not.toBeNull();
  expect(box!.x + box!.width).toBeLessThanOrEqual(body!.x);
  const nameBox = await appRow.locator(".resource-body .resource-name").boundingBox();
  const subBox = await appRow.locator(".resource-body .cell-sub").boundingBox();
  expect(nameBox, "name renders a box").not.toBeNull();
  expect(subBox, "summary renders a box").not.toBeNull();
  expect(nameBox!.y + nameBox!.height).toBeLessThanOrEqual(subBox!.y);
  expect(
    await icon.evaluate((node) => getComputedStyle(node).backgroundColor),
  ).toBe("rgb(110, 118, 129)");
  await expect(icon.locator(".brand-icon__glyph")).toHaveCount(1);
  await expect(icon).toHaveAttribute("aria-hidden", "true");
  await expect(
    page.locator(".resource-table tbody tr", { hasText: "plausible" }).locator(".brand-icon"),
  ).toHaveCount(0);
  await expect(
    page.locator(".resource-table tbody tr", { hasText: "pg-orders" }).locator(".brand-icon"),
  ).toHaveCount(0);
  await page.locator(".preview-switch .n-switch").click();
  await expect(
    page.locator(".resource-table tr.preview-row .brand-icon"),
  ).toHaveCSS("background-color", "rgb(240, 80, 50)");
});

for (const locale of ["en", "vi"] as const) {
  test(`long application names truncate right of the icon (${locale})`, async ({
    page,
  }) => {
    const longName = "storefront-checkout-with-a-very-long-application-name";
    await page.setViewportSize({ width: 1280, height: 900 });
    await mockApi(page);
    await page.addInitScript((value) => {
      window.localStorage.setItem("gotham-locale", value);
    }, locale);
    await page.route("**/api/v1/environments/*/resources*", async (route) => {
      const json = resourcesFor(environmentId, false);
      json.applications = [{ ...json.applications[0], name: longName }];
      await route.fulfill({ json });
    });
    await page.goto(`${baseURL}/projects/${envPath}`);
    await page.locator(".resource-table").waitFor();
    await expect(page.locator(".resource-table thead")).toContainText(
      locale === "en" ? "Name" : "Tên",
    );
    const row = page.locator(".resource-table tbody tr.resource-row").first();
    await expect(row.locator(".resource-name")).toHaveText(longName);
    const iconBox = await row.locator(".resource-cell .brand-icon").boundingBox();
    const name = row.locator(".resource-body .resource-name");
    const nameBox = await name.boundingBox();
    expect(iconBox, "icon renders a box").not.toBeNull();
    expect(nameBox, "name renders a box").not.toBeNull();
    expect(iconBox!.x + iconBox!.width).toBeLessThanOrEqual(nameBox!.x);
    const [scrollWidth, clientWidth] = await name.evaluate((element) => [
      element.scrollWidth,
      element.clientWidth,
    ]);
    expect(scrollWidth, "long name truncates").toBeGreaterThan(clientWidth);
    const rowBox = await row.boundingBox();
    expect(rowBox, "row renders a box").not.toBeNull();
    expect(rowBox!.x + rowBox!.width).toBeLessThanOrEqual(1280);
  });
}

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
  const rows = page.locator(".resource-table tbody tr");
  const names = await rows.evaluateAll((elements) =>
    elements.map((element) => element.querySelector(".resource-name")?.textContent ?? ""),
  );
  expect(names.indexOf("storefront-pr-7")).toBe(names.indexOf("storefront") + 1);
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
    await expect(dialog.getByRole("tab", { name: "Application", exact: true })).toBeVisible();
    await expect(dialog.getByRole("tab", { name: "Service", exact: true })).toBeVisible();
    await expect(dialog.getByRole("tab", { name: "Database", exact: true })).toBeVisible();
    await expect(dialog).toContainText("storefront / production");
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
  const rows = page.locator(".resource-table tbody tr.resource-row");
  expect(await rows.count()).toBe(3);
  for (let index = 0; index < 3; index += 1) {
    const box = await rows.nth(index).boundingBox();
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
      await route.fulfill({ json: { templates: Array.from({ length: 24 }, (_, index) => ({
        slug: `service-${index}`, name: `Service ${index}`, description: "A service template.",
      })) } });
    });
    await page.goto(`${baseURL}/projects/${envPath}`);
    await page.locator(".resource-table").waitFor();
    await page.getByRole("button", { name: "Add resource" }).click();
    const dialog = page.locator(".n-modal.app-modal");
    await expect(dialog.getByRole("heading", { name: "Add resource", exact: true })).toBeVisible();
    await dialog.getByRole("tab", { name: "Service", exact: true }).click();
    await expect(dialog).toHaveCSS("transform", "none");
    const cardBox = await dialog.boundingBox();
    expect(cardBox, "modal has a box").not.toBeNull();
    expect(cardBox!.y).toBeGreaterThanOrEqual(0);
    expect(cardBox!.y + cardBox!.height).toBeLessThanOrEqual(viewport.height);
    const scroller = dialog.locator(".res-pane");
    expect(await scroller.evaluate((element) => element.scrollHeight)).toBeGreaterThan(
      await scroller.evaluate((element) => element.clientHeight),
    );
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
  for (const source of ["Dockerfile", "Docker Compose"]) {
    await page.goto(`${baseURL}/projects/${envPath}`);
    await page.locator(".resource-table").waitFor();
    await page.getByRole("button", { name: "Add resource" }).click();
    const picker = page.locator(".n-modal.app-modal");
    await expect(picker.getByRole("tab", { name: "Application", exact: true })).toBeVisible();
    await picker.locator("button.res-card").filter({ hasText: source }).first().click();
    const wizard = page.locator(".n-modal.wizard-modal");
    await expect(wizard.getByRole("heading", { name: "Create application", exact: true })).toBeVisible();
    const cardBox = await wizard.boundingBox();
    expect(cardBox, `${source} card has a box`).not.toBeNull();
    expect(cardBox!.y + cardBox!.height).toBeLessThanOrEqual(640);
    const cont = wizard.locator(".wizard-foot").getByRole("button", { name: "Continue" });
    await expect(cont, `${source} footer action visible`).toBeVisible();
    const footBox = await cont.boundingBox();
    expect(footBox, `${source} footer has a box`).not.toBeNull();
    expect(footBox!.y + footBox!.height).toBeLessThanOrEqual(640);
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
