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

/** CreateBody is the exact terminal create-session request. */
interface CreateBody {
  target?: string;
  root?: string;
  cwd?: string;
  cols?: number;
  rows?: number;
  [key: string]: unknown;
}

/** FakeSession is one session the mocked HTTP + WS pair understands. */
interface FakeSession {
  id: string;
  serverId: string;
  input: string;
  socket: WebSocketRoute | null;
  closeFrames: number;
  /** closeOnOpen makes the PTY socket drop as soon as it attaches. */
  closeOnOpen: boolean;
}

/** CreateBehavior scripts one POST /terminal/sessions response. */
type CreateBehavior =
  | { kind: "ok" }
  | { kind: "status"; status: number }
  | { kind: "body"; status: number; message: string }
  | { kind: "malformed" }
  | { kind: "network" }
  | { kind: "delay" }
  | { kind: "close" };

interface TerminalState {
  sessions: FakeSession[];
  creates: number;
  deletes: number;
  createServers: string[];
  /** createBodies records the exact payload of every create call. */
  createBodies: CreateBody[];
  seq: number;
  /** behaviors is consumed per create; empty means "ok". */
  behaviors: CreateBehavior[];
  /** releases settle a delayed create during the flapping test. */
  releases: Array<() => void>;
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
  return {
    sessions: [],
    creates: 0,
    deletes: 0,
    createServers: [],
    createBodies: [],
    seq: 0,
    behaviors: [],
    releases: [],
  };
}

/** ROOT_PATTERN mirrors internal/terminal's allow-listed root name shape. */
const ROOT_PATTERN = /^[a-z0-9][a-z0-9_-]{0,127}$/;

/**
 * createBodyError mirrors the control plane's create-session validation. The
 * Terminal page and the Server detail Terminal tab must send a payload this
 * accepts: root is optional (the agent then picks the default directory), but
 * a root that is present must be a bounded name, and a cwd requires a root.
 * Keeping the real rules in the fake makes a UI/backend payload mismatch fail
 * these tests instead of being silently accepted by a permissive mock.
 */
