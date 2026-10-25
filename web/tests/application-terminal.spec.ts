import { Buffer } from "node:buffer";
import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page, WebSocketRoute } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, URL:readonly, window:readonly */

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

const PROJECT_ID = "11111111-1111-4111-8111-111111111111";
const ENV_ID = "22222222-2222-4222-8222-222222222222";
const APP_ID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const APP_B_ID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const SERVER_ID = "33333333-3333-4333-8333-333333333333";

const USER = {
  id: "u-1",
  email: "ada@gotham.dev",
  created_at: "2026-03-04T12:00:00Z",
  display_name: "Ada",
  has_password: true,
  role: "owner",
};

let currentRole: "owner" | "member" = "owner";

const application = {
  id: APP_ID,
  name: "shop",
  environment_id: ENV_ID,
  environment_name: "production",
  project_id: PROJECT_ID,
  project_name: "storefront",
  provider: "github",
  repo: "acme/shop",
  clone_url: "https://github.com/acme/shop.git",
  source_type: "compose",
  branch: "main",
  build_pack: "auto",
  compose_service: "web",
  compose_file: "",
  compose_content: "",
  image_ref: "",
  has_registry_credential: false,
  primary_domain: "",
  server_id: SERVER_ID,
  server_name: "prod-01",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

const deployment = {
  id: "dep-1",
  application_id: APP_ID,
  kind: "deploy",
  state: "running",
  image_tag: "shop:1",
  registry_image: "registry/shop:1",
  digest: "sha256:1",
  error: "",
  attempt: 1,
  container_id: "container-1",
  rollback_from: "",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

function target(service: string, id: string, name: string, replica = 1) {
  return {
    service,
    container_id: id,
    name,
    image: "nginx:1",
    state: "running",
    replica,
    label: service === "" ? name : `${service} · ${name}`,
  };
}

const SINGLE = [target("", "cid-single", "gotham-shop-1")];
const MULTI = [
  target("db", "cid-db", "gotham-shop-db-1"),
  target("web", "cid-web-1", "gotham-shop-web-1", 1),
  target("web", "cid-web-2", "gotham-shop-web-2", 2),
];

interface Ledger {
  targets: number;
  creates: Array<{ service: string; container_id: string }>;
  deletes: number;
  inputs: string[];
}

function freshLedger(): Ledger {
  return { targets: 0, creates: [], deletes: 0, inputs: [] };
}

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

function frame(opcode: number, payload: Buffer = Buffer.alloc(0)): Buffer {
  return Buffer.concat([Buffer.from([opcode]), payload]);
}

function openFrame(): Buffer {
  return frame(
    4,
    Buffer.from(
      JSON.stringify({
        shell: "/bin/sh",
        cols: 120,
        rows: 32,
        service_user: "app",
      }),
      "utf8",
    ),
  );
}

function outputFrame(text: string): Buffer {
  return frame(1, Buffer.from(text, "utf8"));
}

async function mockApi(
  page: Page,
  ledger: Ledger,
  options: {
    role?: "owner" | "member";
    containers?: ReturnType<typeof target>[];
  } = {},
): Promise<void> {
  currentRole = options.role ?? "owner";
  const containers = options.containers ?? SINGLE;
  const sockets = new Map<string, WebSocketRoute>();

  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: { ...USER, role: currentRole } });
  });
  await page.route("**/api/v1/version", async (route) => {
    await route.fulfill({ json: { version: "v0.2.1-dev" } });
  });
  await page.route("**/api/v1/servers", async (route) => {
    await route.fulfill({ json: { servers: [] } });
  });
  await page.route(`**/api/v1/applications/${APP_ID}`, async (route) => {
    await route.fulfill({ json: { application } });
  });
  await page.route(
    `**/api/v1/applications/${APP_ID}/deployments**`,
    async (route) => {
      await route.fulfill({ json: { deployments: [deployment] } });
    },
  );
  await page.route(`**/api/v1/applications/${APP_ID}/env`, async (route) => {
    await route.fulfill({ json: { env: [] } });
  });
  await page.route(
    `**/api/v1/applications/${APP_ID}/storages`,
    async (route) => {
      await route.fulfill({ json: { storage: [] } });
    },
  );
  await page.route(
    `**/api/v1/applications/${APP_ID}/previews`,
    async (route) => {
      await route.fulfill({ json: { previews: [] } });
    },
  );
  await page.route(
    `**/api/v1/applications/${APP_ID}/terminal/targets`,
    async (route) => {
      ledger.targets += 1;
      await route.fulfill({
        json: {
          application_id: APP_ID,
          application_name: "shop",
          server_id: SERVER_ID,
          source_type: "compose",
          containers,
        },
      });
    },
  );
  await page.route(
    `**/api/v1/applications/${APP_ID}/terminal/sessions**`,
    async (route) => {
      const request = route.request();
      if (request.method() === "DELETE") {
        ledger.deletes += 1;
        await route.fulfill({ status: 204, body: "" });
        return;
      }
      if (request.method() !== "POST") {
        await route.fulfill({ json: {} });
        return;
      }
      const input = request.postDataJSON() as {
        service: string;
        container_id: string;
        cols: number;
        rows: number;
      };
      ledger.creates.push({
        service: input.service,
        container_id: input.container_id,
      });
      const id = `session-${ledger.creates.length}`;
      const row = containers.find(
        (item) => item.container_id === input.container_id,
      );
      await route.fulfill({
        status: 201,
        json: {
          id,
          server_id: SERVER_ID,
          target: "container",
          cols: input.cols,
          rows: input.rows,
          expires_at: "2026-10-20T12:01:00Z",
          websocket_path: `/api/v1/applications/${APP_ID}/terminal/sessions/${id}/ws`,
          subprotocol: "gotham.terminal.v1",
          application_id: APP_ID,
          ...(input.service !== "" ? { service: input.service } : {}),
          container_id: input.container_id,
          container_name: row?.name ?? input.container_id,
          target_label: row?.label ?? input.container_id,
        },
      });
    },
  );

  await page.routeWebSocket(
    /\/api\/v1\/applications\/[^/]+\/terminal\/sessions\/[^/]+\/ws$/,
    (ws) => {
      const path = new URL(ws.url()).pathname;
      const id = path.split("/").at(-2) ?? "";
      sockets.set(id, ws);
      ws.onMessage((message) => {
        const bytes =
          typeof message === "string"
            ? Buffer.from(message, "utf8")
            : Buffer.from(message);
        if (bytes.length > 1 && bytes[0] === 1) {
          ledger.inputs.push(bytes.subarray(1).toString("utf8"));
        }
      });
      ws.send(openFrame());
      ws.send(outputFrame(`welcome ${id}\r\n$ `));
    },
  );
}

