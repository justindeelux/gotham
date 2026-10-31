import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Locator, Page } from "@playwright/test";

import { waitForLayoutSettled } from "./support";

/* global URL, document, getComputedStyle, window:readonly, Element, HTMLElement */

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
    role: "owner",
  },
  accessToken: "test-access",
  refreshToken: "test-refresh",
};

const longName = "checkout-service-with-a-very-long-resource-name-for-wrapping";

const server = {
  id: "srv-1",
  name: longName,
  ip: "10.0.0.5",
  port: 22,
  ssh_user: "root",
  ssh_key_id: null,
  has_password: true,
  status: "ready",
  node_id: "localhost",
  os: "Ubuntu 22.04",
  docker_version: "27.0.3",
  arch: "amd64",
  total_mem: 8_589_934_592,
  total_disk: 107_374_182_400,
  cpu_usage: 0.12,
  mem_usage: 0.34,
  disk_usage: 0.56,
  container_count: 3,
  last_seen: "2026-10-26T12:00:00Z",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

const application = {
  id: "app-1",
  name: longName,
  environment_id: "env-1",
  environment_name: "production",
  project_id: "proj-1",
  project_name: "Acme",
  provider: "github",
  repo: "acme/checkout",
  clone_url: "https://github.com/acme/checkout.git",
  source_type: "git_public",
  github_app_id: "",
  branch: "main",
  build_pack: "dockerfile",
  image_ref: "",
  has_registry_credential: false,
  primary_domain: "checkout.example.com",
  server_id: "srv-1",
  server_name: longName,
  pending_changes: [],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

const database = {
  id: "db-1",
  name: longName,
  environment_id: "env-1",
  environment_name: "production",
  project_id: "proj-1",
  project_name: "Acme",
  engine: "postgres",
  version: "16",
  status: "running",
  server_id: "srv-1",
  server_name: longName,
  container_id: "ctr-1",
  public_port: 15432,
  volume: "gotham-db-1-data",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

const service = {
  id: "svc-1",
  name: longName,
  status: "running",
  server_id: "srv-1",
  server_name: longName,
  environment_id: "env-1",
  environment_name: "production",
  project_id: "proj-1",
  project_name: "Acme",
  compose_project: "acme-checkout",
  env: {},
  domains: [{ service: "web", domain: "checkout.example.com", port: 8080 }],
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

const dashboardProjects = Array.from({ length: 7 }, (_, index) => ({
  id: `00000000-0000-4000-8000-00000000000${index + 1}`,
  name: `project-${index + 1}`,
  description: `Product number ${index + 1}`,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
  environment_count: 1,
  resource_counts: { applications: 1, services: 0, databases: 0 },
}));

interface ResourceRoute {
  name: string;
  path: string;
  meta: boolean;
  title: string;
  tabs?: string[];
}

const resourceRoutes: ResourceRoute[] = [
  { name: "server", path: "/servers/srv-1", meta: true, title: longName },
  {
    name: "application",
    path: "/projects/proj-1/environments/env-1/applications/app-1",
    meta: true,
    title: longName,
    tabs: ["Overview", "Logs", "Environment", "Settings"],
  },
  {
    name: "database",
    path: "/projects/proj-1/environments/env-1/databases/db-1",
    meta: true,
    title: longName,
    tabs: ["Overview", "Metrics", "Backups", "Logs", "Settings"],
  },
  {
    name: "service",
    path: "/projects/proj-1/environments/env-1/services/svc-1",
    meta: true,
    title: longName,
    tabs: [
      "Overview",
      "Deployments",
      "Compose",
      "Environment",
      "Containers",
      "Logs",
      "Settings",
    ],
  },
];

function json(status: number, body: unknown) {
  return { status, contentType: "application/json", body: JSON.stringify(body) };
}

interface ApiOverrides {
  application?: Record<string, unknown>;
  database?: Record<string, unknown>;
  service?: Record<string, unknown>;
  server?: Record<string, unknown>;
  deploys?: Record<string, unknown>[];
  projects?: Array<Record<string, unknown>>;
}

function color(value: string): string {
  return value.replaceAll(" ", "");
}

let serverHandle: Server;
let baseURL = "";

test.beforeAll(async () => {
  serverHandle = createServer(async (request, response) => {
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
    serverHandle.listen(0, "127.0.0.1", () => done());
  });
  const address = serverHandle.address() as AddressInfo;
  baseURL = `http://127.0.0.1:${address.port}`;
});

test.afterAll(async () => {
  await new Promise<void>((done) => {
    serverHandle.close(() => done());
  });
});

async function mockApi(
  page: Page,
  role = "owner",
  overrides: ApiOverrides = {},
): Promise<void> {
  const app = { ...application, ...overrides.application };
  const db = { ...database, ...overrides.database };
  const svc = { ...service, ...overrides.service };
  const srv = { ...server, ...overrides.server };
  const deploys = overrides.deploys ?? [];
  const projects = overrides.projects ?? dashboardProjects;
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify({ ...session, user: { ...session.user, role } }));
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const method = request.method();
    const path = request.url().split("/api/v1")[1]!.split("?")[0]!;
    const get = method === "GET";
    if (get && path === "/auth/me") {
      await route.fulfill(json(200, { user: session.user }));
      return;
    }
    if (get && path === "/auth/config") {
      await route.fulfill(json(200, { registrationOpen: false }));
      return;
    }
    if (get && path === "/version") {
      await route.fulfill(json(200, { version: "v0.2.1-dev" }));
      return;
    }
    if (get && path === "/servers") {
      await route.fulfill(json(200, { servers: [srv] }));
      return;
    }
    if (get && path === "/projects") {
      await route.fulfill(json(200, { projects }));
      return;
    }
    if (get && path === "/applications") {
      await route.fulfill(json(200, { applications: [] }));
      return;
    }
    if (get && path === "/deployments") {
      await route.fulfill(json(200, { deployments: [] }));
      return;
    }
    if (get && path === "/incidents") {
      await route.fulfill(json(200, { incidents: [], active_count: 0 }));
      return;
    }
    if (get && path === "/notifications") {
      await route.fulfill(json(200, { notifications: [], unread_count: 0 }));
      return;
    }
    if (get && path === "/notification-channels/failures") {
      await route.fulfill(json(200, { failures: [] }));
      return;
    }
    if (get && path === "/servers/srv-1") {
      await route.fulfill(json(200, { server: srv }));
      return;
    }
    if (get && path === "/servers/srv-1/containers") {
      await route.fulfill(json(200, { containers: [] }));
      return;
    }
    if (get && path === "/applications/app-1") {
      await route.fulfill(json(200, { application: app }));
      return;
    }
    if (get && path === "/applications/app-1/deployments") {
      await route.fulfill(json(200, { deployments: [] }));
      return;
    }
    if (get && path === "/databases") {
      await route.fulfill(json(200, { databases: [db] }));
      return;
    }
    if (get && path === "/databases/db-1") {
      await route.fulfill(json(200, { database: db }));
      return;
    }
    if (get && path === "/databases/db-1/credentials") {
      await route.fulfill(
        json(200, {
          credentials: { username: "gotham", password: "secret", database: "gotham" },
        }),
      );
      return;
    }
    if (get && path === "/services/svc-1") {
      await route.fulfill(json(200, { service: svc }));
      return;
    }
    if (get && path === "/services/svc-1/containers") {
      await route.fulfill(json(200, { containers: [] }));
      return;
    }
    if (get && path === "/services/svc-1/deploys") {
      await route.fulfill(json(200, { deploys }));
      return;
    }
    await route.fulfill(json(200, {}));
  });
}

interface HeaderStyles {
  h1Count: number;
  gap: string;
  marginBottom: string;
  title: Record<string, string>;
  meta: Record<string, string> | null;
  actionBox: { x: number; width: number } | null;
  headBox: { x: number; width: number };
  titleBox: { x: number; width: number };
  metaOverflow: boolean;
  overflow: boolean;
}

async function measure(page: Page): Promise<HeaderStyles> {
  return page.evaluate(() => {
    const head = document.querySelector<HTMLElement>(".page-head")!;
    const read = (element: Element): Record<string, string> => {
      const style = getComputedStyle(element);
      return {
        fontFamily: style.fontFamily,
        fontSize: style.fontSize,
        fontWeight: style.fontWeight,
        lineHeight: style.lineHeight,
        color: style.color,
      };
    };
    const h1 = head.querySelector<HTMLElement>("h1")!;
    const meta = head.querySelector<HTMLElement>(".page-head__meta");
    const actions = head.querySelector<HTMLElement>(".page-actions");
    const headRect = head.getBoundingClientRect();
    const titleRect = h1.getBoundingClientRect();
    const actionRect = actions?.getBoundingClientRect();
    return {
      h1Count: document.querySelectorAll("h1").length,
      gap: getComputedStyle(head).gap,
      marginBottom: getComputedStyle(head).marginBottom,
      title: read(h1),
      meta: meta ? read(meta) : null,
      actionBox: actionRect ? { x: actionRect.x, width: actionRect.width } : null,
      headBox: { x: headRect.x, width: headRect.width },
      titleBox: { x: titleRect.x, width: titleRect.width },
      metaOverflow: meta ? meta.scrollWidth > meta.clientWidth + 1 : false,
      overflow:
        document.documentElement.scrollWidth >
        document.documentElement.clientWidth + 1,
    };
  });
}

for (const route of resourceRoutes) {
  test(`${route.name} detail header follows the shared contract`, async ({ page }) => {
    test.setTimeout(120_000);
    await mockApi(page);

    await page.setViewportSize({ width: 1366, height: 900 });
    await page.goto(`${baseURL}${route.path}`);
    await page.locator(".page-head h1").first().waitFor();
    const desktop = await measure(page);

    expect(desktop.h1Count, `${route.name} has exactly one h1`).toBe(1);
    await expect(page.locator(".page-head h1")).toHaveText(route.title);
    expect(desktop.gap, `${route.name} header gap`).toBe("16px");
    expect(desktop.marginBottom, `${route.name} header bottom margin`).toBe(
      "24px",
    );

    expect(desktop.title.fontFamily, `${route.name} title family`).toContain(
      "Google Sans",
    );
    expect(desktop.title.fontSize, `${route.name} title size`).toBe("24px");
    expect(desktop.title.fontWeight, `${route.name} title weight`).toBe("700");
    expect(desktop.title.lineHeight, `${route.name} title leading`).toBe("30px");
    expect(color(desktop.title.color), `${route.name} title color`).toBe(
      "rgb(242,243,245)",
    );

    if (route.meta) {
      expect(desktop.meta, `${route.name} meta line`).not.toBeNull();
      expect(desktop.meta!.fontFamily, `${route.name} meta family`).toContain(
        "JetBrains Mono",
      );
      expect(desktop.meta!.fontSize, `${route.name} meta size`).toBe("14px");
      expect(color(desktop.meta!.color), `${route.name} meta color`).toBe(
        "rgb(148,155,164)",
      );
      expect(desktop.metaOverflow, `${route.name} meta wraps`).toBe(false);
    }

    expect(desktop.actionBox, `${route.name} action row`).not.toBeNull();
    const right = desktop.actionBox!.x + desktop.actionBox!.width;
    expect(
      Math.abs(right - (desktop.headBox.x + desktop.headBox.width)),
      `${route.name} action row ends at the header right edge`,
    ).toBeLessThanOrEqual(1);

    for (const tab of route.tabs ?? []) {
      await expect(
        page.locator(".n-tabs-tab").filter({ hasText: new RegExp(`^${tab}$`) }).first(),
        `${route.name} shows the ${tab} tab`,
      ).toBeVisible();
    }
    if (route.name === "service") {
      await expect(
        page.locator(".page-head__breadcrumb"),
        "service header has no duplicate back link",
      ).toHaveCount(0);
    }
    await expect(
      page.locator(".page-actions button").filter({ hasText: /^Delete$/ }),
      `${route.name} header has no delete action`,
    ).toHaveCount(0);
    if (route.tabs?.includes("Settings")) {
      await page.locator(".n-tabs-tab").filter({ hasText: /^Settings$/ }).click();
      await expect(
        page.locator(".n-tab-pane button").filter({ hasText: /Delete/ }).first(),
        `${route.name} delete action lives in Settings`,
      ).toBeVisible();
    }

    await page.setViewportSize({ width: 390, height: 844 });
    const mobile = await measure(page);

    expect(mobile.h1Count, `${route.name} keeps one h1 at 390px`).toBe(1);
    expect(mobile.overflow, `${route.name} horizontal overflow at 390px`).toBe(
      false,
    );
    expect(
      mobile.titleBox.x + mobile.titleBox.width,
      `${route.name} title fits the viewport at 390px`,
    ).toBeLessThanOrEqual(391);
    expect(mobile.title.fontSize, `${route.name} title size at 390px`).toBe(
      "24px",
    );
    if (route.meta) {
      expect(mobile.metaOverflow, `${route.name} meta wraps at 390px`).toBe(
        false,
      );
    }
    expect(mobile.actionBox, `${route.name} action row at 390px`).not.toBeNull();
    expect(
      Math.abs(mobile.actionBox!.x - mobile.headBox.x),
      `${route.name} action row is left-aligned at 390px`,
    ).toBeLessThanOrEqual(1);
  });
}

test("containers tab renders the table inside the server page", async ({
  page,
}) => {
  await mockApi(page);
  await page.goto(`${baseURL}/servers/srv-1/containers`);
  await expect(page).toHaveURL(`${baseURL}/servers/srv-1?tab=containers`);
  await expect(page.locator(".page-head h1")).toHaveText(longName);
  await expect(
    page.locator(".n-tabs-tab--active"),
  ).toHaveText("Containers");
  await expect(page.locator(".containers-tab .n-data-table")).toBeVisible();
  await expect(
    page.locator(".containers-tab .n-card-header button", { hasText: "Refresh" }),
  ).toBeVisible();
});

test("member views keep the same header and lifecycle controls", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await mockApi(page, "member");

  for (const route of resourceRoutes.filter((item) => item.name !== "server")) {
    await page.setViewportSize({ width: 1366, height: 900 });
    await page.goto(`${baseURL}${route.path}`);
    await page.locator(".page-head h1").first().waitFor();
    await expect(page.locator(".page-head h1")).toHaveCount(1);
    expect(
      await page.locator(".page-head .page-actions button").count(),
      `${route.name} keeps its lifecycle controls for a member`,
    ).toBeGreaterThan(0);
  }
});

