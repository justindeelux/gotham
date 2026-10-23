import { Buffer } from "node:buffer";
import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page, Request, WebSocketRoute } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, setTimeout:readonly, URL:readonly, window:readonly */

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

const TEAM_ID = "team-1";
const SERVER_ID = "11111111-1111-4111-8111-111111111111";
const SERVER_B_ID = "22222222-2222-4222-8222-222222222222";
const APPLICATION_ID = "33333333-3333-4333-8333-333333333333";
const CONTAINER_ID = "a".repeat(64);
const CONTAINER_B_ID = "b".repeat(64);

const session = {
  user: {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-03-04T12:00:00Z",
    display_name: "Ada",
    has_password: true,
    is_platform_admin: true,
  },
  accessToken: "test-access",
  refreshToken: "test-refresh",
};

const team = {
  id: TEAM_ID,
  name: "Acme",
  is_personal: false,
  role: "owner",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

function serverRow(id: string, name: string) {
  return { id, name, ip: "10.0.0.1", status: "ready" };
}

function targetContainer(
  service: string,
  containerId: string,
  name: string,
  replica = 1,
) {
  return {
    service,
    container_id: containerId,
    name,
    image: "nginx:1.28.0-alpine",
    state: "running",
    replica,
    label: `${service} · ${name}`,
  };
}

function targetPayload(options?: {
  servers?: unknown[];
  applications?: unknown[];
  errors?: unknown[];
  truncated?: boolean;
}) {
  return {
    truncated: options?.truncated ?? false,
    servers: options?.servers ?? [
      { id: SERVER_ID, name: "prod-01", status: "ready" },
      { id: SERVER_B_ID, name: "prod-02", status: "ready" },
    ],
    applications: options?.applications ?? [
      {
        application_id: APPLICATION_ID,
        application_name: "shop",
        server_id: SERVER_ID,
        source_type: "image",
        containers: [
          targetContainer("web", CONTAINER_ID, "shop-web-1", 1),
          targetContainer("web", CONTAINER_B_ID, "shop-web-2", 2),
        ],
      },
    ],
    errors: options?.errors ?? [
      { source: "application", code: "unavailable" },
    ],
  };
}

interface FakeSession {
  id: string;
  socket: WebSocketRoute | null;
}

interface ApiState {
  creates: Array<{
    kind: "host" | "container";
    body: Record<string, unknown>;
    url: string;
    teamId: string;
  }>;
  deletes: number;
  targetsCalls: Array<{ teamId: string }>;
  sessions: FakeSession[];
  seq: number;
  targetsFailures: number;
  createDelayMs: number;
  payload: () => unknown;
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
        shell: "/bin/bash",
        cols: 120,
        rows: 32,
        service_user: "gotham-agent",
      }),
      "utf8",
    ),
  );
}

function outputFrame(text: string): Buffer {
  return frame(1, Buffer.from(text, "utf8"));
}

function newState(): ApiState {
  return {
    creates: [],
    deletes: 0,
    targetsCalls: [],
    sessions: [],
    seq: 0,
    targetsFailures: 0,
    createDelayMs: 0,
    payload: () => targetPayload(),
  };
}

function teamOf(request: Request): string {
  return request.headers()["x-team-id"] ?? "";
}

function validateBody(
  request: Request,
  kind: "host" | "container",
): string | null {
  const body = request.postDataJSON() as Record<string, unknown> | null;
  if (body === null || typeof body !== "object") {
    return "body must be an object";
  }
  const allowed =
    kind === "host"
      ? new Set(["target", "root", "cwd", "cols", "rows"])
      : new Set(["service", "container_id", "cols", "rows"]);
  for (const key of Object.keys(body)) {
    if (!allowed.has(key)) {
      return `unknown field ${key}`;
    }
  }
  if (
    typeof body.cols !== "number" ||
    typeof body.rows !== "number" ||
    body.cols <= 0 ||
    body.rows <= 0
  ) {
    return "cols/rows must be positive numbers";
  }
  if (kind === "host") {
    if (body.target !== undefined && body.target !== "host") {
      return "a host create may only target host";
    }
    return null;
  }
  if (typeof body.service !== "string") {
    return "service is required";
  }
  if (
    typeof body.container_id !== "string" ||
    !/^[0-9a-f]{64}$/.test(body.container_id)
  ) {
    return "container_id must be the full resolved id";
  }
  const payload = targetPayload() as {
    applications: Array<{ containers: Array<{ service: string; container_id: string }> }>;
  };
  const known = payload.applications[0]!.containers.some(
    (candidate) =>
      candidate.service === body.service &&
      candidate.container_id === body.container_id,
  );
  if (!known) {
    return "the selected container is no longer available";
  }
  return null;
}

