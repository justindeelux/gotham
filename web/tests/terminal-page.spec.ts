import { Buffer } from "node:buffer";
import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page, WebSocketRoute } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, URL:readonly, window:readonly */

// JUS-246 real-pane proof: the committed webdist is served statically, the
// control-plane API is mocked, and Playwright's WebSocket router stands in for
// the PTY with the exact binary framing. The standalone page, the server
// detail tab and the paused-session dock are driven with real input.

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

const ADMIN = {
  id: "u-1",
  email: "ada@gotham.dev",
  created_at: "2026-03-04T12:00:00Z",
  display_name: "Ada",
  has_password: true,
  is_platform_admin: true,
};

const MEMBER = {
  id: "u-2",
  email: "sam@gotham.dev",
  created_at: "2026-03-04T12:00:00Z",
  display_name: "Sam",
  has_password: true,
  is_platform_admin: false,
};

const team = {
  id: "team-1",
  name: "Acme",
  is_personal: false,
  role: "owner",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

const SERVER_A = "11111111-1111-4111-8111-111111111111";
const SERVER_B = "22222222-2222-4222-8222-222222222222";
const SERVER_OFFLINE = "33333333-3333-4333-8333-333333333333";

/** serverRow builds one managed server row. */
function serverRow(id: string, name: string, status: string) {
  return {
    id,
    name,
    ip: "10.0.0.1",
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status,
    node_id: id,
    os: "linux",
    docker_version: "27",
    arch: "amd64",
    total_mem: 1,
    total_disk: 1,
    cpu_usage: null,
    mem_usage: null,
    disk_usage: null,
    container_count: 0,
    last_seen: null,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

const servers = [
  serverRow(SERVER_A, "prod-01", "ready"),
  serverRow(SERVER_B, "prod-02", "ready"),
  serverRow(SERVER_OFFLINE, "prod-03", "offline"),
];

/** fakeSession is one session the mocked HTTP + WS pair understands. */
interface FakeSession {
  id: string;
  serverId: string;
  input: string;
  socket: WebSocketRoute | null;
  closeFrames: number;
}

interface TerminalState {
  sessions: FakeSession[];
  creates: number;
  deletes: number;
  createServers: string[];
  seq: number;
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

/** frame builds one binary server frame. */
function frame(opcode: number, payload: Buffer = Buffer.alloc(0)): Buffer {
  return Buffer.concat([Buffer.from([opcode]), payload]);
}

/** openFrame confirms a PTY for one session. */
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

/** outputFrame writes one line of shell output. */
function outputFrame(text: string): Buffer {
  return frame(1, Buffer.from(text, "utf8"));
}

/** freshState resets the mock ledger. */
function freshState(): TerminalState {
  return { sessions: [], creates: 0, deletes: 0, createServers: [], seq: 0 };
}

/** mockApi answers the shell, server and terminal routes. */
async function mockApi(
  page: Page,
  state: TerminalState,
  user: typeof ADMIN | typeof MEMBER = ADMIN,
): Promise<void> {
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: user });
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
  for (const row of servers) {
    await page.route(`**/api/v1/servers/${row.id}`, async (route) => {
      await route.fulfill({ json: { server: row } });
    });
  }

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
    const segments = new URL(request.url()).pathname.split("/");
    const serverId = segments[segments.indexOf("servers") + 1] ?? "";
    state.seq += 1;
    state.creates += 1;
    state.createServers.push(serverId);
    const id = `session-${state.seq}`;
    state.sessions.push({
      id,
      serverId,
      input: "",
      socket: null,
      closeFrames: 0,
    });
    await route.fulfill({
      status: 201,
      json: {
        id,
        server_id: serverId,
        target: "host",
        root: "volumes",
        cwd: "a/deep/working/directory",
        cols: 120,
        rows: 32,
        expires_at: "2026-10-20T12:01:00Z",
        websocket_path: `/api/v1/servers/${serverId}/terminal/sessions/${id}/ws`,
        subprotocol: "gotham.terminal.v1",
      },
    });
  });

  await page.routeWebSocket(
    /\/api\/v1\/servers\/[^/]+\/terminal\/sessions\/[^/]+\/ws$/,
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
      ws.onMessage((message) => {
        const bytes =
          typeof message === "string"
            ? Buffer.from(message, "utf8")
            : Buffer.from(message);
        if (bytes.length === 0) {
          return;
        }
        if (bytes[0] === 1) {
          current.input += bytes.subarray(1).toString("utf8");
        } else if (bytes[0] === 3) {
          current.closeFrames += 1;
        }
      });
    },
  );
}

/** seedApp seeds the signed-in user and opens one route. */
async function seedApp(
  page: Page,
  path: string,
  user: typeof ADMIN | typeof MEMBER = ADMIN,
): Promise<void> {
  await page.addInitScript(
    (value) => {
      window.localStorage.setItem("gotham.auth.session", value);
    },
    JSON.stringify({
      user,
      accessToken: "test-access",
      refreshToken: "test-refresh",
    }),
  );
  await page.goto(`${baseURL}${path}`);
  await expect(page.locator(".topbar")).toBeVisible();
}