async function seedApp(page: Page, path: string): Promise<void> {
  await page.addInitScript(
    (value) => {
      window.localStorage.setItem("gotham.auth.session", value);
    },
    JSON.stringify({
      user: { ...USER, role: currentRole },
      accessToken: "test-access",
      refreshToken: "test-refresh",
    }),
  );
  await page.goto(`${baseURL}${path}`);
  await expect(page.locator(".topbar")).toBeVisible();
}

async function terminalTabs(page: Page) {
  return page.evaluate(() => {
    const app = (document.querySelector("#app") as unknown as {
      __vue_app__: {
        config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
      };
    }).__vue_app__;
    const store = app.config.globalProperties.$pinia._s.get("terminal") as {
      tabs: Array<{
        target: string;
        applicationId: string;
        service: string;
        containerId: string;
        targetLabel: string;
        title: string;
        sessionId: string;
        mode: string;
      }>;
    };
    return store.tabs.map((tab) => ({
      target: tab.target,
      applicationId: tab.applicationId,
      service: tab.service,
      containerId: tab.containerId,
      targetLabel: tab.targetLabel,
      title: tab.title,
      sessionId: tab.sessionId,
      mode: tab.mode,
    }));
  });
}

async function navigate(page: Page, path: string): Promise<void> {
  await page.evaluate(async (next: string) => {
    const app = (document.querySelector("#app") as unknown as {
      __vue_app__: {
        config: { globalProperties: { $router: { push: (_path: string) => Promise<void> } } };
      };
    }).__vue_app__;
    await app.config.globalProperties.$router.push(next);
  }, path);
}