const detailPaths = {
  application: "/projects/proj-1/environments/env-1/applications/app-1",
  database: "/projects/proj-1/environments/env-1/databases/db-1",
  service: "/projects/proj-1/environments/env-1/services/svc-1",
};

interface BannerCase {
  name: string;
  path: string;
  role?: string;
  overrides: ApiOverrides;
  text: string;
}

const bannerCases: BannerCase[] = [
  {
    name: "database provisioning",
    path: detailPaths.database,
    overrides: { database: { status: "creating" } },
    text: "Setting up the database",
  },
  {
    name: "database provisioning error",
    path: detailPaths.database,
    overrides: {
      database: { status: "error", status_message: "Image pull failed" },
    },
    text: "Image pull failed",
  },
  {
    name: "service deploying",
    path: detailPaths.service,
    overrides: {
      deploys: [
        {
          id: "dep-1",
          service_id: "svc-1",
          state: "deploying",
          created_at: "2026-01-03T00:00:00Z",
          updated_at: "2026-01-03T00:00:00Z",
        },
      ],
    },
    text: "The deploy is running in the background",
  },
  {
    name: "application pending changes",
    path: detailPaths.application,
    overrides: { application: { pending_changes: ["environment"] } },
    text: "Redeploy to apply changes",
  },
  {
    name: "application blocked terminal",
    path: `${detailPaths.application}?tab=terminal`,
    role: "member",
    overrides: {},
    text: "Terminal not available",
  },
];