/** tabInfo reads the first terminal tab from the live Pinia store. */
async function tabInfo(page: Page): Promise<{
  id: string;
  sessionId: string;
  mode: string;
  sourcePath: string;
} | null> {
  return page.evaluate(() => {
    const app = (document.querySelector("#app") as unknown as {
      __vue_app__: {
        config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
      };
    }).__vue_app__;
    const store = app.config.globalProperties.$pinia._s.get("terminal") as {
      tabs: Array<{
        id: string;
        sessionId: string;
        mode: string;
        sourcePath: string;
      }>;
    };
    const tab = store.tabs[0];
    return tab
      ? {
          id: tab.id,
          sessionId: tab.sessionId,
          mode: tab.mode,
          sourcePath: tab.sourcePath,
        }
      : null;
  });
}

/** navLabels returns the sidebar entries in order. */
async function navLabels(page: Page): Promise<string[]> {
  return page.locator(".sidebar .nav-item").allInnerTexts();
}

test("the standalone page creates one shell for the chosen server and survives route changes", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state);
  await seedApp(page, "/terminal");

  // Choosing a server is the activation: nothing is created before it.
  expect(state.creates).toBe(0);
  await expect(page.locator(".term-surface")).toHaveCount(0);
  await page.locator(".server-pick", { hasText: "prod-01" }).click();
  await expect.poll(() => state.creates).toBe(1);
  expect(state.createServers).toEqual([SERVER_A]);
  await expect(page).toHaveURL(new RegExp(`/terminal\\?server=${SERVER_A}`));
  await expect(
    page.locator(".term-surface .term-state"),
  ).toHaveText("Connected");

  // Real input reaches the session for the correct server.
  await page.locator(".term-surface .xterm-screen").click();
  await page.keyboard.type("echo one");
  await expect
    .poll(() => state.sessions[0]?.input)
    .toBe("echo one");

  // 390px: the meta row wraps instead of clipping and the pane fits.
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".term-meta")).toBeVisible();
  const metaFits = await page.locator(".term-meta").evaluate((element) => {
    const node = element as HTMLElement;
    return node.scrollWidth <= node.clientWidth + 1;
  });
  expect(metaFits).toBe(true);
  const noHorizontalScroll = await page.evaluate(
    () => document.documentElement.scrollWidth <= window.innerWidth + 1,
  );
  expect(noHorizontalScroll).toBe(true);
  await page.setViewportSize({ width: 1280, height: 800 });

  // Leaving the route keeps the PTY alive and surfaces a paused card.
  const before = await tabInfo(page);
  await page.locator('a[href="/dashboard"]').first().click();
  await expect(page).toHaveURL(/\/dashboard$/);
  const dock = page.locator(".term-dock");
  await expect(dock).toBeVisible();
  await expect(dock).toContainText("Paused");
  await expect(dock).toContainText("prod-01");
  await expect(dock.locator(".term-host")).toHaveCount(0);
  expect(state.creates).toBe(1);
  expect(state.deletes).toBe(0);

  // Resume returns to the owning route and reuses the same session/buffer.
  await dock.locator(".term-dock-resume").click();
  await expect(page).toHaveURL(new RegExp(`/terminal\\?server=${SERVER_A}`));
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  expect(await tabInfo(page)).toEqual(before);
  expect(state.creates).toBe(1);
  await expect
    .poll(() => page.locator(".term-surface .xterm-rows").innerText())
    .toContain(`welcome ${before?.sessionId}`);

  // The page pane close affordance runs the shared Naive confirmation.
  const paneClose = page.locator('.term-surface [aria-label="Close terminal"]');
  await paneClose.click();
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Keep terminals" }).click();
  await expect(dialog).toBeHidden();
  expect(state.deletes).toBe(0);
  await expect(page.locator(".term-surface")).toBeVisible();

  await paneClose.click();
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();
  await expect(page.locator(".term-surface")).toHaveCount(0);
  await expect.poll(() => state.deletes).toBe(1);
  await expect(page.locator(".term-dock")).toHaveCount(0);
  await expect(page.locator(".rail-btn--terminal")).toHaveCount(0);
});