const detailPath = `/projects/${PROJECT_ID}/environments/${ENV_ID}/applications/${APP_ID}`;
const detailBPath = `/projects/${PROJECT_ID}/environments/${ENV_ID}/applications/${APP_B_ID}`;

test("the Terminal tab sits after Environment and opens one exact container shell", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger);
  await seedApp(page, detailPath);

  await expect(page.locator(".n-tabs-tab", { hasText: "Environment" })).toBeVisible();
  expect(ledger.targets).toBe(0);
  expect(ledger.creates).toHaveLength(0);
  await expect(page.locator(".app-term-select")).toHaveCount(0);
  const labels = await page.locator(".n-tabs-tab").allInnerTexts();
  const env = labels.indexOf("Environment");
  expect(env).toBeGreaterThanOrEqual(0);
  expect(labels.indexOf("Terminal")).toBe(env + 1);

  await page.locator(".n-tabs-tab", { hasText: "Terminal" }).click();
  await expect.poll(() => ledger.creates.length).toBe(1);
  expect(ledger.targets).toBe(1);
  expect(ledger.creates[0]).toEqual({ service: "", container_id: "cid-single" });
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  await expect(page.locator(".term-surface")).toContainText("gotham-shop-1");

  await expect(page.locator(".app-term-toolbar .app-term-refresh")).toBeVisible();
  await page.locator(".app-term-toolbar .app-term-refresh").click();
  await expect.poll(() => ledger.targets).toBe(2);
  expect(ledger.creates).toHaveLength(1);

  const tabs = await terminalTabs(page);
  expect(tabs).toHaveLength(1);
  expect(tabs[0]).toMatchObject({
    target: "container",
    applicationId: APP_ID,
    service: "",
    containerId: "cid-single",
    targetLabel: "gotham-shop-1",
    title: "shop · gotham-shop-1",
    mode: "page",
  });

  await page.locator(".term-surface .xterm-screen").click();
  await page.keyboard.type("echo app");
  await expect(page.locator(".term-surface .xterm-rows")).toContainText("welcome session-1");

  const before = (await terminalTabs(page))[0]!;
  await page.locator('a[href="/dashboard"]').first().click();
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.locator(".rail-btn--terminal .rail-badge")).toHaveText("1");
  const dock = page.locator(".term-dock");
  await expect(dock).toBeVisible();
  await expect(dock).toContainText("shop · gotham-shop-1");
  await expect(dock).toContainText("Paused");
  expect(ledger.creates).toHaveLength(1);
  await dock.locator(".term-dock-resume").click();
  await expect(page).toHaveURL(new RegExp(`${APP_ID}\\?tab=terminal`));
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  const resumed = (await terminalTabs(page))[0]!;
  expect(resumed.sessionId).toBe(before.sessionId);
  expect(ledger.creates).toHaveLength(1);

  const targetReads = ledger.targets;
  const overview = page.locator(".n-tabs-tab", { hasText: "Overview" });
  const terminalTab = page.locator(".n-tabs-tab", { hasText: "Terminal" });
  for (let index = 0; index < 3; index += 1) {
    await overview.click();
    await terminalTab.click();
  }
  await terminalTab.dblclick();
  expect(ledger.targets).toBe(targetReads);
  expect(ledger.creates).toHaveLength(1);

  await page.locator('.term-surface [aria-label="Close terminal"]').click();
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();
  await expect(page.locator(".term-stage-closed")).toBeVisible();
  await expect.poll(() => ledger.deletes).toBe(1);
  expect(ledger.creates).toHaveLength(1);
  await overview.click();
  await terminalTab.click();
  await expect(page.locator(".term-stage-closed")).toBeVisible();
  expect(ledger.creates).toHaveLength(1);

  await page
    .locator(".term-stage-closed")
    .getByRole("button", { name: "Start new session" })
    .click();
  await expect.poll(() => ledger.creates.length).toBe(2);
  expect(ledger.creates[1]).toEqual({ service: "", container_id: "cid-single" });
});