async function mockApi(page: Page, state: ApiState): Promise<void> {
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
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
    await route.fulfill({
      json: {
        servers: [serverRow(SERVER_ID, "prod-01"), serverRow(SERVER_B_ID, "prod-02")],
      },
    });
  });
  await page.route("**/api/v1/terminal/targets", async (route) => {
    state.targetsCalls.push({ teamId: teamOf(route.request()) });
    if (state.targetsFailures > 0) {
      state.targetsFailures -= 1;
      await route.fulfill({ status: 503, json: { message: "node down" } });
      return;
    }
    await route.fulfill({ json: state.payload() });
  });
  await page.route("**/api/v1/servers/*/terminal/sessions**", async (route) => {
    const request = route.request();
    if (request.method() === "DELETE") {
      state.deletes += 1;
      await route.fulfill({ status: 204, body: "" });
      return;
    }
    if (request.method() !== "POST") {
      await route.fulfill({ json: {} });
      return;
    }
    const problem = validateBody(request, "host");
    if (problem !== null) {
      await route.fulfill({ status: 400, json: { message: problem } });
      return;
    }
    state.creates.push({
      kind: "host",
      body: request.postDataJSON() as Record<string, unknown>,
      url: request.url(),
      teamId: teamOf(request),
    });
    state.seq += 1;
    if (state.createDelayMs > 0) {
      await new Promise((done) => setTimeout(done, state.createDelayMs));
    }
    const id = `session-${state.seq}`;
    state.sessions.push({ id, socket: null });
    await route.fulfill({
      status: 201,
      json: {
        id,
        server_id: SERVER_ID,
        target: "host",
        cols: 120,
        rows: 32,
        expires_at: "2026-10-20T12:01:00Z",
        websocket_path: `/api/v1/servers/${SERVER_ID}/terminal/sessions/${id}/ws`,
        subprotocol: "gotham.terminal.v1",
      },
    });
  });
  await page.route("**/api/v1/applications/*/terminal/sessions**", async (route) => {
    const request = route.request();
    if (request.method() === "DELETE") {
      state.deletes += 1;
      await route.fulfill({ status: 204, body: "" });
      return;
    }
    if (request.method() !== "POST") {
      await route.fulfill({ json: {} });
      return;
    }
    const problem = validateBody(request, "container");
    if (problem !== null) {
      await route.fulfill({ status: 404, json: { message: problem } });
      return;
    }
    const body = request.postDataJSON() as {
      service: string;
      container_id: string;
    };
    state.creates.push({
      kind: "container",
      body: body as unknown as Record<string, unknown>,
      url: request.url(),
      teamId: teamOf(request),
    });
    state.seq += 1;
    if (state.createDelayMs > 0) {
      await new Promise((done) => setTimeout(done, state.createDelayMs));
    }
    const id = `session-${state.seq}`;
    state.sessions.push({ id, socket: null });
    await route.fulfill({
      status: 201,
      json: {
        id,
        server_id: SERVER_ID,
        target: "container",
        cols: 120,
        rows: 32,
        expires_at: "2026-10-20T12:01:00Z",
        websocket_path: `/api/v1/applications/${APPLICATION_ID}/terminal/sessions/${id}/ws`,
        subprotocol: "gotham.terminal.v1",
        application_id: APPLICATION_ID,
        service: body.service,
        container_id: body.container_id,
        container_name: "shop-web-1",
        target_label: "web · shop-web-1",
      },
    });
  });

  await page.routeWebSocket(
    /\/api\/v1\/(?:servers|applications)\/[^/]+\/terminal\/sessions\/[^/]+\/ws$/,
    (ws) => {
      const path = new URL(ws.url()).pathname;
      const id = path.split("/").at(-2) ?? "";
      const current = state.sessions.find((item) => item.id === id);
      if (!current) {
        ws.close();
        return;
      }
      current.socket = ws;
      ws.send(openFrame());
      ws.send(outputFrame(`welcome ${id}\r\n$ `));
    },
  );
}

async function seedApp(page: Page, path: string): Promise<void> {
  await page.addInitScript(
    (value) => {
      window.localStorage.setItem("gotham.auth.session", value);
      window.localStorage.setItem("gotham.teams.active", "team-1");
    },
    JSON.stringify(session),
  );
  await page.goto(`${baseURL}${path}`);
  await expect(page.locator(".topbar")).toBeVisible();
}