interface BannerLayout {
  count: number;
  gaps: number[];
  bottomGap: number;
  minSpace: number;
  bannerBottom: number;
  actionsTop: number | null;
}

async function measureBanners(page: Page): Promise<BannerLayout> {
  return page.evaluate(() => {
    const container = document.querySelector<HTMLElement>(".page-banners")!;
    const head = document.querySelector<HTMLElement>(".page-head")!;
    const alerts = Array.from(
      container.querySelectorAll<HTMLElement>(".n-alert"),
    );
    const rects = alerts.map((alert) => alert.getBoundingClientRect());
    const gaps: number[] = [];
    for (let index = 1; index < rects.length; index += 1) {
      gaps.push(
        Number((rects[index]!.top - rects[index - 1]!.bottom).toFixed(2)),
      );
    }
    const last = rects[rects.length - 1]!;
    const actions = head.querySelector<HTMLElement>(".page-actions");
    return {
      count: alerts.length,
      gaps,
      bottomGap: Number(
        (head.getBoundingClientRect().top - last.bottom).toFixed(2),
      ),
      minSpace: parseFloat(
        getComputedStyle(document.documentElement).getPropertyValue("--space-3"),
      ),
      bannerBottom: last.bottom,
      actionsTop: actions ? actions.getBoundingClientRect().top : null,
    };
  });
}

