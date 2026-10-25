import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

/* global URL, StorageEvent, document, window:readonly */

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

const serverNode = {
  id: "srv-1",
  name: "node-1",
  role: "manager",
  status: "online",
};

const service = {
  id: "svc-1",
  name: "blog-staging",
  status: "running",
  server_id: "srv-1",
  server_name: "node-1",
  environment_id: "env-1",
  environment_name: "production",
  project_id: "proj-1",
  project_name: "shop",
  compose_project: "gotham-svc-1",
  compose_yaml: "services:\n  web:\n    image: nginx:1.27-alpine\n",
  env: { MYSQL_PASSWORD: "s3cret" },
  domains: [{ service: "web", domain: "blog.example.com", port: 80 }],
  created_at: "2026-09-01T10:00:00Z",
  updated_at: "2026-09-02T10:00:00Z",
};

const deploys = [
  {
    id: "dep-9abcdef",
    service_id: "svc-1",
    state: "running",
    created_at: "2026-09-02T10:00:00Z",
    updated_at: "2026-09-02T10:01:00Z",
    finished_at: "2026-09-02T10:01:00Z",
  },
];

const templates = [
  {
    slug: "n8n",
    name: "n8n",
    icon: "n8n",
    description:
      "n8n workflow automation with a persistent named volume and a routed domain.",
  },
  {
    slug: "acme-custom",
    name: "Acme custom",
    icon: "box",
    description: "Operator-provided template copy stays untouched.",
  },
];

const n8nDetail = {
  slug: "n8n",
  name: "n8n",
  icon: "n8n",
  description:
    "n8n workflow automation with a persistent named volume and a routed domain.",
  fields: [
    {
      key: "domain",
      label: "Domain",
      type: "text",
      required: true,
      placeholder: "n8n.example.com",
      help: "The public host served by the node's Traefik proxy.",
      pattern: "^[A-Za-z0-9][A-Za-z0-9.-]*$",
      max_length: 253,
    },
    {
      key: "encryption_key",
      label: "Encryption key",
      type: "secret",
      required: true,
      help: "Encrypts stored credentials; keep it safe, losing it locks the credentials.",
    },
  ],
};

const n8nRender = {
  slug: "n8n",
  compose_yaml: "services:\n  n8n:\n    image: n8nio/n8n:1.0\n",
  env: { N8N_ENCRYPTION_KEY: "s3cret" },
  spec: { services: ["n8n"], domains: [], named_volumes: ["n8n-data"], mounts: [] },
};

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
      const { readFile } = await import("node:fs/promises");
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

async function mockApi(
  page: Page,
  deploysFailure: { status: number; message: string } | null = null,
): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: { user: session.user } });
  });
  await page.route("**/api/v1/version", async (route) => {
    await route.fulfill({ json: { version: "v0.2.1-dev" } });
  });
  await page.route("**/api/v1/servers", async (route) => {
    await route.fulfill({ json: { servers: [serverNode] } });
  });
  await page.route("**/api/v1/projects/proj-1", async (route) => {
    await route.fulfill({
      json: {
        project: {
          id: "proj-1",
          name: "shop",
          description: "",
          created_at: "2026-09-01T10:00:00Z",
          updated_at: "2026-09-01T10:00:00Z",
          environment_count: 1,
          resource_counts: { applications: 0, services: 1, databases: 0 },
        },
        environments: [
          {
            id: "env-1",
            project_id: "proj-1",
            name: "production",
            created_at: "2026-09-01T10:00:00Z",
            updated_at: "2026-09-01T10:00:00Z",
            resource_counts: { applications: 0, services: 1, databases: 0 },
          },
        ],
      },
    });
  });
  await page.route("**/api/v1/projects", async (route) => {
    await route.fulfill({
      json: {
        projects: [
          {
            id: "proj-1",
            name: "shop",
            description: "",
            created_at: "2026-09-01T10:00:00Z",
            updated_at: "2026-09-01T10:00:00Z",
            environment_count: 1,
            resource_counts: { applications: 0, services: 1, databases: 0 },
          },
        ],
      },
    });
  });
  await page.route("**/api/v1/environments/*/resources*", async (route) => {
    await route.fulfill({
      json: {
        environment: {
          id: "env-1",
          project_id: "proj-1",
          name: "production",
          created_at: "2026-09-01T10:00:00Z",
          updated_at: "2026-09-01T10:00:00Z",
          resource_counts: { applications: 0, services: 1, databases: 0 },
        },
        project: {
          id: "proj-1",
          name: "shop",
          description: "",
          created_at: "2026-09-01T10:00:00Z",
          updated_at: "2026-09-01T10:00:00Z",
          environment_count: 1,
          resource_counts: { applications: 0, services: 1, databases: 0 },
        },
        applications: [],
        services: [],
        databases: [],
      },
    });
  });
  await page.route("**/api/v1/services/svc-1/deploys", async (route) => {
    if (deploysFailure === null) {
      await route.fulfill({ json: { deploys } });
    } else {
      await route.fulfill({
        status: deploysFailure.status,
        json: { message: deploysFailure.message },
      });
    }
  });
  await page.route("**/api/v1/services/svc-1", async (route) => {
    await route.fulfill({ json: { service } });
  });
  await page.route("**/api/v1/templates", async (route) => {
    await route.fulfill({ json: { templates } });
  });
  await page.route("**/api/v1/templates/n8n/render", async (route) => {
    await route.fulfill({ json: n8nRender });
  });
  await page.route("**/api/v1/templates/n8n", async (route) => {
    await route.fulfill({ json: { template: n8nDetail } });
  });
}

