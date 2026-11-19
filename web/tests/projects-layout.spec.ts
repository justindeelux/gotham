import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, HTMLElement:readonly, getComputedStyle, window:readonly */

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
  {
    id: "44444444-4444-4444-8444-444444444444",
    name: "analytics-pipeline-with-a-name-long-enough-to-truncate",
    description: "",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    environment_count: 4,
    resource_counts: { applications: 1, services: 1, databases: 1 },
  },
  {
    id: "55555555-5555-4555-8555-555555555555",
    name: "warehouse",
    description:
      "A deliberately long description that runs well past two lines so the card clamps it instead of growing, keeping every card in the grid the same height no matter how much text a project carries.",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    environment_count: 1,
    resource_counts: { applications: 0, services: 0, databases: 2 },
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
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: { user: session.user } });
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
  await expect(
    page.locator(".project-card-link").first(),
    "the projects page keeps environment and resource counts",
  ).toContainText("2 environments");
  await expect(page.locator(".project-card-link").first()).toContainText(
    "6 resources",
  );
});

test("project cards keep one height with and without a description", async ({
  page,
}) => {
  for (const width of [1366, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await page.goto(`${baseURL}/projects`);
    await page.locator(".project-card-link").first().waitFor();

    const cards = await page.locator(".project-card-link").evaluateAll((links) =>
      links.map((link) => {
        const box = link.getBoundingClientRect();
        const name = link.querySelector<HTMLElement>(".project-card__name")!;
        const desc = link.querySelector<HTMLElement>(".project-card__desc")!;
        const stats = link.querySelector<HTMLElement>(".project-card__stats")!;
        return {
          height: box.height,
          right: box.right,
          overflow: link.scrollWidth > link.clientWidth + 1,
          nameHeight: name.getBoundingClientRect().height,
          nameLineHeight: parseFloat(getComputedStyle(name).lineHeight),
          nameEllipsized: name.scrollWidth > name.clientWidth + 1,
          descLines: Math.round(
            desc.getBoundingClientRect().height /
              parseFloat(getComputedStyle(desc).lineHeight),
          ),
          statsHeight: stats.getBoundingClientRect().height,
        };
      }),
    );

    expect(cards, `four cards at ${width}px`).toHaveLength(4);
    expect(
      new Set(cards.map((card) => Math.round(card.height))).size,
      `one card height at ${width}px`,
    ).toBe(1);
    for (const card of cards) {
      expect(card.overflow, `card fits its track at ${width}px`).toBe(false);
      expect(
        card.right,
        `card stays in the viewport at ${width}px`,
      ).toBeLessThanOrEqual(width);
      expect(
        card.nameHeight,
        `the long name stays on one line at ${width}px`,
      ).toBeLessThanOrEqual(card.nameLineHeight + 1);
      expect(
        card.descLines,
        `the description is clamped to two lines at ${width}px`,
      ).toBe(2);
      expect(
        card.statsHeight,
        `the stats row keeps its height at ${width}px`,
      ).toBeGreaterThan(0);
    }
    const longest = cards[2]!;
    expect(longest.nameEllipsized, `the long name truncates at ${width}px`).toBe(true);
  }
});

test("projects grid collapses to one column at 480px", async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects`);
  await page.locator(".project-grid").waitFor();
  expect(await gridColumns(page)).toBe(1);
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
    await page.locator(".n-tabs-tab", { hasText: "Shared variables" }).click();
    await expect(page.locator(".project-page")).toContainText(
      "An environment variable overrides a project one",
    );
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