const detailViewports = [
  { width: 1366, height: 900 },
  { width: 390, height: 844 },
];

for (const bannerCase of bannerCases) {
  test(`${bannerCase.name} banner does not touch the page header`, async ({
    page,
  }) => {
    test.setTimeout(120_000);
    await mockApi(page, bannerCase.role ?? "owner", bannerCase.overrides);

    for (const viewport of detailViewports) {
      await page.setViewportSize(viewport);
      await page.goto(`${baseURL}${bannerCase.path}`);
      await page.locator(".page-head h1").first().waitFor();
      const banner = page.locator(".page-banners .n-alert").first();
      await expect(banner).toContainText(bannerCase.text);

      const layout = await measureBanners(page);
      expect(
        layout.count,
        `${bannerCase.name} banner count at ${viewport.width}px`,
      ).toBe(1);
      expect(
        layout.minSpace,
        `${bannerCase.name} space token resolves`,
      ).toBe(12);
      expect(
        layout.bottomGap,
        `${bannerCase.name} banner to header gap at ${viewport.width}px`,
      ).toBeGreaterThanOrEqual(layout.minSpace);
      for (const gap of layout.gaps) {
        expect(
          gap,
          `${bannerCase.name} banner to banner gap at ${viewport.width}px`,
        ).toBeGreaterThanOrEqual(layout.minSpace);
      }
      expect(
        layout.actionsTop,
        `${bannerCase.name} header actions at ${viewport.width}px`,
      ).not.toBeNull();
      expect(
        layout.actionsTop!,
        `${bannerCase.name} header actions stay below the banner at ${viewport.width}px`,
      ).toBeGreaterThanOrEqual(layout.bannerBottom);
    }
  });
}