async function switchLocale(page: Page, locale: string): Promise<void> {
  await page.evaluate((next) => {
    window.localStorage.setItem("gotham-locale", next);
    window.dispatchEvent(
      new StorageEvent("storage", { key: "gotham-locale", newValue: next }),
    );
  }, locale);
}

async function openServicePicker(page: Page): Promise<Locator> {
  await page.goto(`${baseURL}/projects/proj-1/environments/env-1`);
  await page
    .getByRole("button", { name: /^(Add resource|Thêm tài nguyên)$/ })
    .first()
    .click();
  const picker = page.locator(".n-modal.app-modal");
  await picker.getByRole("tab").nth(1).click();
  await picker.locator(".res-pane").waitFor();
  return picker;
}

async function expectNoOverflow(page: Page, width: number): Promise<void> {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth);
  expect(overflow, "no horizontal overflow").toBeLessThanOrEqual(width + 1);
}

const widths = [390, 900, 1280];

test("service picker renders overlay copy at every width, en and vi", async ({
  page,
}) => {
  for (const width of widths) {
    for (const locale of ["en", "vi"] as const) {
      await page.setViewportSize({ width, height: 900 });
      await mockApi(page);
      const picker = await openServicePicker(page);
      await switchLocale(page, locale);
      if (locale === "en") {
        await expect(picker.locator(".res-pane")).toContainText(
          "Pick a service template, or import a Docker Compose file.",
        );
        await expect(picker).toContainText(
          "n8n workflow automation with a persistent named volume and a routed domain.",
        );
      } else {
        await expect(picker.locator(".res-pane")).toContainText(
          "Chọn template service, hoặc nhập tệp Docker Compose.",
        );
        await expect(picker).toContainText(
          "Tự động hóa quy trình n8n với volume có tên bền vững và tên miền được định tuyến.",
        );
      }
      await expect(picker).toContainText(
        "Operator-provided template copy stays untouched.",
      );
      await expectNoOverflow(page, width);
      await page.screenshot({ path: test.info().outputPath(`picker-${locale}-${width}.png`) });
      await page.unrouteAll({ behavior: "wait" });
    }
  }
});

test("wizard keeps its draft while feedback switches language", async ({ page }) => {
  await page.setViewportSize({ width: 900, height: 900 });
  await mockApi(page);
  const picker = await openServicePicker(page);
  await picker.locator('button.res-card[data-template="n8n"]').click();
  await page.locator('[data-testid="wizard-step-1"]').waitFor();
  await expect(page.locator(".template-wizard")).toContainText("Step 1 / 3");
  await expect(page.locator(".field-domain")).toContainText(
    "The public host served by the node's Traefik proxy.",
  );
  await switchLocale(page, "vi");
  await expect(page.locator(".field-domain")).toContainText(
    "Host công khai do Traefik proxy của node phục vụ.",
  );
  await switchLocale(page, "en");
  await page.locator(".field-domain input").fill("not a domain!!!");
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.locator(".field-domain")).toContainText(
    "Does not match the required format.",
  );
  await expect(page.locator(".field-domain input")).toHaveValue("not a domain!!!");
  await switchLocale(page, "vi");
  await expect(page.locator(".template-wizard")).toContainText("Bước 1 / 3");
  await expect(page.locator(".field-domain")).toContainText(
    "Không đúng định dạng yêu cầu.",
  );
  await expect(page.locator(".field-domain input")).toHaveValue("not a domain!!!");
  await expectNoOverflow(page, 900);
  await page.screenshot({ path: test.info().outputPath("wizard-vi-900.png") });
  await switchLocale(page, "en");
  await expect(page.locator(".template-wizard")).toContainText("Step 1 / 3");
  await expect(page.locator(".field-domain input")).toHaveValue("not a domain!!!");
  await expect(page.locator(".field-domain")).toContainText(
    "Does not match the required format.",
  );
});

