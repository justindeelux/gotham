import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, window:readonly, getComputedStyle:readonly */
// JUS-75 round 1: real-browser proof that the create-application notices are
// light, the two Change links are unambiguous, and the Compose web-service
// select is visible at 1440x900. Same mocked-API webdist harness as
// environment-layout.spec.ts: static file server, seeded session, no backend.

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
      updated_at: "2026-10-01T00:00:00Z",
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

/** mockApi seeds the session and answers the calls the wizard makes. */
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
    await route.fulfill({ json: { templates: [] } });
  });
}

/** openWizard opens the create-application wizard preselected to one source. */
async function openWizard(page: Page, cardText: string): Promise<Locator> {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects/${envPath}`);
  await page.locator(".environment-page .title").waitFor();
  await page.getByRole("button", { name: "Add resource" }).first().click();
  const picker = page.locator(".n-modal.app-modal");
  await expect(picker.getByRole("heading", { name: "Application" })).toBeVisible();
  await picker.locator("button.res-card").filter({ hasText: cardText }).first().click();
  const wizard = page.locator(".n-modal.wizard-modal");
  await expect(
    wizard.getByRole("heading", { name: "Create application", exact: true }),
  ).toBeVisible();
  // Let the modal entrance transition settle before measuring boxes.
  await page.waitForTimeout(500);
  return wizard;
}

/** noticeBox measures one rendered warning notice inside the wizard. */
async function noticeBox(wizard: Locator, text: string): Promise<{ height: number }> {
  const notice = wizard.locator(".n-alert").filter({ hasText: text }).first();
  await expect(notice).toBeVisible();
  const box = await notice.boundingBox();
  expect(box, "notice has a box").not.toBeNull();
  return { height: box!.height };
}

test("JUS-75 notices render light and short in a real DOM", async ({ page }) => {
  // Compose secrets notice.
  const compose = await openWizard(page, "Docker Compose");
  const secrets = await noticeBox(compose, "Do not paste secrets");
  expect(secrets.height, "compose secrets notice height").toBeLessThanOrEqual(64);
  const composeStyle = await compose
    .locator(".n-alert")
    .filter({ hasText: "Do not paste secrets" })
    .first()
    .evaluate((element) => {
      const style = getComputedStyle(element);
      const content = element.querySelector(".n-alert-body__content");
      return {
        paddingTop: style.paddingTop,
        fontSize: content ? getComputedStyle(content).fontSize : "",
        borderLeftWidth: style.borderLeftWidth,
        background: style.backgroundColor,
      };
    });
  expect(composeStyle.fontSize, "notice text size").toBe("13px");
  expect(composeStyle.borderLeftWidth, "notice accent bar").toBe("3px");
  expect(composeStyle.background, "notice tint is translucent").toMatch(/\/ 0\.16\)/);
  await page.keyboard.press("Escape");

  // Latest-tag warning (appears after typing a moving tag).
  const image = await openWizard(page, "Container image");
  await image.locator("input[placeholder='registry.example.com/team/app:1.2']").fill("app:latest");
  const latest = await noticeBox(image, "moving latest tag");
  expect(latest.height, "latest-tag notice height").toBeLessThanOrEqual(64);
  await page.keyboard.press("Escape");

  // Build-args warning in the Dockerfile step.
  const docker = await openWizard(page, "Dockerfile");
  const args = await noticeBox(docker, "Do not put secrets here");
  expect(args.height, "build-args notice height").toBeLessThanOrEqual(64);
});

test("JUS-75 the two header links are unambiguous in a real DOM", async ({ page }) => {
  const wizard = await openWizard(page, "Docker Compose");
  await expect(wizard.getByRole("button", { name: "Change scope" })).toBeVisible();
  await expect(wizard.getByRole("button", { name: "Change source" })).toBeVisible();
  // No element carries the bare ambiguous label anymore.
  const bareCount = await wizard.evaluate(
    (element) =>
      Array.from(element.querySelectorAll("*")).filter(
        (node) => (node.textContent ?? "").trim() === "Change",
      ).length,
  );
  expect(bareCount).toBe(0);
});

test("JUS-75 the Compose web-service select is visible at 1440x900", async ({
  page,
}) => {
  const wizard = await openWizard(page, "Docker Compose");
  const serviceItem = wizard.locator(".n-form-item", { hasText: "Web service" }).first();
  await expect(serviceItem).toBeVisible();
  const select = serviceItem.locator(".n-select").first();
  const selectBox = await select.boundingBox();
  expect(selectBox, "service select has a box").not.toBeNull();
  const bodyBox = await wizard.locator(".wizard-body").boundingBox();
  expect(bodyBox, "wizard body has a box").not.toBeNull();
  // Fully inside the scroll body viewport: no scrolling needed to reach it.
  expect(selectBox!.y).toBeGreaterThanOrEqual(bodyBox!.y - 1);
  expect(selectBox!.y + selectBox!.height).toBeLessThanOrEqual(bodyBox!.y + bodyBox!.height + 1);
});