async function storeCall(
  page: Page,
  action: string,
  argument: unknown = {},
): Promise<unknown> {
  return page.evaluate(
    async ({ name, arg }) => {
      const app = (document.querySelector("#app") as unknown as {
        __vue_app__: {
          config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
        };
      }).__vue_app__;
      const store = app.config.globalProperties.$pinia._s.get("terminal") as {
        [key: string]: (..._args: unknown[]) => unknown;
      };
      return store[name]!(arg);
    },
    { name: action, arg: argument },
  );
}

async function openWindow(page: Page): Promise<void> {
  await storeCall(page, "openWindowSession", {
    serverId: SERVER_ID,
    serverName: "prod-01",
    cwd: "app",
  });
  await expect(page.locator(".term-window")).toBeVisible();
}

async function openPicker(page: Page, surface: "window" | "page"): Promise<void> {
  const scope = surface === "window" ? ".term-window" : ".term-page";
  await page.locator(`${scope} .term-add-tab`).click();
  await expect(page.locator(".term-targets")).toBeVisible();
}

test.describe("add-tab target picker", () => {
  test("the window list groups servers and containers, filters, and opens exactly one container tab", async ({
    page,
  }) => {
    const state = newState();
    await mockApi(page, state);
    await seedApp(page, "/dashboard");
    await openWindow(page);
    expect(state.creates).toHaveLength(1);

    await openPicker(page, "window");
    expect(state.targetsCalls).toEqual([{ teamId: TEAM_ID }]);
    await expect(page.locator(".term-targets-group-title")).toHaveText([
      "Servers",
      "shop",
    ]);
    await expect(page.locator(".term-target-option")).toHaveCount(4);

    await page.getByRole("dialog").getByRole("textbox").fill("shop-web-2");
    await expect(page.locator(".term-targets-group-title")).toHaveText(["shop"]);
    await expect(page.locator(".term-target-option")).toHaveCount(1);
    await expect(page.locator(".term-target-option")).toContainText(
      "web · shop-web-2",
    );

    await expect(page.locator(".term-targets-partial")).toContainText(
      "Some targets could not be loaded.",
    );

    await page.getByRole("dialog").getByRole("textbox").press("ArrowDown");
    await expect(page.locator(".term-target-option")).toBeFocused();
    await page.keyboard.press("Enter");

    await expect(page.locator(".term-targets")).toHaveCount(0);
    await expect(page.locator(".term-tab")).toHaveCount(2);

    const created = state.creates[1]!;
    expect(created.kind).toBe("container");
    expect(created.teamId).toBe(TEAM_ID);
    expect(created.url).toContain(`/api/v1/applications/${APPLICATION_ID}/terminal/sessions`);
    expect(created.body).toMatchObject({
      service: "web",
      container_id: CONTAINER_B_ID,
    });
    expect(created.body).not.toHaveProperty("target");
    expect(state.creates.filter((entry) => entry.kind === "host")).toHaveLength(1);
    expect(state.deletes).toBe(0);
    expect(state.sessions.filter((item) => item.socket !== null)).toHaveLength(2);
    await expect(page.locator(".term-tab").nth(1)).toContainText(
      "shop · web · shop-web-1",
    );
  });

  test("the page opens host and container sessions in separate tabs and keeps them isolated", async ({
    page,
  }) => {
    const state = newState();
    await mockApi(page, state);
    await seedApp(page, "/terminal");
    await expect(page.locator(".term-page")).toBeVisible();

    expect(state.creates).toHaveLength(0);
    await expect(page.getByRole("heading", { name: "Terminal", level: 1 })).toBeVisible();

    await openPicker(page, "page");
    await page.locator(".term-target-option").filter({ hasText: "prod-01" }).click();
    await expect(page.locator(".term-page-tab")).toHaveCount(1);
    await expect(page.locator(".term-page-tab")).toContainText("prod-01");
    expect(state.creates).toHaveLength(1);
    expect(state.creates[0]!.kind).toBe("host");
    expect(state.creates[0]!.body).toMatchObject({ target: "host" });
    expect(state.creates[0]!.body).not.toHaveProperty("root");
    expect(state.creates[0]!.body).not.toHaveProperty("cwd");

    await openPicker(page, "page");
    expect(state.targetsCalls).toHaveLength(2);
    await page
      .locator(".term-target-option")
      .filter({ hasText: "shop-web-1" })
      .click();
    await expect(page.locator(".term-page-tab")).toHaveCount(2);
    expect(state.creates).toHaveLength(2);
    expect(state.creates[1]!.kind).toBe("container");
    expect(state.creates[1]!.body).toMatchObject({
      service: "web",
      container_id: CONTAINER_ID,
    });

    const tabs = page.locator(".term-page-tab");
    await expect(tabs.nth(1)).toContainText("shop · web · shop-web-1");
    await expect(page.locator(".term-page .xterm-rows")).toContainText("welcome session-2");
    await tabs.nth(0).click();
    await expect(page.locator(".term-page .xterm-rows")).toContainText("welcome session-1");
    await expect(
      page.locator(".term-page-tab-name").nth(0),
    ).toHaveAttribute("aria-selected", "true");
    await expect(
      page.locator(".term-page-tab-name").nth(1),
    ).toHaveAttribute("aria-selected", "false");
    expect(state.creates).toHaveLength(2);
  });

  test("single-flight per target: a double click opens one session in one new tab", async ({
    page,
  }) => {
    const state = newState();
    state.createDelayMs = 400;
    await mockApi(page, state);
    await seedApp(page, "/dashboard");
    await openWindow(page);
    await openPicker(page, "window");

    const option = page.locator(".term-target-option").first();
    await option.dblclick({ delay: 20 });
    await expect(page.locator(".term-targets")).toHaveCount(0);
    await expect(page.locator(".term-tab")).toHaveCount(2);
    expect(state.creates).toHaveLength(2);
  });

  test("empty, error and retry states stay local and never show backend text", async ({
    page,
  }) => {
    const state = newState();
    state.payload = () => targetPayload({ servers: [], applications: [], errors: [] });
    await mockApi(page, state);
    await seedApp(page, "/dashboard");
    await openWindow(page);

    await openPicker(page, "window");
    await expect(page.locator(".term-targets-empty")).toHaveText(
      "No terminal targets available",
    );

    await page.keyboard.press("Escape");
    await expect(page.locator(".term-targets")).toHaveCount(0);
    state.payload = () => targetPayload();
    state.targetsFailures = 1;
    await openPicker(page, "window");
    await expect(page.locator(".term-targets-error")).toContainText(
      "The target list is unavailable right now.",
    );
    await expect(page.locator(".term-targets-error")).not.toContainText("node down");

    await page.locator(".term-targets").getByRole("button", { name: "Retry" }).click();
    await expect(page.locator(".term-targets-group-title")).toHaveText([
      "Servers",
      "shop",
    ]);
    expect(state.targetsCalls).toHaveLength(3);
    expect(state.creates).toHaveLength(1);
  });

  test("per-source failures with no list still offer a retry", async ({ page }) => {
    const state = newState();
    state.payload = () =>
      targetPayload({
        servers: [],
        applications: [],
        errors: [{ source: "applications", code: "unavailable" }],
      });
    await mockApi(page, state);
    await seedApp(page, "/dashboard");
    await openWindow(page);
    await openPicker(page, "window");

    await expect(page.locator(".term-targets-error")).toContainText(
      "Some targets could not be loaded.",
    );
    await expect(page.locator(".term-targets-empty")).toContainText(
      "The target list is unavailable right now.",
    );
    await expect(page.locator(".term-targets-empty")).not.toContainText(
      "No terminal targets available",
    );
    await page
      .locator(".term-targets")
      .getByRole("button", { name: "Retry" })
      .click();
    await expect(page.locator(".term-targets-error")).toHaveCount(1);
    expect(state.targetsCalls).toHaveLength(2);
  });

  test("page single-flight: a double click opens one page tab", async ({ page }) => {
    const state = newState();
    state.createDelayMs = 400;
    await mockApi(page, state);
    await seedApp(page, "/terminal");
    await openPicker(page, "page");

    const option = page
      .locator(".term-target-option")
      .filter({ hasText: "prod-01" });
    await option.dblclick({ delay: 20 });
    await expect(page.locator(".term-targets")).toHaveCount(0);
    await expect(page.locator(".term-page-tab")).toHaveCount(1);
    expect(state.creates).toHaveLength(1);
    expect(state.creates[0]?.kind).toBe("host");
  });

  test("keyboard: Enter opens, focus lands in search, arrows/Enter pick, Escape returns focus", async ({
    page,
  }) => {
    const state = newState();
    await mockApi(page, state);
    await seedApp(page, "/dashboard");
    await openWindow(page);

    const trigger = page.locator(".term-window .term-add-tab");
    await trigger.focus();
    await page.keyboard.press("Space");
    await expect(page.locator(".term-targets")).toBeVisible();
    await expect(page.locator(".term-targets input").first()).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(page.locator(".term-targets")).toHaveCount(0);
    await expect(trigger).toBeFocused();

    await page.keyboard.press("Enter");
    await expect(page.locator(".term-targets")).toBeVisible();
    await expect(page.locator(".term-targets input").first()).toBeFocused();

    await page.keyboard.type("shop-web-2");
    await expect(page.locator(".term-target-option")).toHaveCount(1);
    await page.keyboard.press("ArrowDown");
    await expect(page.locator(".term-target-option")).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(page.locator(".term-tab")).toHaveCount(2);
    expect(state.creates[1]?.kind).toBe("container");
    expect(state.creates[1]?.body).toMatchObject({ container_id: CONTAINER_B_ID });

  });

  test("page tab strip scrolls the active tab into view at every width", async ({
    page,
  }) => {
    const state = newState();
    await mockApi(page, state);
    await seedApp(page, "/terminal");
    for (let index = 0; index < 10; index += 1) {
      await storeCall(page, "openPageSession", {
        serverId: SERVER_ID,
        serverName: "prod-01",
        cwd: `tab-${index}`,
        fresh: true,
      });
    }
    await expect(page.locator(".term-page-tab")).toHaveCount(10);
    expect(state.creates).toHaveLength(10);

    for (const width of [1366, 1280, 480, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await expect
        .poll(() =>
          page.evaluate(() => {
            const strip = document.querySelector(
              ".term-page-tabs",
            ) as HTMLElement;
            const active = document.querySelector(
              ".term-page-tab.is-active",
            ) as HTMLElement;
            const stripRect = strip.getBoundingClientRect();
            const tabRect = active.getBoundingClientRect();
            return (
              tabRect.left >= stripRect.left - 1 &&
              tabRect.right <= stripRect.right + 1
            );
          }),
        )
        .toBe(true);
      const geometry = await page.evaluate(() => {
        const strip = document.querySelector(".term-page-tabs") as HTMLElement;
        const active = document.querySelector(
          ".term-page-tab.is-active",
        ) as HTMLElement;
        const add = document.querySelector(
          ".term-page .term-add-tab",
        ) as HTMLElement;
        const box = (element: HTMLElement) => {
          const rect = element.getBoundingClientRect();
          return { left: rect.left, right: rect.right };
        };
        return {
          strip: box(strip),
          active: box(active),
          add: box(add),
          scrollWidth: strip.scrollWidth,
          clientWidth: strip.clientWidth,
        };
      });
      expect(geometry.active.left).toBeGreaterThanOrEqual(geometry.strip.left - 1);
      expect(geometry.active.right).toBeLessThanOrEqual(geometry.strip.right + 1);
      expect(geometry.strip.right).toBeLessThanOrEqual(geometry.add.left + 1);
      if (width <= 480) {
        expect(geometry.scrollWidth).toBeGreaterThan(geometry.clientWidth);
      }
    }

    await page.locator(".term-page-tab-name").nth(0).click();
    await expect
      .poll(() =>
        page.evaluate(() => {
          const strip = document.querySelector(".term-page-tabs") as HTMLElement;
          const active = document.querySelector(
            ".term-page-tab.is-active",
          ) as HTMLElement;
          const stripRect = strip.getBoundingClientRect();
          const tabRect = active.getBoundingClientRect();
          return (
            tabRect.left >= stripRect.left - 1 &&
            tabRect.right <= stripRect.right + 1
          );
        }),
      )
      .toBe(true);
  });

  test("a non-operator payload carries containers but no servers", async ({ page }) => {
    const state = newState();
    state.payload = () =>
      targetPayload({
        servers: [],
        applications: [
          {
            application_id: APPLICATION_ID,
            application_name: "shop",
            server_id: SERVER_ID,
            source_type: "image",
            containers: [targetContainer("web", CONTAINER_ID, "shop-web-1")],
          },
        ],
        errors: [],
        truncated: true,
      });
    await mockApi(page, state);
    await seedApp(page, "/dashboard");
    await openWindow(page);
    await openPicker(page, "window");

    await expect(page.locator(".term-targets-group-title")).toHaveText(["shop"]);
    await expect(page.locator(".term-target-option")).toHaveCount(1);
    await expect(page.locator(".term-target-option")).toContainText("shop-web-1");
    await expect(page.locator(".term-targets-partial")).toHaveCount(0);
    await expect(page.locator(".term-targets-truncated")).toContainText(
      "Showing the first 1 of the available targets.",
    );
  });
});