for (const width of [390, 480, 1280, 1366]) {
  test(`Environment → Terminal ordering and layout at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    const ledger = freshLedger();
    await mockApi(page, ledger);
    await seedApp(page, detailPath);
    await expect(page.locator(".n-tabs-tab", { hasText: "Environment" })).toBeVisible();
    const labels = await page.locator(".n-tabs-tab").allInnerTexts();
    const env = labels.indexOf("Environment");
    expect(env).toBeGreaterThanOrEqual(0);
    expect(labels.indexOf("Terminal")).toBe(env + 1);

    await page.locator(".n-tabs-tab", { hasText: "Terminal" }).click();
    await expect.poll(() => ledger.creates.length).toBe(1);
    await expect(page.locator(".term-surface")).toBeVisible();

    const metaFits = await page.locator(".term-meta").evaluate((element) => {
      const node = element as HTMLElement;
      return node.scrollWidth <= node.clientWidth + 1;
    });
    expect(metaFits).toBe(true);
    const noHorizontalScroll = await page.evaluate(
      () => document.documentElement.scrollWidth <= window.innerWidth + 1,
    );
    expect(noHorizontalScroll).toBe(true);
  });
}

test("multiple services and replicas need an explicit pick and switch only after confirmation", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger, { containers: MULTI });
  await seedApp(page, `${detailPath}?tab=terminal`);

  await expect(page.locator(".app-term-select-card")).toBeVisible();
  expect(ledger.targets).toBe(1);
  expect(ledger.creates).toHaveLength(0);

  await page.locator(".app-term-select").click();
  const options = page.locator(".n-base-select-option");
  await expect(options).toHaveCount(3);
  await expect(options.nth(0)).toContainText("db · gotham-shop-db-1");
  await expect(options.nth(1)).toContainText("web · gotham-shop-web-1");
  await expect(options.nth(2)).toContainText("web · gotham-shop-web-2");
  await options.nth(1).click();
  await expect.poll(() => ledger.creates.length).toBe(1);
  expect(ledger.creates[0]).toEqual({
    service: "web",
    container_id: "cid-web-1",
  });
  expect((await terminalTabs(page))[0]?.targetLabel).toBe(
    "web · gotham-shop-web-1",
  );

  await page.locator(".app-term-select").click();
  await page
    .locator(".n-base-select-option")
    .filter({ hasText: "db · gotham-shop-db-1" })
    .click();
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Keep terminals" }).click();
  await expect(dialog).toBeHidden();
  expect(ledger.deletes).toBe(0);
  expect(ledger.creates).toHaveLength(1);
  await expect(page.locator(".term-surface")).toBeVisible();

  await page.locator(".app-term-select").click();
  await page
    .locator(".n-base-select-option")
    .filter({ hasText: "db · gotham-shop-db-1" })
    .click();
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();
  await expect.poll(() => ledger.creates.length).toBe(2);
  expect(ledger.deletes).toBe(1);
  expect(ledger.creates[1]).toEqual({ service: "db", container_id: "cid-db" });
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  await expect
    .poll(async () => (await terminalTabs(page))[0]?.sessionId)
    .toBe("session-2");
  const tabs = await terminalTabs(page);
  expect(tabs).toHaveLength(1);
  expect(tabs[0]).toMatchObject({
    service: "db",
    containerId: "cid-db",
    targetLabel: "db · gotham-shop-db-1",
    sessionId: "session-2",
  });
});

test("a member sees no enabled terminal tab and issues zero requests", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger, { role: "member" });
  await seedApp(page, `${detailPath}?tab=terminal`);

  await expect(page.locator(".app-blocked-terminal")).toBeVisible();
  await expect(page.locator(".app-blocked-terminal")).toContainText(
    "Terminal not available",
  );
  const labels = await page.locator(".n-tabs-tab").allInnerTexts();
  expect(labels).not.toContain("Terminal");
  expect(ledger.targets).toBe(0);
  expect(ledger.creates).toHaveLength(0);
  await expect(page.locator(".term-surface")).toHaveCount(0);
});

test("a target discovery failure is bounded, explicit and never auto-retries", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger);
  await page.route(
    `**/api/v1/applications/${APP_ID}/terminal/targets`,
    async (route) => {
      ledger.targets += 1;
      await route.fulfill({
        status: 409,
        json: { message: "the application has no running deployment" },
      });
    },
  );
  await seedApp(page, `${detailPath}?tab=terminal`);

  await expect(
    page.getByText("This application has no running deployment."),
  ).toBeVisible();
  expect(ledger.targets).toBe(1);
  expect(ledger.creates).toHaveLength(0);
  await page.waitForTimeout(1000);
  expect(ledger.targets).toBe(1);
  await page.locator(".app-term-retry").click();
  await expect.poll(() => ledger.targets).toBe(2);
  expect(ledger.creates).toHaveLength(0);
});

test("a replaced container is rediscovered and recreated with the new id", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger);
  let reads = 0;
  const rows = [
    target("", "cid-single", "gotham-shop-1"),
    target("", "cid-new", "gotham-shop-2"),
  ];
  await page.route(
    `**/api/v1/applications/${APP_ID}/terminal/targets`,
    async (route) => {
      ledger.targets += 1;
      const row = rows[Math.min(reads, rows.length - 1)]!;
      reads += 1;
      await route.fulfill({
        json: {
          application_id: APP_ID,
          application_name: "shop",
          server_id: SERVER_ID,
          source_type: "image",
          containers: [row],
        },
      });
    },
  );
  await page.route(
    `**/api/v1/applications/${APP_ID}/terminal/sessions**`,
    async (route) => {
      const request = route.request();
      if (request.method() === "DELETE") {
        ledger.deletes += 1;
        await route.fulfill({ status: 204, body: "" });
        return;
      }
      const input = request.postDataJSON() as {
        service: string;
        container_id: string;
        cols: number;
        rows: number;
      };
      ledger.creates.push({
        service: input.service,
        container_id: input.container_id,
      });
      if (ledger.creates.length === 1) {
        await route.fulfill({
          status: 404,
          json: {
            message:
              "the selected container is no longer available; refresh the targets",
          },
        });
        return;
      }
      const id = `session-${ledger.creates.length}`;
      await route.fulfill({
        status: 201,
        json: {
          id,
          server_id: SERVER_ID,
          target: "container",
          cols: input.cols,
          rows: input.rows,
          expires_at: "2026-10-20T12:01:00Z",
          websocket_path: `/api/v1/applications/${APP_ID}/terminal/sessions/${id}/ws`,
          subprotocol: "gotham.terminal.v1",
          application_id: APP_ID,
          container_id: input.container_id,
          container_name: "gotham-shop-2",
          target_label: "gotham-shop-2",
        },
      });
    },
  );
  await seedApp(page, `${detailPath}?tab=terminal`);

  await expect.poll(() => ledger.creates.length).toBe(1);
  expect(ledger.creates[0]).toEqual({
    service: "",
    container_id: "cid-single",
  });
  await expect(page.locator(".term-surface")).toContainText(
    "The selected container is no longer available.",
  );

  await page
    .getByRole("button", { name: "Start new session" })
    .click();
  await expect.poll(() => ledger.creates.length).toBe(2);
  expect(ledger.targets).toBe(2);
  expect(ledger.creates[1]).toEqual({
    service: "",
    container_id: "cid-new",
  });
  await expect(page.locator(".term-surface .term-state")).toHaveText(
    "Connected",
  );
  await expect
    .poll(async () => (await terminalTabs(page))[0]?.containerId)
    .toBe("cid-new");
});

test("a live shell whose container vanished recovers through rediscovery", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger);
  let reads = 0;
  const rows = [
    target("", "cid-single", "gotham-shop-1"),
    target("", "cid-new", "gotham-shop-2"),
  ];
  await page.route(
    `**/api/v1/applications/${APP_ID}/terminal/targets`,
    async (route) => {
      ledger.targets += 1;
      const row = rows[Math.min(reads, rows.length - 1)]!;
      reads += 1;
      await route.fulfill({
        json: {
          application_id: APP_ID,
          application_name: "shop",
          server_id: SERVER_ID,
          source_type: "image",
          containers: [row],
        },
      });
    },
  );
  await page.route(
    `**/api/v1/applications/${APP_ID}/terminal/sessions**`,
    async (route) => {
      const request = route.request();
      if (request.method() === "DELETE") {
        ledger.deletes += 1;
        await route.fulfill({ status: 204, body: "" });
        return;
      }
      const input = request.postDataJSON() as {
        service: string;
        container_id: string;
        cols: number;
        rows: number;
      };
      ledger.creates.push({
        service: input.service,
        container_id: input.container_id,
      });
      const id = `session-${ledger.creates.length}`;
      await route.fulfill({
        status: 201,
        json: {
          id,
          server_id: SERVER_ID,
          target: "container",
          cols: input.cols,
          rows: input.rows,
          expires_at: "2026-10-20T12:01:00Z",
          websocket_path: `/api/v1/applications/${APP_ID}/terminal/sessions/${id}/ws`,
          subprotocol: "gotham.terminal.v1",
          application_id: APP_ID,
          container_id: input.container_id,
          container_name: "gotham-shop-1",
          target_label: "gotham-shop-1",
        },
      });
    },
  );
  await page.routeWebSocket(
    /\/api\/v1\/applications\/[^/]+\/terminal\/sessions\/[^/]+\/ws$/,
    (ws) => {
      ws.send(openFrame());
      ws.send(
        frame(
          3,
          Buffer.from(
            "the container no longer exists; redeploy the application",
            "utf8",
          ),
        ),
      );
    },
  );
  await seedApp(page, `${detailPath}?tab=terminal`);

  await expect(page.locator(".term-surface .term-state")).toHaveText("Error");
  await page
    .getByRole("button", { name: "Start new session" })
    .click();
  await expect.poll(() => ledger.creates.length).toBe(2);
  expect(ledger.targets).toBe(2);
  expect(ledger.deletes).toBe(1);
  expect(ledger.creates[1]).toEqual({
    service: "",
    container_id: "cid-new",
  });
});

test("switching application keeps both container sessions isolated", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger);
  const appB = {
    ...application,
    id: APP_B_ID,
    name: "blog",
  };
  await page.route(`**/api/v1/applications/${APP_B_ID}`, async (route) => {
    await route.fulfill({ json: { application: appB } });
  });
  await page.route(
    `**/api/v1/applications/${APP_B_ID}/terminal/targets`,
    async (route) => {
      ledger.targets += 1;
      await route.fulfill({
        json: {
          application_id: APP_B_ID,
          application_name: "blog",
          server_id: SERVER_ID,
          source_type: "image",
          containers: [target("", "cid-blog", "gotham-blog-1")],
        },
      });
    },
  );
  await page.route(
    `**/api/v1/applications/${APP_B_ID}/terminal/sessions**`,
    async (route) => {
      const request = route.request();
      if (request.method() === "DELETE") {
        ledger.deletes += 1;
        await route.fulfill({ status: 204, body: "" });
        return;
      }
      const input = request.postDataJSON() as {
        service: string;
        container_id: string;
      };
      ledger.creates.push({
        service: input.service,
        container_id: input.container_id,
      });
      const id = `session-${ledger.creates.length}`;
      await route.fulfill({
        status: 201,
        json: {
          id,
          server_id: SERVER_ID,
          target: "container",
          cols: 120,
          rows: 32,
          expires_at: "2026-10-20T12:01:00Z",
          websocket_path: `/api/v1/applications/${APP_B_ID}/terminal/sessions/${id}/ws`,
          subprotocol: "gotham.terminal.v1",
          application_id: APP_B_ID,
          container_id: input.container_id,
          container_name: "gotham-blog-1",
          target_label: "gotham-blog-1",
        },
      });
    },
  );
  await seedApp(page, `${detailPath}?tab=terminal`);
  await expect.poll(() => ledger.creates.length).toBe(1);

  await navigate(page, `${detailBPath}?tab=terminal`);
  await expect.poll(() => ledger.creates.length).toBe(2);
  expect(ledger.creates[1]).toEqual({
    service: "",
    container_id: "cid-blog",
  });
  const tabs = await terminalTabs(page);
  expect(tabs).toHaveLength(2);
  expect(tabs.map((tab) => tab.applicationId).sort()).toEqual([
    APP_ID,
    APP_B_ID,
  ]);
  expect(
    tabs.find((tab) => tab.applicationId === APP_B_ID)?.targetLabel,
  ).toBe("gotham-blog-1");
  await navigate(page, `${detailPath}?tab=terminal`);
  await expect(page.locator(".term-surface .term-state")).toHaveText(
    "Connected",
  );
  expect(ledger.creates).toHaveLength(2);
});

test("detaching keeps the session, label and window tab without a new POST", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger);
  await seedApp(page, `${detailPath}?tab=terminal`);
  await expect.poll(() => ledger.creates.length).toBe(1);
  const before = (await terminalTabs(page))[0]!;

  await page.locator(".term-open-in-window").click();
  const window = page.locator(".term-window");
  await expect(window).toBeVisible();
  await expect(window).toContainText("shop · gotham-shop-1");
  await expect(page.locator(".term-detached")).toBeVisible();
  await expect(page.locator(".rail-btn--terminal .rail-badge")).toHaveText("1");
  const after = (await terminalTabs(page))[0]!;
  expect(after.mode).toBe("window");
  expect(after.sessionId).toBe(before.sessionId);
  expect(ledger.creates).toHaveLength(1);
  expect(ledger.deletes).toBe(0);
});

test("Start new session beside a minimized window shell opens one fresh page session", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger);
  let reads = 0;
  const rows = [
    target("", "cid-single", "gotham-shop-1"),
    target("", "cid-new", "gotham-shop-2"),
  ];
  await page.route(
    `**/api/v1/applications/${APP_ID}/terminal/targets`,
    async (route) => {
      ledger.targets += 1;
      const row = rows[Math.min(reads, rows.length - 1)]!;
      reads += 1;
      await route.fulfill({
        json: {
          application_id: APP_ID,
          application_name: "shop",
          server_id: SERVER_ID,
          source_type: "image",
          containers: [row],
        },
      });
    },
  );
  await seedApp(page, `${detailPath}?tab=terminal`);
  await expect.poll(() => ledger.creates.length).toBe(1);
  const before = (await terminalTabs(page))[0]!;

  await page.locator(".term-open-in-window").click();
  await expect(page.locator(".term-window")).toBeVisible();
  await page
    .locator(".term-window")
    .getByRole("button", { name: "Minimize terminal window" })
    .click();
  await expect(page.locator(".term-detached-actions")).toBeVisible();

  await page
    .locator(".term-detached")
    .getByRole("button", { name: "Start new session" })
    .click();
  await expect.poll(() => ledger.creates.length).toBe(2);
  expect(ledger.targets).toBe(2);
  expect(ledger.creates[1]).toEqual({
    service: "",
    container_id: "cid-new",
  });
  expect(ledger.deletes).toBe(0);
  await expect(page.locator(".term-surface--page .term-state")).toHaveText(
    "Connected",
  );

  const tabs = await terminalTabs(page);
  expect(tabs).toHaveLength(2);
  const retained = tabs.find((tab) => tab.sessionId === before.sessionId)!;
  expect(retained.mode).toBe("window");
  const fresh = tabs.find((tab) => tab.mode === "page")!;
  expect(fresh.containerId).toBe("cid-new");
  expect(fresh.sessionId).toBe("session-2");
  await expect(page.locator(".rail-btn--terminal .rail-badge")).toHaveText("2");
});

test("the theme menu on a container session sends no PTY input and shows no host banner", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const ledger = freshLedger();
  await mockApi(page, ledger);
  await seedApp(page, `${detailPath}?tab=terminal`);
  await expect.poll(() => ledger.creates.length).toBe(1);
  await expect(page.locator(".term-surface .term-state")).toHaveText(
    "Connected",
  );

  await expect
    .poll(() => page.locator(".term-surface .xterm-rows").innerText())
    .toContain("welcome session-1");
  const rows = await page.locator(".term-surface .xterm-rows").innerText();
  expect(rows).not.toContain("Gotham");

  await page.locator(".term-theme-btn").click();
  await page.locator(".term-theme-option", { hasText: "Gotham Light" }).click();
  await expect(page.locator(".term-surface")).toHaveAttribute(
    "data-theme",
    "gotham-light",
  );
  expect(ledger.inputs).toEqual([]);
  expect(ledger.creates).toHaveLength(1);
});