test("page → window transfer keeps the session id and buffer, one way only", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state);
  await seedApp(page, `/terminal?server=${SERVER_A}`);
  await expect.poll(() => state.creates).toBe(1);
  const before = await tabInfo(page);
  expect(before?.mode).toBe("page");

  await page.locator(".term-surface .xterm-screen").click();
  await page.keyboard.type("echo two");
  await expect.poll(() => state.sessions[0]?.input).toBe("echo two");

  await page.locator(".term-open-in-window").click();
  const window = page.locator(".term-window");
  await expect(window).toBeVisible();
  expect(state.creates).toBe(1);
  const after = await tabInfo(page);
  expect(after?.id).toBe(before?.id);
  expect(after?.sessionId).toBe(before?.sessionId);
  expect(after?.mode).toBe("window");
  await expect
    .poll(() => window.locator(".xterm-rows").innerText())
    .toContain(`welcome ${before?.sessionId}`);

  // A page pane never offers a return trip, and the source shows the notice.
  await expect(page.locator(".term-window").getByRole("button", { name: /page/i }))
    .toHaveCount(0);
  await expect(page.locator(".term-open-in-page")).toHaveCount(0);
  await expect(page.locator(".term-detached")).toBeVisible();

  // Returning to the source route reuses the same (still window) session.
  await page.locator('a[href="/dashboard"]').first().click();
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.locator(".rail-btn--terminal .rail-badge")).toHaveText("1");
  await expect(page.locator(".term-dock")).toHaveCount(0);
  await page.goBack();
  await expect(page).toHaveURL(new RegExp(`/terminal\\?server=${SERVER_A}`));
  await expect(page.locator(".term-detached")).toBeVisible();
  expect(state.creates).toBe(1);
  expect(state.deletes).toBe(0);
  expect((await tabInfo(page))?.sessionId).toBe(before?.sessionId);
});

test("the server detail Terminal tab is lazy, single-flight and server-correct", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state);
  await seedApp(page, `/servers/${SERVER_A}`);

  // Opening the detail page never starts a shell.
  expect(state.creates).toBe(0);
  await expect(
    page.locator(".n-tabs-tab", { hasText: "Overview" }),
  ).toBeVisible();
  const labels = await page.locator(".n-tabs-tab").allInnerTexts();
  const files = labels.indexOf("Files");
  expect(files).toBeGreaterThanOrEqual(0);
  expect(labels.indexOf("Terminal")).toBe(files + 1);

  await page.locator(".n-tabs-tab", { hasText: "Terminal" }).click();
  await expect.poll(() => state.creates).toBe(1);
  expect(state.createServers).toEqual([SERVER_A]);
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  const before = await tabInfo(page);
  expect(before?.sourcePath).toContain("tab=terminal");

  // Rapid activation and re-entry reuse the live session, never a duplicate.
  for (let index = 0; index < 3; index += 1) {
    await page.locator(".n-tabs-tab", { hasText: "Overview" }).click();
    await page.locator(".n-tabs-tab", { hasText: "Terminal" }).click();
  }
  expect(state.creates).toBe(1);
  expect((await tabInfo(page))?.sessionId).toBe(before?.sessionId);

  // Leaving the detail page pauses the shell; the dock resumes the exact tab.
  await page.locator('a[href="/dashboard"]').first().click();
  await expect(page.locator(".term-dock")).toBeVisible();
  await page.locator(".term-dock-resume").click();
  await expect(page).toHaveURL(
    new RegExp(`/servers/${SERVER_A}\\?tab=terminal`),
  );
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  expect((await tabInfo(page))?.sessionId).toBe(before?.sessionId);
  expect(state.creates).toBe(1);

  // An offline node shows the unavailable state and never dials a shell.
  await page.goto(`${baseURL}/servers/${SERVER_OFFLINE}`);
  await expect(page.locator(".topbar")).toBeVisible();
  await page.locator(".n-tabs-tab", { hasText: "Terminal" }).click();
  await expect(page.getByText("Terminal unavailable")).toBeVisible();
  await expect(page.locator(".server-terminal")).toContainText("Offline");
  expect(state.creates).toBe(1);
  await expect(page.locator(".term-surface")).toHaveCount(0);
});

test("menu order and operator gating match the backend", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state);
  await seedApp(page, "/dashboard");

  const labels = await navLabels(page);
  expect(labels.indexOf("Terminal")).toBe(labels.indexOf("File manager") + 1);
  expect(labels.indexOf("Template library")).toBe(
    labels.indexOf("Terminal") + 1,
  );

  await page.goto(`${baseURL}/terminal`);
  await expect(page).toHaveURL(/\/terminal/);
  await expect(page.locator(".server-pick")).toHaveCount(3);
});

test("a non-operator sees no Terminal entry point and no shell", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state, MEMBER);
  await seedApp(page, "/dashboard", MEMBER);

  const labels = await navLabels(page);
  expect(labels).not.toContain("Terminal");
  expect(labels).not.toContain("File manager");

  await page.goto(`${baseURL}/terminal`);
  await expect(page).toHaveURL(/\/forbidden$/);
  expect(state.creates).toBe(0);

  await page.goto(`${baseURL}/servers/${SERVER_A}`);
  await expect(page.locator(".topbar")).toBeVisible();
  const tabs = await page.locator(".n-tabs-tab").allInnerTexts();
  expect(tabs).not.toContain("Terminal");
  expect(tabs).not.toContain("Files");
  expect(state.creates).toBe(0);
});