test("server overview puts usage beside node info and stacks on phones", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await mockApi(page);

  await page.setViewportSize({ width: 1366, height: 900 });
  await page.goto(`${baseURL}/servers/srv-1`);
  await page.locator(".overview-grid").waitFor();

  const desktop = await page.evaluate(() => {
    const grid = document.querySelector<HTMLElement>(".overview-grid")!;
    const usage = grid.querySelector<HTMLElement>(".usage-panel")!;
    const node = grid.querySelector<HTMLElement>(".node-info-panel")!;
    const usageRect = usage.getBoundingClientRect();
    const nodeRect = node.getBoundingClientRect();
    return {
      usageWidth: usageRect.width,
      nodeWidth: nodeRect.width,
      usageTop: usageRect.top,
      nodeTop: nodeRect.top,
      usageRight: usageRect.right,
      nodeLeft: nodeRect.left,
    };
  });

  expect(desktop.nodeWidth / desktop.usageWidth, "usage is 1/3 of the row").toBeGreaterThan(1.8);
  expect(desktop.nodeWidth / desktop.usageWidth, "usage is 1/3 of the row").toBeLessThan(2.2);
  expect(desktop.usageTop, "usage and node info share a row").toBeCloseTo(desktop.nodeTop, 0);
  expect(desktop.usageRight).toBeLessThanOrEqual(desktop.nodeLeft + 1);

  const usageRows = page.locator(".usage-panel .usage-row");
  await expect(usageRows).toHaveCount(3);
  await expect(usageRows.first().locator(".usage-block")).toHaveCount(50);
  await expect(usageRows.first().locator(".usage-block.is-filled")).toHaveCount(6);
  await expect(page.locator(".node-info-panel")).not.toContainText("CPU usage");
  await expect(page.locator(".node-info-panel")).not.toContainText("Memory usage");
  await expect(page.locator(".node-info-panel")).not.toContainText("Disk usage");
  await expect(page.locator(".usage-panel")).toContainText("Stale");

  await page.setViewportSize({ width: 390, height: 844 });
  const mobile = await page.evaluate(() => {
    const grid = document.querySelector<HTMLElement>(".overview-grid")!;
    const usage = grid.querySelector<HTMLElement>(".usage-panel")!;
    const node = grid.querySelector<HTMLElement>(".node-info-panel")!;
    const usageRect = usage.getBoundingClientRect();
    const nodeRect = node.getBoundingClientRect();
    return {
      usageTop: usageRect.top,
      nodeTop: nodeRect.top,
      usageLeft: usageRect.left,
      nodeLeft: nodeRect.left,
      usageRight: usageRect.right,
      nodeRight: nodeRect.right,
      overflow:
        document.documentElement.scrollWidth >
        document.documentElement.clientWidth + 1,
    };
  });

  expect(mobile.usageTop, "usage stacks above node info").toBeLessThan(
    mobile.nodeTop,
  );
  expect(mobile.usageLeft).toBeCloseTo(mobile.nodeLeft, 0);
  expect(mobile.usageRight).toBeCloseTo(mobile.nodeRight, 0);
  expect(mobile.overflow, "no horizontal overflow at 390px").toBe(false);
});