function createBodyError(body: CreateBody): string | null {
  const target = body.target ?? "host";
  if (target !== "host") {
    return `unsupported target ${target}`;
  }
  if (typeof body.root !== "undefined") {
    if (
      typeof body.root !== "string" ||
      (body.root !== "" && !ROOT_PATTERN.test(body.root))
    ) {
      return "root must match ^[a-z0-9][a-z0-9_-]{0,127}$";
    }
  }
  if (typeof body.cwd !== "undefined" && typeof body.cwd !== "string") {
    return "invalid working directory";
  }
  const root = body.root ?? "";
  const cwd = body.cwd ?? "";
  if (cwd.length > 4096 || cwd.includes("\0")) {
    return "invalid working directory";
  }
  if (cwd.startsWith("/") || cwd.startsWith("\\")) {
    return "working directory must be relative to the root";
  }
  if (cwd.split(/[/\\]/).includes("..")) {
    return "working directory must not contain a parent segment";
  }
  if (cwd !== "" && root === "") {
    return "a working directory requires a file root";
  }
  if (
    !Number.isInteger(body.cols) ||
    !Number.isInteger(body.rows) ||
    (body.cols as number) < 1 ||
    (body.cols as number) > 1000 ||
    (body.rows as number) < 1 ||
    (body.rows as number) > 1000
  ) {
    return "dimensions must be between 1 and 1000";
  }
  return null;
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
    const behavior: CreateBehavior = state.behaviors.shift() ?? { kind: "ok" };
    state.creates += 1;
    state.createServers.push(serverId);

    // Validate exactly like the control plane. A create body the backend would
    // refuse must fail here, never be accepted because the mock is permissive.
    const body = (request.postDataJSON() ?? {}) as CreateBody;
    state.createBodies.push(body);
    const bodyError = createBodyError(body);
    if (bodyError !== null) {
      await route.fulfill({
        status: 400,
        json: { message: `terminal: invalid request: ${bodyError}` },
      });
      return;
    }

    // A fault scripted by a test: count the request first so a "no retry"
    // assertion sees the exact number of attempts.
    if (behavior.kind === "network") {
      await route.abort("failed");
      return;
    }
    if (behavior.kind === "status") {
      await route.fulfill({
        status: behavior.status,
        json: { error: `scripted ${behavior.status}` },
      });
      return;
    }
    if (behavior.kind === "body") {
      // A backend body message reaches the ApiError verbatim; the UI must map
      // the failure to a bounded catalog code and never echo this text.
      await route.fulfill({
        status: behavior.status,
        json: { message: behavior.message },
      });
      return;
    }
    if (behavior.kind === "malformed") {
      // A 2xx body that violates the create contract: the UI must report an
      // unexpected response, never a network error, and never open a socket.
      await route.fulfill({ status: 201, json: { id: "only-an-id" } });
      return;
    }
    if (behavior.kind === "delay") {
      await new Promise<void>((resolve) => {
        state.releases.push(resolve);
      });
    }

    state.seq += 1;
    const id = `session-${state.seq}`;
    state.sessions.push({
      id,
      serverId,
      input: "",
      socket: null,
      closeFrames: 0,
      closeOnOpen: behavior.kind === "close",
    });
    const root = body.root ?? "";
    const cwd = body.cwd ?? "";
    await route.fulfill({
      status: 201,
      json: {
        id,
        server_id: serverId,
        target: "host",
        ...(root !== "" ? { root } : {}),
        ...(cwd !== "" ? { cwd } : {}),
        cols: body.cols ?? 120,
        rows: body.rows ?? 32,
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
      if (current.closeOnOpen) {
        // The PTY stream dropped right after attach (transport failure).
        ws.close({ code: 1006 });
        return;
      }
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
    const tab =
      store.tabs.find((item) => item.mode === "page") ?? store.tabs[0];
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

/** tabCounts returns how many page/window tabs the live store holds. */
async function tabCounts(
  page: Page,
): Promise<{ page: number; window: number }> {
  return page.evaluate(() => {
    const app = (document.querySelector("#app") as unknown as {
      __vue_app__: {
        config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
      };
    }).__vue_app__;
    const store = app.config.globalProperties.$pinia._s.get("terminal") as {
      tabs: Array<{ mode: string }>;
    };
    return {
      page: store.tabs.filter((tab) => tab.mode === "page").length,
      window: store.tabs.filter((tab) => tab.mode === "window").length,
    };
  });
}

/** openServerSelect opens the Naive select and picks one server. */
async function pickServer(page: Page, name: string): Promise<void> {
  await page.locator(".term-server-select").click();
  await page
    .locator(".n-base-select-option")
    .filter({ hasText: name })
    .click();
}
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
  // The page has no File manager context: the create payload must not invent a
  // root or cwd (the control plane now accepts an omitted root).
  expect(state.createBodies[0]).toEqual({ target: "host", cols: 120, rows: 32 });
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

  // On a phone the dock is a compact bar until the operator expands it, so
  // it cannot blanket the page content.
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".term-dock-head")).toBeVisible();
  await expect(page.locator(".term-dock-list")).toBeHidden();
  await page.locator(".term-dock-head").click();
  await expect(page.locator(".term-dock-list")).toBeVisible();
  await page.setViewportSize({ width: 1280, height: 800 });

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
  await expect(page.getByText("Session closed")).toBeVisible();
  // Closing never starts a replacement on its own.
  expect(state.creates).toBe(1);
  await expect(page.locator(".term-dock")).toHaveCount(0);
  await expect(page.locator(".rail-btn--terminal")).toHaveCount(0);

  // The explicit restart is the only path back, and it creates exactly once.
  await page
    .locator(".term-stage-closed")
    .getByRole("button", { name: "Start new session" })
    .click();
  await expect.poll(() => state.creates).toBe(2);
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  expect((await tabInfo(page))?.sessionId).toBe("session-2");
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
  // The detail tab has no File manager context either; the same exact payload
  // the Terminal page sends must pass the control plane's validation.
  expect(state.createBodies[0]).toEqual({ target: "host", cols: 120, rows: 32 });
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

test("the moved-to-window notice starts a new page session only on demand", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state);
  await seedApp(page, `/terminal?server=${SERVER_A}`);
  await expect.poll(() => state.creates).toBe(1);

  await page.locator(".term-open-in-window").click();
  await expect(page.locator(".term-detached")).toBeVisible();
  await expect(page.getByText("This terminal is open in the window")).toBeVisible();
  // Nothing starts from the notice itself.
  expect(state.creates).toBe(1);

  // The notice sits above the floating window (JUS-246 r2 finding 4), so the
  // real pointer path works; keyboard was the only path before.
  const startNew = page
    .locator(".term-detached")
    .getByRole("button", { name: "Start new session" });
  await startNew.click();
  await expect.poll(() => state.creates).toBe(2);
  // Scope to the page pane: the floating window keeps its own (already
  // Connected) surface, so a bare .term-surface locator is ambiguous here.
  await expect(page.locator(".term-page .term-surface .term-state")).toHaveText(
    "Connected",
  );
  expect(await tabCounts(page)).toEqual({ page: 1, window: 1 });
});

