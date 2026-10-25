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
  {
    slug: "n8n",
    name: "n8n",
    icon: "n8n",
    description: "n8n workflow automation.",
  },
  {
    slug: "uptime-kuma",
    name: "Uptime Kuma",
    icon: "uptime-kuma",
    description: "Uptime Kuma monitoring.",
  },
  {
    slug: "mystery",
    name: "Mystery",
    icon: "mystery",
    description: "A catalog entry without a brand glyph.",
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

async function openPicker(page: Page) {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  await page.getByRole("button", { name: "Add resource" }).first().click();
  const picker = page.locator(".n-modal.app-modal");
  await expect(picker.getByRole("tab", { name: "Application", exact: true })).toBeVisible();
  return picker;
}

test("picker tiles render the approved monochrome brand glyphs", async ({ page }) => {
  const picker = await openPicker(page);
  const expected: Record<string, { colors: string[]; glyphs: number }> = {
    Application: {
      colors: [
        "rgb(240, 80, 50)",
        "rgb(240, 80, 50)",
        "rgb(110, 118, 129)",
        "rgb(252, 109, 38)",
        "rgb(36, 150, 237)",
        "rgb(36, 150, 237)",
        "rgb(36, 150, 237)",
      ],
      glyphs: 7,
    },
    Service: {
      colors: [
        "rgb(33, 117, 155)",
        "rgb(0, 130, 201)",
        "rgb(234, 75, 113)",
        "rgb(54, 155, 107)",
        "rgb(88, 101, 242)",
      ],
      glyphs: 4,
    },
    Database: {
      colors: [
        "rgb(65, 105, 225)",
        "rgb(68, 121, 161)",
        "rgb(192, 118, 90)",
        "rgb(71, 162, 72)",
        "rgb(255, 68, 56)",
      ],
      glyphs: 5,
    },
  };
  for (const [tab, { colors, glyphs }] of Object.entries(expected)) {
    await picker.getByRole("tab", { name: tab, exact: true }).click();
    const cards = picker.locator("button.res-card");
    const frames = cards.locator(".brand-icon");
    await expect(frames).toHaveCount(colors.length);
    expect(await frames.evaluateAll((nodes) =>
      nodes.map((node) => getComputedStyle(node).backgroundColor),
    )).toEqual(colors);
    const glyphNodes = cards.locator(".brand-icon__glyph");
    await expect(glyphNodes).toHaveCount(glyphs);
    for (const glyph of await glyphNodes.all()) {
      const box = await glyph.boundingBox();
      const frame = await glyph.locator("xpath=..").boundingBox();
      expect(box, "glyph renders a box").not.toBeNull();
      expect(frame, "frame renders a box").not.toBeNull();
      expect(box!.width / frame!.width).toBeGreaterThan(0.55);
      expect(box!.width / frame!.width).toBeLessThan(0.65);
      expect(box!.x - frame!.x).toBeCloseTo((frame!.width - box!.width) / 2, 0);
      const style = await glyph.evaluate((node) => {
        const computed = getComputedStyle(node);
        return {
          image:
            computed.getPropertyValue("mask-image") ||
            computed.getPropertyValue("-webkit-mask-image"),
          background: computed.backgroundColor,
        };
      });
      expect(style.image).toContain("svg");
      expect(style.background).toBe("rgb(255, 255, 255)");
    }
  }
  await picker.getByRole("tab", { name: "Service", exact: true }).click();
  const fallback = picker.locator('button.res-card[data-template="mystery"]');
  await expect(fallback.locator(".brand-icon__glyph")).toHaveCount(0);
  await expect(fallback.locator(".brand-icon__letters")).toHaveText("M");
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
  await expect(picker).toBeHidden();
  await expect(cards.first()).toBeHidden();
  const wizardBox = await wizard.boundingBox();
  expect(wizardBox, "wizard has a box").not.toBeNull();
  await wizard.locator(".preselected button").click();
  await expect(wizard).toBeHidden();
  await expect(cards.first()).toBeVisible();
});

for (const width of [390, 480, 1280, 1366]) {
  test(`tabs keep the modal size stable and inside the viewport at ${width}`, async ({ page }) => {
    const picker = await openPicker(page);
    await page.setViewportSize({ width, height: 640 });
    await expect(picker).toHaveCSS("transform", "none");
    const initial = await picker.boundingBox();
    expect(initial).not.toBeNull();
    for (const tab of ["Application", "Service", "Database"]) {
      await picker.getByRole("tab", { name: tab, exact: true }).click();
      await expect(picker.getByRole("tabpanel", { name: tab, exact: true })).toBeVisible();
      const box = (await picker.boundingBox())!;
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.y).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(width);
      expect(box.y + box.height).toBeLessThanOrEqual(640);
      expect(box.height).toBeCloseTo(initial!.height, 0);
      expect(box.width).toBeCloseTo(initial!.width, 0);
      const header = picker.locator(".n-card-header");
      const headerY = (await header.boundingBox())!.y;
      await picker.locator(".res-pane").evaluate((pane) => { pane.scrollTop = pane.scrollHeight; });
      expect((await header.boundingBox())!.y).toBe(headerY);
    }
  });
}