test("server usage shows a dash instead of a fake zero when nothing was reported", async ({
  page,
}) => {
  await mockApi(page, "owner", {
    server: { cpu_usage: null, mem_usage: null, disk_usage: null },
  });
  await page.setViewportSize({ width: 1366, height: 900 });
  await page.goto(`${baseURL}/servers/srv-1`);
  await page.locator(".overview-grid").waitFor();

  const usageRows = page.locator(".usage-panel .usage-row");
  await expect(usageRows).toHaveCount(3);
  for (let index = 0; index < 3; index += 1) {
    await expect(usageRows.nth(index)).toContainText("—");
    await expect(usageRows.nth(index).locator('[role="meter"]')).toHaveCount(0);
    await expect(usageRows.nth(index).locator(".usage-block.is-filled")).toHaveCount(0);
  }
  await expect(page.locator(".usage-panel")).not.toContainText("0%");
});

test("server usage refreshes in place from the shared poll", async ({ page }) => {
  test.setTimeout(120_000);
  await mockApi(page);

  let reads = 0;
  await page.route("**/api/v1/servers", async (route) => {
    reads += 1;
    const current = reads === 1 ? server : { ...server, cpu_usage: 0.62 };
    await route.fulfill(json(200, { servers: [current] }));
  });

  await page.setViewportSize({ width: 1366, height: 900 });
  await page.goto(`${baseURL}/servers/srv-1`);
  const cpuRow = page.locator(".usage-panel .usage-row").first();
  await expect(cpuRow).toContainText("12%");

  await expect(cpuRow).toContainText("62%", { timeout: 15_000 });
  await expect(cpuRow.locator('[role="meter"]')).toHaveAttribute(
    "aria-valuenow",
    "62",
  );
});

test("a page without banners keeps its header position", async ({ page }) => {
  await mockApi(page);
  await page.goto(`${baseURL}${detailPaths.database}`);
  await page.locator(".page-head h1").first().waitFor();

  const layout = await page.evaluate(() => {
    const container = document.querySelector<HTMLElement>(".page-banners");
    const head = document.querySelector<HTMLElement>(".page-head")!;
    return {
      containerHeight: container
        ? container.getBoundingClientRect().height
        : null,
      containerMargin: container
        ? getComputedStyle(container).marginBottom
        : null,
      headTop: head.getBoundingClientRect().top,
      containerBottom: container
        ? container.getBoundingClientRect().bottom
        : null,
    };
  });

  expect(layout.containerHeight).toBe(0);
  expect(layout.containerMargin).toBe("0px");
  expect(
    Math.abs(layout.headTop - layout.containerBottom!),
    "header starts right where the empty banner container ends",
  ).toBeLessThanOrEqual(0.5);
});

interface SettingsCardBox {
  x: number;
  y: number;
  width: number;
  height: number;
  overflows: boolean;
}

async function settingsCardBox(locator: Locator): Promise<SettingsCardBox> {
  const box = await locator.boundingBox();
  if (box === null) {
    throw new Error("expected a visible settings card");
  }
  const overflows = await locator.evaluate(
    (element) => element.scrollWidth > element.clientWidth + 1,
  );
  return { ...box, overflows };
}

async function openSettings(page: Page, path: string): Promise<void> {
  await page.goto(`${baseURL}${path}`);
  await page.locator(".page-head h1").first().waitFor();
  await page.locator(".n-tabs-tab").filter({ hasText: /^Settings$/ }).click();
  await expect(page.locator(".settings-grid")).toBeVisible();
}