test("the server detail tab recovers from a closed session with an explicit restart", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state);
  await seedApp(page, `/servers/${SERVER_A}?tab=terminal`);
  await expect.poll(() => state.creates).toBe(1);

  await page.locator('.term-surface [aria-label="Close terminal"]').click();
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();

  await expect(page.locator(".term-surface")).toHaveCount(0);
  await expect.poll(() => state.deletes).toBe(1);
  await expect(page.locator(".term-stage-closed")).toBeVisible();
  await expect(page.getByText("Session closed")).toBeVisible();
  expect(state.creates).toBe(1);

  // Focus lands on the explicit restart, not on a rail link.
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.textContent ?? ""))
    .toContain("Start new session");

  // Switching Overview -> Terminal keeps the closed card and creates nothing.
  await page.locator(".n-tabs-tab", { hasText: "Overview" }).click();
  await page.locator(".n-tabs-tab", { hasText: "Terminal" }).click();
  await expect(page.locator(".term-stage-closed")).toBeVisible();
  expect(state.creates).toBe(1);

  await page
    .locator(".term-stage-closed")
    .getByRole("button", { name: "Start new session" })
    .click();
  await expect.poll(() => state.creates).toBe(2);
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  expect((await tabInfo(page))?.sessionId).toBe("session-2");
});

test("a deleted server shows the unknown state and never dials a shell", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state);
  await seedApp(page, "/terminal?server=deleted-server-id");

  await expect(
    page.getByText("This server is not managed by this control plane."),
  ).toBeVisible();
  expect(state.creates).toBe(0);
  expect(state.deletes).toBe(0);
});

const createFailures = [
  {
    name: "403 forbidden",
    behavior: { kind: "status", status: 403 } as const,
    message: "Only a platform operator can open a host terminal.",
  },
  {
    name: "429 quota",
    behavior: { kind: "status", status: 429 } as const,
    message: "The terminal session quota is reached. Close a session and try again.",
  },
  {
    name: "501 unsupported agent",
    behavior: { kind: "status", status: 501 } as const,
    message: "This node agent does not support terminal sessions.",
  },
  {
    name: "network failure",
    behavior: { kind: "network" } as const,
    message: "The control plane could not be reached.",
  },
];

for (const failure of createFailures) {
  test(`create failure (${failure.name}) is explicit and never auto-retries`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    const state = freshState();
    state.behaviors = [failure.behavior];
    await mockApi(page, state);
    await seedApp(page, `/terminal?server=${SERVER_A}`);

    await expect.poll(() => state.creates).toBe(1);
    expect(state.createServers).toEqual([SERVER_A]);
    await expect(page.getByText(failure.message)).toBeVisible();
    await expect(
      page.getByRole("button", { name: "Start new session" }),
    ).toBeVisible();

    // No reconnect loop: the count is unchanged after a full retry window.
    await page.waitForTimeout(2000);
    expect(state.creates).toBe(1);

    // Starting a new session is explicit and creates exactly once.
    await page.getByRole("button", { name: "Start new session" }).click();
    await expect.poll(() => state.creates).toBe(2);
    await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  });
}

