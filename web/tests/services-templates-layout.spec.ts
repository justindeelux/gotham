import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

import { waitForLayoutSettled } from "./support";

/* global URL, StorageEvent, document, window, getComputedStyle:readonly */

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
  compose_yaml:
    "services:\n  web:\n    image: nginx:1.27-alpine\n    ports:\n      - \"8080:8080\"\n",
  env: { MYSQL_PASSWORD: "s3cret" },
  domains: [{ service: "web", domain: "blog.example.com", port: 80 }],
  pending_changes: [],
  created_at: "2026-09-01T10:00:00Z",
  updated_at: "2026-09-02T10:00:00Z",
};

const serviceDomains = [
  {
    id: "dom-1",
    service_id: "svc-1",
    domain: "app.example.com",
    compose_service: "web",
    port: 8080,
    protocol: "http",
    disabled: false,
    source: "managed",
    status: "routed",
  },
  {
    service_id: "svc-1",
    domain: "legacy.example.com",
    compose_service: "web",
    port: 3001,
    protocol: "http",
    disabled: false,
    source: "compose",
    status: "held",
    reason: "container port 3001 is not published; add it to the compose service ports",
  },
];

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
      required: false,
      default: "",
      placeholder: "n8n.example.com",
      help: "Optional. Leave empty to add a domain later from the service page.",
      pattern: "^([A-Za-z0-9][A-Za-z0-9.-]*)?$",
      max_length: 253,
    },
    {
      key: "encryption_key",
      label: "Encryption key",
      type: "secret",
      required: true,
      generate: true,
    },
  ],
};