test("Resource settings places Server and environment beside Runtime health", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await mockApi(page);

  for (const width of [769, 1024, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await openSettings(page, detailPaths.application);
    const move = page
      .locator(".settings-grid .n-card")
      .filter({ hasText: "Server and environment" });
    const runtime = page
      .locator(".settings-grid .n-card")
      .filter({ hasText: "Runtime health" });
    await expect(move).toHaveCount(1);
    await expect(runtime).toHaveCount(1);

    const [moveBox, runtimeBox] = await Promise.all([
      settingsCardBox(move),
      settingsCardBox(runtime),
    ]);
    expect(
      Math.abs(moveBox.y - runtimeBox.y),
      `cards share a row at ${width}px`,
    ).toBeLessThanOrEqual(1);
    expect(
      Math.abs(moveBox.width - runtimeBox.width),
      `cards are equal width at ${width}px`,
    ).toBeLessThanOrEqual(1);
    expect(
      Math.abs(moveBox.height - runtimeBox.height),
      `cards share a row height at ${width}px`,
    ).toBeLessThanOrEqual(1);
    expect(
      runtimeBox.x,
      `Runtime health sits right of Server and environment at ${width}px`,
    ).toBeGreaterThan(moveBox.x);
    expect(moveBox.overflows, `move card fits at ${width}px`).toBe(false);
    expect(runtimeBox.overflows, `runtime card fits at ${width}px`).toBe(false);

    const save = await move.locator(".actions .n-button").boundingBox();
    if (save === null) {
      throw new Error("expected the move card save button");
    }
    expect(
      save.y - moveBox.y,
      `move save button stays at the card bottom at ${width}px`,
    ).toBeGreaterThan(moveBox.height / 2);
    expect(save.y + save.height).toBeLessThanOrEqual(
      moveBox.y + moveBox.height,
    );
  }

  await page.setViewportSize({ width: 390, height: 844 });
  await openSettings(page, detailPaths.application);
  const move = page
    .locator(".settings-grid .n-card")
    .filter({ hasText: "Server and environment" });
  const runtime = page
    .locator(".settings-grid .n-card")
    .filter({ hasText: "Runtime health" });
  const [moveBox, runtimeBox] = await Promise.all([
    settingsCardBox(move),
    settingsCardBox(runtime),
  ]);
  expect(
    runtimeBox.y,
    "Runtime health stacks under Server and environment at 390px",
  ).toBeGreaterThanOrEqual(moveBox.y + moveBox.height - 1);
  expect(Math.abs(moveBox.x - runtimeBox.x)).toBeLessThanOrEqual(1);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    ),
  ).toBeLessThanOrEqual(1);
});

test("Database settings keeps its extra cards inside the same grid", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await mockApi(page);
  await page.setViewportSize({ width: 1280, height: 900 });
  await openSettings(page, detailPaths.database);
  await expect(
    page
      .locator(".settings-grid .n-card")
      .filter({ hasText: "Server and environment" }),
  ).toHaveCount(1);
  await expect(
    page.locator(".settings-grid .n-card").filter({ hasText: "Runtime health" }),
  ).toHaveCount(1);
  await expect(
    page.locator(".settings-grid .n-card").filter({ hasText: "Rename database" }),
  ).toHaveCount(1);

  await page.setViewportSize({ width: 390, height: 844 });
  await openSettings(page, detailPaths.database);
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    ),
  ).toBeLessThanOrEqual(1);
});

test("A settings tab without slot content keeps the move card full width", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await mockApi(page);
  await page.setViewportSize({ width: 1280, height: 900 });
  await openSettings(page, detailPaths.service);

  await expect(page.locator(".settings-grid__slot")).toHaveCount(0);
  const gridBox = await page.locator(".settings-grid").boundingBox();
  const moveBox = await page
    .locator(".settings-grid .n-card")
    .filter({ hasText: "Server and environment" })
    .boundingBox();
  if (gridBox === null || moveBox === null) {
    throw new Error("expected the settings grid and move card");
  }
  expect(Math.abs(moveBox.width - gridBox.width)).toBeLessThanOrEqual(1);
});