test("service detail renders history at every width, en and vi", async ({ page }) => {
  for (const width of widths) {
    for (const locale of ["en", "vi"] as const) {
      await page.setViewportSize({ width, height: 1000 });
      await mockApi(page);
      await page.goto(`${baseURL}/projects/proj-1/environments/env-1/services/svc-1`);
      await expect(page.locator(".service-detail-page h1")).toHaveText("blog-staging");
      await switchLocale(page, locale);
      const historyTitle =
        locale === "vi" ? "Lịch sử triển khai" : "Deploy history";
      await expect(page.locator(".service-detail-page")).toContainText(historyTitle);
      const logsNote =
        locale === "vi" ? "docker compose logs -f qua node agent" : "docker compose logs -f via the node agent";
      await expect(page.locator(".service-detail-page")).toContainText(logsNote);
      await expect(page.locator(".service-detail-page")).toContainText("gotham-svc-1");
      await expect(page.locator(".service-detail-page")).toContainText("blog.example.com:80");
      const deploy = page.getByRole("button", {
        name: locale === "vi" ? "Triển khai" : "Deploy",
        exact: true,
      });
      await expect(deploy).toBeVisible();
      const box = await deploy.boundingBox();
      expect(box, "deploy button has a box").not.toBeNull();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
      await expectNoOverflow(page, width);
      await page.screenshot({ path: test.info().outputPath(`service-${locale}-${width}.png`) });
      await page.unrouteAll({ behavior: "wait" });
    }
  }
});

test("feature-off failure shows the translated frame with the intact token", async ({
  page,
}) => {
  for (const width of widths) {
    for (const locale of ["en", "vi"] as const) {
      await page.setViewportSize({ width, height: 900 });
      await mockApi(page);
      await page.route("**/api/v1/templates", async (route) => {
        await route.fulfill({ status: 503, json: { message: "" } });
      });
      const picker = await openServicePicker(page);
      await switchLocale(page, locale);
      const expected =
        locale === "vi"
          ? "Dịch vụ đang tắt trên control plane này. Hãy đề nghị chủ sở hữu bật."
          : "Services are turned off on this control plane. Ask the owner to enable them.";
      await expect(picker.locator(".res-pane")).toContainText(expected);
      const retry = locale === "vi" ? "Thử lại" : "Retry";
      await expect(picker.getByRole("button", { name: retry })).toBeVisible();
      await expectNoOverflow(page, width);
      await page.screenshot({
        path: test.info().outputPath(`feature-off-${locale}-${width}.png`),
      });
      await page.unrouteAll({ behavior: "wait" });
    }
  }
});

test("retained deploy-history failure re-derives in the current locale", async ({
  page,
}) => {
  await page.setViewportSize({ width: 900, height: 1000 });
  await mockApi(page, { status: 502, message: "services: dial node" });
  await page.goto(`${baseURL}/projects/proj-1/environments/env-1/services/svc-1`);
  await expect(page.locator(".service-detail-page h1")).toHaveText("blog-staging");
  await expect(page.locator('[data-testid="history-unavailable"]')).toContainText(
    "Deploy history unavailable: Node agent error: dial node",
  );
  await switchLocale(page, "vi");
  await expect(page.locator('[data-testid="history-unavailable"]')).toContainText(
    "Không đọc được lịch sử triển khai: Lỗi node agent: dial node",
  );
  await page.screenshot({ path: test.info().outputPath("service-history-vi-900.png") });
});

test("unknown failures pair a localized summary with the identical raw detail", async ({
  page,
}) => {
  for (const width of widths) {
    for (const locale of ["en", "vi"] as const) {
      await page.setViewportSize({ width, height: 1000 });
      await mockApi(page, { status: 500, message: "services: boom" });
      await page.goto(`${baseURL}/projects/proj-1/environments/env-1/services/svc-1`);
      await expect(page.locator(".service-detail-page h1")).toHaveText("blog-staging");
      await switchLocale(page, locale);
      const expected =
        locale === "vi"
          ? "Không đọc được lịch sử triển khai: Yêu cầu thất bại: boom"
          : "Deploy history unavailable: Request failed: boom";
      await expect(page.locator('[data-testid="history-unavailable"]')).toContainText(
        expected,
      );
      await expectNoOverflow(page, width);
      await page.screenshot({
        path: test.info().outputPath(`service-unknown-${locale}-${width}.png`),
      });
      await page.unrouteAll({ behavior: "wait" });
    }
  }
});