const n8nRender = {
  slug: "n8n",
  compose_yaml:
    'services:\n  n8n:\n    image: "n8nio/n8n:1.0"\n    environment:\n      N8N_ENCRYPTION_KEY: ${encryption_key}\n    # exposed by Traefik\n    ports:\n      - "5678:5678"\n',
  env: { N8N_ENCRYPTION_KEY: "s3cret" },
  domains: [],
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
  await page.route("**/api/v1/services/svc-1/domains", async (route) => {
    await route.fulfill({ json: { domains: serviceDomains } });
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

async function openTemplateWizard(page: Page): Promise<Locator> {
  const picker = await openServicePicker(page);
  await picker.locator('button.res-card[data-template="n8n"]').click();
  const wizard = page.locator(".template-wizard");
  await wizard.waitFor();
  // The card enters with a 0.5 -> 1 scale transition anchored at the trigger.
  // Measuring before it ends samples two frames and reports a fake offset.
  await waitForLayoutSettled(page);
  return wizard;
}

async function openPreviewStep(page: Page): Promise<Locator> {
  const wizard = await openTemplateWizard(page);
  await page.locator('[data-testid="wizard-step-1"]').waitFor();
  await page.locator(".field-domain input").fill("n8n.example.com");
  await page.locator(".field-encryption_key input").fill("hunter2");
  await page.getByRole("button", { name: "Next", exact: true }).click();
  await expect(page.locator('[data-testid="compose-preview"]')).toBeVisible();
  return wizard;
}

async function openCreateStep(page: Page): Promise<Locator> {
  const wizard = await openPreviewStep(page);
  const next = page.getByRole("button", { name: "Next", exact: true });
  await expect(next).toBeEnabled();
  await next.click();
  await page.locator('[data-testid="wizard-step-3"]').waitFor();
  return wizard;
}

async function previewTokenColors(page: Page): Promise<string[]> {
  return page.locator('[data-testid="compose-preview"] .cm-content').evaluate((root) => {
    const seen = new Set<string>();
    for (const span of root.querySelectorAll("span")) {
      seen.add(getComputedStyle(span).color);
    }
    return [...seen];
  });
}

async function boxOf(page: Page, selector: string) {
  const box = await page.locator(selector).boundingBox();
  expect(box, `${selector} has a box`).not.toBeNull();
  return box!;
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
    "Optional. Leave empty to add a domain later from the service page.",
  );
  await switchLocale(page, "vi");
  await expect(page.locator(".field-domain")).toContainText(
    "Không bắt buộc. Để trống để thêm tên miền sau từ trang dịch vụ.",
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

test("generated secret stays fully visible beside its actions", async ({ page }) => {
  for (const width of [390, 1280] as const) {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page);
    await openTemplateWizard(page);
    await page.locator('[data-testid="wizard-step-1"]').waitFor();

    const input = page.locator(".field-encryption_key input");
    const generate = page.locator(
      '.field-encryption_key [aria-label="Generate Encryption key"]',
    );
    await generate.click();
    await expect(input).toHaveValue(/^[A-Za-z0-9]{24}$/);

    const metrics = await input.evaluate((element) => {
      const node = element as unknown as { scrollWidth: number; clientWidth: number };
      return { scrollWidth: node.scrollWidth, clientWidth: node.clientWidth };
    });
    expect(
      metrics.scrollWidth,
      `the generated value is clipped at ${width}px`,
    ).toBeLessThanOrEqual(metrics.clientWidth + 1);

    const inputBox = await input.boundingBox();
    const buttonBox = await generate.boundingBox();
    expect(inputBox, "secret input has a box").not.toBeNull();
    expect(buttonBox, "generate button has a box").not.toBeNull();
    const stacked = buttonBox!.y >= inputBox!.y + inputBox!.height;
    if (!stacked) {
      expect(inputBox!.x + inputBox!.width).toBeLessThanOrEqual(buttonBox!.x + 1);
    }
    expect(buttonBox!.x + buttonBox!.width).toBeLessThanOrEqual(width + 1);

    const copy = page.locator(
      '.field-encryption_key [aria-label="Copy Encryption key"]',
    );
    await expect(copy).toBeVisible();
    const copyBox = await copy.boundingBox();
    expect(copyBox, "copy button has a box").not.toBeNull();
    const gap = copyBox!.x - (buttonBox!.x + buttonBox!.width);
    expect(gap, `actions sit together at ${width}px`).toBeGreaterThanOrEqual(-1);
    expect(gap, `actions sit together at ${width}px`).toBeLessThanOrEqual(16);
    expect(
      Math.abs(copyBox!.y - buttonBox!.y),
      `actions share a row at ${width}px`,
    ).toBeLessThanOrEqual(1);
    await expectNoOverflow(page, width);
    await page.screenshot({
      path: test.info().outputPath(`generated-secret-${width}.png`),
    });
    await page.unrouteAll({ behavior: "wait" });
  }
});

test("service detail renders history at every width, en and vi", async ({ page }) => {
  for (const width of widths) {
    for (const locale of ["en", "vi"] as const) {
      await page.setViewportSize({ width, height: 1000 });
      await mockApi(page);
      await page.goto(`${baseURL}/projects/proj-1/environments/env-1/services/svc-1`);
      await expect(page.locator(".service-detail-page h1")).toHaveText("blog-staging");
      await switchLocale(page, locale);
      const tabs =
        locale === "vi"
          ? {
              labels: ["Tổng quan", "Triển khai", "Compose", "Môi trường", "Container", "Tên miền", "Nhật ký", "Cài đặt"],
              deployments: "Triển khai",
              logs: "Nhật ký",
              overview: "Tổng quan",
            }
          : {
              labels: ["Overview", "Deployments", "Compose", "Environment", "Containers", "Domains", "Logs", "Settings"],
              deployments: "Deployments",
              logs: "Logs",
              overview: "Overview",
            };
      for (const label of tabs.labels) {
        await expect(
          page.locator(".n-tabs-tab").filter({ hasText: new RegExp(`^${label}$`) }).first(),
        ).toBeVisible();
      }
      await page.locator(".n-tabs-tab").filter({ hasText: new RegExp(`^${tabs.deployments}$`) }).click();
      const historyTitle =
        locale === "vi" ? "Lịch sử triển khai" : "Deploy history";
      await expect(page.locator(".service-detail-page")).toContainText(historyTitle);
      await page.locator(".n-tabs-tab").filter({ hasText: new RegExp(`^${tabs.logs}$`) }).click();
      await expect(page.locator(".service-detail-page")).toContainText("docker compose logs -f");
      await page.locator(".n-tabs-tab").filter({ hasText: new RegExp(`^${tabs.overview}$`) }).click();
      await expect(page.locator(".service-detail-page")).toContainText("gotham-svc-1");
      await expect(page.locator(".service-detail-page")).toContainText("app.example.com");
      const deploy = page.getByRole("button", {
        name: locale === "vi" ? "Triển khai lại" : "Redeploy",
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

test("service domains tab opens the add modal without overflow", async ({
  page,
}) => {
  for (const width of [390, 1366]) {
    await page.setViewportSize({ width, height: 1000 });
    await mockApi(page);
    await page.goto(`${baseURL}/projects/proj-1/environments/env-1/services/svc-1`);
    await expect(page.locator(".service-detail-page h1")).toHaveText("blog-staging");
    await page
      .locator(".n-tabs-tab")
      .filter({ hasText: /^Domains$/ })
      .click();
    await expect(page.locator(".domain-table")).toContainText("app.example.com");
    await expect(page.locator(".domain-table")).toContainText("From compose file");
    await expect(page.locator(".domain-open-compose")).toBeVisible();

    await page.locator(".domain-add").click();
    const modal = page.locator(".domain-form-modal");
    await expect(modal).toBeVisible();
    await expect(modal.locator(".domain-target")).toContainText("web");
    await expect(modal.locator(".domain-port input")).toHaveValue("8080");
    await expectNoOverflow(page, width);
    await page.screenshot({
      path: test.info().outputPath(`service-domains-${width}.png`),
    });
    await page.unrouteAll({ behavior: "wait" });
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
  await page.locator(".n-tabs-tab").filter({ hasText: /^Deployments$/ }).click();
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
      await page
        .locator(".n-tabs-tab")
        .filter({ hasText: new RegExp(`^${locale === "vi" ? "Triển khai" : "Deployments"}$`) })
        .click();
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

test("create step top-aligns Service name and Node, then stacks at 390px", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1366, height: 900 });
  await mockApi(page);
  const wizard = await openCreateStep(page);

  const nameInput = await boxOf(page, ".wizard__metaform .field-service-name .n-input");
  const nodeSelect = await boxOf(
    page,
    ".wizard__metaform > .n-form-item:nth-child(2) .n-select",
  );
  expect(
    Math.abs(nameInput.y - nodeSelect.y),
    "Service name and Node inputs share one top edge",
  ).toBeLessThanOrEqual(1);

  await expect(wizard).not.toContainText("/api/v1");
  await expect(page.locator(".template-wizard .wizard__desc")).toHaveCount(0);
  await page.screenshot({ path: test.info().outputPath("create-1366.png") });

  await page.setViewportSize({ width: 390, height: 900 });
  await waitForLayoutSettled(page);
  const stackedName = await boxOf(page, ".wizard__metaform .field-service-name .n-input");
  const stackedNode = await boxOf(
    page,
    ".wizard__metaform > .n-form-item:nth-child(2) .n-select",
  );
  expect(
    stackedNode.y,
    "stacked fields keep a normal vertical gap",
  ).toBeGreaterThanOrEqual(stackedName.y + stackedName.height + 11);
  await expectNoOverflow(page, 390);
  await page.screenshot({ path: test.info().outputPath("create-390.png") });
});

test("configure step top-aligns fields where only one has hint text", async ({ page }) => {
  await page.setViewportSize({ width: 1366, height: 900 });
  await mockApi(page);
  await openTemplateWizard(page);

  const domain = await boxOf(page, ".field-domain .n-input");
  const key = await boxOf(page, ".field-encryption_key .n-input");
  expect(
    Math.abs(domain.y - key.y),
    "inputs on the same dynamic-form row share one top edge",
  ).toBeLessThanOrEqual(1);
});

test("compose preview highlights YAML and keeps a gap before the alert", async ({
  page,
}) => {
  for (const theme of ["dark", "light"] as const) {
    await page.setViewportSize({ width: 1366, height: 900 });
    await page.addInitScript((value: string) => {
      window.localStorage.setItem("gotham.files.editor.v1.u-1", value);
    }, JSON.stringify({ version: 1, theme }));
    await mockApi(page);
    await openPreviewStep(page);

    const preview = page.locator('[data-testid="compose-preview"]');
    await expect(preview).toContainText("n8nio/n8n");
    await expect(preview).toContainText("${encryption_key}");
    await expect
      .poll(async () => new Set(await previewTokenColors(page)).size, {
        message: `${theme}: YAML keys and strings use distinct colours`,
      })
      .toBeGreaterThan(1);

    await waitForLayoutSettled(page);
    const tags = await boxOf(page, ".wizard__preview-body > .n-space");
    const previewBox = await boxOf(page, '[data-testid="compose-preview"]');
    const alert = await boxOf(page, ".wizard__preview-body > .n-alert");
    expect(tags.y + tags.height + 11).toBeLessThanOrEqual(previewBox.y);
    expect(previewBox.y + previewBox.height + 11).toBeLessThanOrEqual(alert.y);
    await page.screenshot({
      path: test.info().outputPath(`preview-${theme}-1366.png`),
    });

    await page.setViewportSize({ width: 390, height: 900 });
    await waitForLayoutSettled(page);
    const narrow = await boxOf(page, '[data-testid="compose-preview"]');
    expect(narrow.x + narrow.width).toBeLessThanOrEqual(390);
    await expectNoOverflow(page, 390);
    await page.screenshot({
      path: test.info().outputPath(`preview-${theme}-390.png`),
    });
    await page.unrouteAll({ behavior: "wait" });
  }
});

test("stacked forms elsewhere keep their item gap", async ({ page }) => {
  await page.setViewportSize({ width: 1366, height: 900 });
  await mockApi(page);
  const picker = await openServicePicker(page);
  await picker.getByRole("link", { name: "Import a compose file" }).click();

  const yamlField = page.locator(".field-import-yaml");
  await expect(yamlField).toBeVisible();
  const marginTop = await yamlField.evaluate((element) => getComputedStyle(element).marginTop);
  expect(marginTop, "stacked form items keep the shared rhythm").toBe("16px");
});