test("dashboard keeps projects and servers in the main column and deploys beside them", async ({
  page,
}) => {
  test.setTimeout(120_000);
  await mockApi(page);
  await page.setViewportSize({ width: 1366, height: 900 });
  await page.goto(`${baseURL}/dashboard`);
  await page.locator(".dash-columns").waitFor();
  await page.locator(".project-card-link").first().waitFor();

  const desktop = await page.evaluate(() => {
    const main = document.querySelector<HTMLElement>(".dash-main")!;
    const side = document.querySelector<HTMLElement>(".dash-side")!;
    const mainRect = main.getBoundingClientRect();
    const sideRect = side.getBoundingClientRect();
    return {
      mainWidth: mainRect.width,
      sideWidth: sideRect.width,
      mainRight: mainRect.right,
      sideLeft: sideRect.left,
      mainTop: mainRect.top,
      sideTop: sideRect.top,
    };
  });

  expect(desktop.mainWidth / desktop.sideWidth, "main column is 2/3").toBeGreaterThan(1.8);
  expect(desktop.mainWidth / desktop.sideWidth, "deploys column is 1/3").toBeLessThan(2.2);
  expect(desktop.mainRight).toBeLessThanOrEqual(desktop.sideLeft + 1);
  expect(desktop.sideTop, "deploys start beside the main content").toBeCloseTo(desktop.mainTop, 0);

  await expect(page.locator(".project-grid .project-card-link")).toHaveCount(6);
  const firstProject = page.getByRole("link", {
    name: "Open project project-1",
    exact: true,
  });
  await expect(firstProject).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Open project project-7", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "view all projects", exact: true }),
  ).toHaveAttribute("href", "/projects");

  const serverLink = page.getByRole("link", {
    name: `Open server ${longName}`,
    exact: true,
  });
  await expect(serverLink).toBeVisible();
  await expect(serverLink).toHaveAttribute("href", "/servers/srv-1");
  await expect(page.locator(".dash-side")).toContainText("Recent deploys");
  await expect(page.locator(".dash-main")).not.toContainText("Recent deploys");

  const meter = serverLink.locator(".usage-segments").first();
  await expect(meter.locator(".usage-block")).toHaveCount(50);
  const block = await meter.locator(".usage-block").first().boundingBox();
  if (block === null) {
    throw new Error("expected a usage block");
  }
  expect(Math.abs(block.width - block.height), "usage blocks are square").toBeLessThanOrEqual(1);
  expect(await meter.evaluate((element) => getComputedStyle(element).gap)).toBe("2px");

  await firstProject.click();
  await expect(page).toHaveURL(new RegExp(`/projects/${dashboardProjects[0].id}$`));
  await page.goBack();
  await page.locator(".dash-columns").waitFor();
  await page.getByRole("link", {
    name: `Open server ${longName}`,
    exact: true,
  }).click();
  await expect(page).toHaveURL(/\/servers\/srv-1$/);

  for (const width of [1366, 1280, 480, 390]) {
    await page.goto(`${baseURL}/dashboard`);
    await page.locator(".dash-columns").waitFor();
    await page.setViewportSize({ width, height: 900 });
    await waitForLayoutSettled(page);
    const layout = await page.evaluate(() => {
      const main = document.querySelector<HTMLElement>(".dash-main")!;
      const side = document.querySelector<HTMLElement>(".dash-side")!;
      const mainRect = main.getBoundingClientRect();
      const sideRect = side.getBoundingClientRect();
      return {
        overflow:
          document.documentElement.scrollWidth >
          document.documentElement.clientWidth + 1,
        mainTop: mainRect.top,
        mainLeft: mainRect.left,
        sideTop: sideRect.top,
        sideLeft: sideRect.left,
      };
    });
    expect(layout.overflow, `no horizontal overflow at ${width}px`).toBe(false);
    if (width <= 1024) {
      expect(layout.sideTop, `deploys stack under the main column at ${width}px`).toBeGreaterThan(
        layout.mainTop,
      );
      expect(layout.sideLeft, `columns share the left edge at ${width}px`).toBeCloseTo(
        layout.mainLeft,
        0,
      );
    } else {
      expect(layout.sideTop, `deploys stay beside the main column at ${width}px`).toBeCloseTo(
        layout.mainTop,
        0,
      );
    }
  }
});

test("dashboard projects show an empty state at zero and a single card at one", async ({
  page,
}) => {
  await mockApi(page, "owner", { projects: [] });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto(`${baseURL}/dashboard`);
  await page.locator(".dash-columns").waitFor();
  await expect(page.getByText("No projects yet", { exact: true })).toBeVisible();
  const openProjects = page.getByRole("button", {
    name: "Open projects",
    exact: true,
  });
  await expect(openProjects).toBeVisible();
  await openProjects.click();
  await expect(page).toHaveURL(/\/projects$/);
  await expect(page.locator(".project-card-link")).toHaveCount(0);

  await mockApi(page, "owner", { projects: [dashboardProjects[0]!] });
  await page.goto(`${baseURL}/dashboard`);
  await page.locator(".project-card-link").first().waitFor();
  await expect(page.locator(".project-card-link")).toHaveCount(1);
  await expect(
    page.getByRole("link", { name: "Open project project-1", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Open project project-2", exact: true }),
  ).toHaveCount(0);
});