test("a dropped stream shows Disconnected and retries only on demand", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  state.behaviors = [{ kind: "close" }];
  await mockApi(page, state);
  await seedApp(page, `/terminal?server=${SERVER_A}`);

  await expect.poll(() => state.creates).toBe(1);
  await expect(page.locator(".term-surface .term-state")).toHaveText(
    "Disconnected",
  );
  await page.waitForTimeout(2000);
  expect(state.creates).toBe(1);

  await page.getByRole("button", { name: "Start new session" }).click();
  await expect.poll(() => state.creates).toBe(2);
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
});

test("server detail create failure is explicit and never auto-retries", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  state.behaviors = [{ kind: "status", status: 501 }];
  await mockApi(page, state);
  await seedApp(page, `/servers/${SERVER_A}?tab=terminal`);

  await expect.poll(() => state.creates).toBe(1);
  await expect(
    page.getByText("This node agent does not support terminal sessions."),
  ).toBeVisible();
  await page.waitForTimeout(2000);
  expect(state.creates).toBe(1);

  await page.getByRole("button", { name: "Start new session" }).click();
  await expect.poll(() => state.creates).toBe(2);
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
});

test("an unexpected create response is bounded and never reported as network", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  state.behaviors = [{ kind: "malformed" }];
  await mockApi(page, state);
  await seedApp(page, `/terminal?server=${SERVER_A}`);

  await expect.poll(() => state.creates).toBe(1);
  await expect(
    page.getByText(
      "The control plane sent an unexpected response. Start a new session to try again.",
    ),
  ).toBeVisible();
  await expect(
    page.getByText("The control plane could not be reached."),
  ).toHaveCount(0);
  // No session was registered, so no WebSocket could be opened.
  expect(state.sessions).toHaveLength(0);
  await page.waitForTimeout(1500);
  expect(state.creates).toBe(1);

  // Explicit retry only; the next create is a well-formed response.
  await page.getByRole("button", { name: "Start new session" }).click();
  await expect.poll(() => state.creates).toBe(2);
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  expect(state.sessions).toHaveLength(1);
});

test("rapid tab flapping while a create is in flight issues one POST", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  state.behaviors = [{ kind: "delay" }];
  await mockApi(page, state);
  await seedApp(page, `/servers/${SERVER_A}`);

  await expect(
    page.locator(".n-tabs-tab", { hasText: "Overview" }),
  ).toBeVisible();
  const terminalTab = page.locator(".n-tabs-tab", { hasText: "Terminal" });
  await terminalTab.click();
  await expect.poll(() => state.creates).toBe(1);

  // Ten switches, a double-click and arrow-key moves while the POST is held.
  const overview = page.locator(".n-tabs-tab", { hasText: "Overview" });
  for (let index = 0; index < 10; index += 1) {
    await overview.click();
    await terminalTab.click();
  }
  await terminalTab.dblclick();
  await terminalTab.focus();
  await page.keyboard.press("ArrowLeft");
  await page.keyboard.press("ArrowRight");
  expect(state.creates).toBe(1);
  expect(state.createServers).toEqual([SERVER_A]);

  // Release the single in-flight create; the session lands in the tab.
  state.releases.shift()?.();
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
  expect(state.creates).toBe(1);
});

test("cancelling a server switch keeps the shell, confirming closes only it", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state);
  await seedApp(page, `/terminal?server=${SERVER_A}`);
  await expect.poll(() => state.creates).toBe(1);

  const dialog = page.locator(".term-close-modal");
  await pickServer(page, "prod-02");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Keep terminals" }).click();
  await expect(dialog).toBeHidden();
  expect(state.creates).toBe(1);
  expect(state.deletes).toBe(0);
  await expect(page).toHaveURL(new RegExp(`/terminal\\?server=${SERVER_A}`));
  await expect(page.locator(".term-server-select")).toContainText("prod-01");

  await pickServer(page, "prod-02");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();
  await expect.poll(() => state.creates).toBe(2);
  expect(state.deletes).toBe(1);
  expect(state.createServers).toEqual([SERVER_A, SERVER_B]);
  await expect(page).toHaveURL(new RegExp(`/terminal\\?server=${SERVER_B}`));
  await expect(page.locator(".term-surface .term-state")).toHaveText("Connected");
});

test("signing out from /terminal lands on login with no forbidden redirect", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockApi(page, state);
  await seedApp(page, `/terminal?server=${SERVER_A}`);
  await expect.poll(() => state.creates).toBe(1);

  // The real account-menu path: sign-out confirms the live shell, then the
  // auth flow redirects to login. It must not be mistaken for a demotion.
  await page.locator(".topbar").getByRole("button", { name: "Account" }).click();
  await page.locator(".n-dropdown-option", { hasText: "Sign out" }).click();
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();

  await expect(page).toHaveURL(/\/login$/);
  expect(new URL(page.url()).searchParams.get("redirect")).toBeNull();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  expect(await tabCounts(page)).toEqual({ page: 0, window: 0 });
});

/** RAW_PATTERNS are backend/axios strings that must never reach the DOM. */
const RAW_PATTERNS = [
  "/srv/gotham/secret",
  "Request failed with status code",
  "Network Error",
];

/** expectNoRawText asserts no raw failure text in text or attributes. */
async function expectNoRawText(page: Page): Promise<void> {
  const html = await page.content();
  for (const pattern of RAW_PATTERNS) {
    expect(html).not.toContain(pattern);
  }
}

const rawFailures = [
  {
    name: "409 backend body",
    behavior: {
      kind: "body",
      status: 409,
      message: "scripted /srv/gotham/secret 409",
    } as const,
    bounded: "The terminal could not be started.",
  },
  {
    name: "503 backend body",
    behavior: {
      kind: "body",
      status: 503,
      message: "scripted /srv/gotham/secret 503",
    } as const,
    bounded: "The node agent is unavailable. Try again once it reconnects.",
  },
  {
    name: "500 axios text",
    behavior: { kind: "status", status: 500 } as const,
    bounded: "The terminal could not be started.",
  },
];

for (const failure of rawFailures) {
  test(`create failure (${failure.name}) never leaks raw text into the page DOM`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1280, height: 800 });
    const state = freshState();
    state.behaviors = [failure.behavior];
    await mockApi(page, state);
    await seedApp(page, `/terminal?server=${SERVER_A}`);

    await expect.poll(() => state.creates).toBe(1);
    await expect(page.getByText(failure.bounded)).toBeVisible();
    await expectNoRawText(page);
    await page.waitForTimeout(1000);
    expect(state.creates).toBe(1);
  });
}

test("raw backend text never reaches the detail tab or the paused dock", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  state.behaviors = [
    {
      kind: "body",
      status: 409,
      message: "scripted /srv/gotham/secret 409",
    },
  ];
  await mockApi(page, state);
  await seedApp(page, `/servers/${SERVER_A}?tab=terminal`);

  await expect.poll(() => state.creates).toBe(1);
  await expect(page.getByText("The terminal could not be started.")).toBeVisible();
  await expectNoRawText(page);

  // Leaving the route keeps the failed tab in the dock; the card must not
  // echo the backend message.
  await page.locator('a[href="/dashboard"]').first().click();
  await expect(page.locator(".term-dock")).toBeVisible();
  await expect(page.locator(".term-dock")).toContainText("Error");
  await expectNoRawText(page);
});

test("raw backend text never reaches the floating window", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  state.behaviors = [
    {
      kind: "body",
      status: 409,
      message: "scripted /srv/gotham/secret 409",
    },
  ];
  await mockApi(page, state);
  await seedApp(page, "/dashboard");
  await page.evaluate(async ({ id }) => {
    const app = (document.querySelector("#app") as unknown as {
      __vue_app__: {
        config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
      };
    }).__vue_app__;
    const store = app.config.globalProperties.$pinia._s.get("terminal") as {
      openWindowSession: (_options: Record<string, unknown>) => Promise<unknown>;
    };
    await store.openWindowSession({ serverId: id, serverName: "prod-01" });
  }, { id: SERVER_A });

  await expect(page.locator(".term-window")).toBeVisible();
  await expect(
    page.getByText("The terminal could not be started.").last(),
  ).toBeVisible();
  await expectNoRawText(page);
});
