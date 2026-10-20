import { Buffer } from "node:buffer";
import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page, WebSocketRoute } from "@playwright/test";

/* global Event:readonly, document:readonly, getComputedStyle:readonly, HTMLElement:readonly, localStorage:readonly, URL:readonly, window:readonly */
// URL/window run inside the page (browser globals); the repo lint envs do not
// cover tests/, so they are declared.

// Real-browser proof for JUS-245: the committed webdist is served statically,
// the control-plane terminal API is mocked, and Playwright's WebSocket router
// stands in for the PTY stream with the exact binary framing. Real Chromium
// types into xterm, drags and resizes the window, minimizes to the rail and
// measures the viewport at desktop and phone sizes.

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
    is_platform_admin: true,
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

const serverId = "33333333-3333-4333-8333-333333333333";

const serverRow = {
  id: serverId,
  name: "prod-01",
  ip: "10.0.0.1",
  status: "ready",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

const root = {
  name: "volumes",
  path: "/var/lib/gotham/volumes",
  writable: true,
  free_bytes: 21 * 1024 ** 3,
};

const entry = {
  name: "app.yaml",
  kind: "file",
  size: 24,
  mode: 0o100644,
  modified_at: "2026-10-12T09:02:00Z",
  etag: '"v1"',
};

/** fakeSession is one session the mocked HTTP + WS pair understands. */
interface FakeSession {
  id: string;
  cwd: string;
  input: string;
  socket: WebSocketRoute | null;
  closeFrames: number;
  /** opcodes records every client frame in order. */
  opcodes: number[];
  /** resizes records each 0x02 payload decoded from the bytes. */
  resizes: Array<{ cols: number; rows: number }>;
  heartbeats: number;
}

/** terminalState is the per-test mock ledger. */
interface TerminalState {
  sessions: FakeSession[];
  creates: number;
  deletes: number;
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

/** jsonFrame builds one JSON control frame. */
function jsonFrame(opcode: number, value: unknown): Buffer {
  return frame(opcode, Buffer.from(JSON.stringify(value), "utf8"));
}

/** openFrame confirms a PTY for one session. */
function openFrame(): Buffer {
  return jsonFrame(4, {
    shell: "/bin/bash",
    cols: 120,
    rows: 32,
    service_user: "gotham-agent",
  });
}

/** outputFrame writes one line of shell output. */
function outputFrame(text: string): Buffer {
  return frame(1, Buffer.from(text, "utf8"));
}

/** mockTerminalApi answers session create/delete and routes the PTY socket. */
async function mockTerminalApi(
  page: Page,
  state: TerminalState,
): Promise<void> {
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
    await route.fulfill({ json: { servers: [serverRow] } });
  });
  await page.route(`**/api/v1/servers/${serverId}`, async (route) => {
    await route.fulfill({ json: serverRow });
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
    const body = request.postDataJSON() as { cwd?: string };
    state.seq += 1;
    state.creates += 1;
    const id = `session-${state.seq}`;
    const cwd = body.cwd ?? "app";
    state.sessions.push({
      id,
      cwd,
      input: "",
      socket: null,
      closeFrames: 0,
      opcodes: [],
      resizes: [],
      heartbeats: 0,
    });
    await route.fulfill({
      status: 201,
      json: {
        id,
        server_id: serverId,
        target: "host",
        root: "volumes",
        cwd,
        cols: 120,
        rows: 32,
        expires_at: "2026-10-20T12:01:00Z",
        websocket_path: `/api/v1/servers/${serverId}/terminal/sessions/${id}/ws`,
        subprotocol: "gotham.terminal.v1",
      },
    });
  });

  // The PTY stream: binary frames only, exactly as the contract specifies.
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
        current.opcodes.push(bytes[0]!);
        if (bytes[0] === 1) {
          current.input += bytes.subarray(1).toString("utf8");
        } else if (bytes[0] === 2 && bytes.length === 5) {
          current.resizes.push({
            cols: bytes.readUInt16BE(1),
            rows: bytes.readUInt16BE(3),
          });
        } else if (bytes[0] === 3) {
          current.closeFrames += 1;
        } else if (bytes[0] === 4) {
          current.heartbeats += 1;
        }
      });
    },
  );

  // File manager routes, used by the editor-coexistence test.
  await page.route(`**/api/v1/servers/${serverId}/files/roots`, async (route) => {
    await route.fulfill({ json: { roots: [root] } });
  });
  await page.route(`**/api/v1/servers/${serverId}/files/list*`, async (route) => {
    await route.fulfill({
      json: { entries: [entry], truncated: false, cursor: "" },
    });
  });
  await page.route(`**/api/v1/servers/${serverId}/files/content*`, async (route) => {
    await route.fulfill({
      json: {
        content: "services:\n  web:\n",
        etag: '"v1"',
        sha256: "abc",
        size: 24,
        mode: 0o100644,
      },
    });
  });
}

/** seedApp seeds the session and opens one route. */
async function seedApp(page: Page, path: string): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  await page.goto(`${baseURL}${path}`);
  // main.ts mounts after the initial navigation resolves; wait for the shell
  // before reaching into the Pinia store.
  await expect(page.locator(".topbar")).toBeVisible();
}

/** storeCall runs one action on the live Pinia terminal store. */
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

/** countOf counts non-overlapping occurrences of one marker in the buffer. */
function countOf(text: string, marker: string): number {
  return text.split(marker).length - 1;
}

/** openWindow opens one floating-window session through the public store API. */
async function openWindow(
  page: Page,
  cwd: string,
): Promise<{ id: string; sessionId: string }> {
  return (await page.evaluate(async ({ serverId: id, dir }) => {
    const app = (document.querySelector("#app") as unknown as {
      __vue_app__: {
        config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
      };
    }).__vue_app__;
    const store = app.config.globalProperties.$pinia._s.get("terminal") as {
      openWindowSession: (_options: Record<string, unknown>) => Promise<{
        id: string;
        sessionId: string;
      }>;
    };
    return store.openWindowSession({
      serverId: id,
      serverName: "prod-01",
      root: "volumes",
      cwd: dir,
    });
  }, { serverId, dir: cwd })) as { id: string; sessionId: string };
}

/** tabState reads one live tab's reactive size/status from Pinia. */
async function tabState(
  page: Page,
  tabId: string,
): Promise<{ id: string; cols: number; rows: number; status: string }> {
  return page.evaluate((id) => {
    const app = (document.querySelector("#app") as unknown as {
      __vue_app__: {
        config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
      };
    }).__vue_app__;
    const store = app.config.globalProperties.$pinia._s.get("terminal") as {
      tabs: Array<{ id: string; cols: number; rows: number; status: string }>;
    };
    const tab = store.tabs.find((item) => item.id === id);
    if (!tab) {
      throw new Error(`tab ${id} missing`);
    }
    return { id: tab.id, cols: tab.cols, rows: tab.rows, status: tab.status };
  }, tabId);
}

/** terminalText reads the visible xterm rows. */
async function terminalText(page: Page): Promise<string> {
  return page
    .locator(".term-window .term-surface:visible .xterm-rows")
    .innerText();
}

/** boxOf returns the visible terminal window box. */
async function windowBox(page: Page): Promise<{
  x: number;
  y: number;
  width: number;
  height: number;
}> {
  const box = await page.locator(".term-window").boundingBox();
  if (!box) {
    throw new Error("terminal window has no box");
  }
  return box;
}

/** expectInsideViewport asserts the window keeps its margins and stays on screen. */
async function expectInsideViewport(
  page: Page,
  viewport: { width: number; height: number },
): Promise<void> {
  // A viewport change refits on the resize event; poll until the rendered box
  // reflects the new screen before measuring.
  await expect
    .poll(async () => {
      const box = await windowBox(page);
      return (
        box.x >= 0 &&
        box.y >= 0 &&
        box.x + box.width <= viewport.width + 0.5 &&
        box.y + box.height <= viewport.height + 0.5
      );
    })
    .toBe(true);
  const box = await windowBox(page);
  expect(box.width).toBeLessThanOrEqual(viewport.width);
}

/** freshState resets the mock ledger for one test. */
function freshState(): TerminalState {
  return { sessions: [], creates: 0, deletes: 0, seq: 0 };
}

test("two terminal tabs stay isolated through typing, switches and minimize", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");

  const first = await openWindow(page, "one");
  const second = await openWindow(page, "two");
  expect(state.creates).toBe(2);

  const window = page.locator(".term-window");
  await expect(window).toBeVisible();
  await expect(page.locator(".term-tab")).toHaveCount(2);
  await expect(
    page.locator(".term-window .term-surface:visible .term-state"),
  ).toHaveText("Connected");

  // The fitted renderer size must reach the PTY as one 0x02 frame right after
  // `opened`, and the footer must show that size (not the server's 120x32).
  await expect
    .poll(() => state.sessions[0]?.resizes.length ?? 0)
    .toBeGreaterThan(0);
  const firstState = await tabState(page, first.id);
  expect(state.sessions[0]?.resizes.at(-1)).toEqual({
    cols: firstState.cols,
    rows: firstState.rows,
  });
  expect(firstState.cols).not.toBe(120);
  await page
    .locator(".term-tab-name", { hasText: "prod-01 · one" })
    .click();
  await expect(
    page.locator(".term-window .term-surface:visible .term-statusbar"),
  ).toContainText(`${firstState.cols} × ${firstState.rows}`);
  await page
    .locator(".term-tab-name", { hasText: "prod-01 · two" })
    .click();

  // Type into the active (second) tab.
  await page.locator(".term-window .term-surface:visible .xterm-screen").click();
  await page.keyboard.type("echo two");
  await expect
    .poll(() => state.sessions.find((item) => item.id === second.sessionId)?.input)
    .toBe("echo two");

  // Switch to the first tab: the same keystrokes must land in its own PTY.
  await page
    .locator(".term-tab-name", { hasText: "prod-01 · one" })
    .click();
  await expect(
    page.locator(".term-window .term-surface:visible .xterm-screen"),
  ).toBeVisible();
  await page.locator(".term-window .term-surface:visible .xterm-screen").click();
  await page.keyboard.type("echo one");
  await expect
    .poll(() => state.sessions.find((item) => item.id === first.sessionId)?.input)
    .toBe("echo one");
  expect(
    state.sessions.find((item) => item.id === second.sessionId)?.input,
  ).toBe("echo two");

  // A window resize re-fits the visible renderer, sends a fresh 0x02 with the
  // new size, and typing still reaches the same session.
  const resizeCount = state.sessions.find(
    (item) => item.id === first.sessionId,
  )?.resizes.length ?? 0;
  await page.locator(".term-resize--se").focus();
  await page.keyboard.press("Shift+ArrowLeft");
  await page.keyboard.press("Shift+ArrowUp");
  await expect
    .poll(
      () =>
        state.sessions.find((item) => item.id === first.sessionId)?.resizes
          .length ?? 0,
    )
    .toBeGreaterThan(resizeCount);
  const resizedState = await tabState(page, first.id);
  expect(
    state.sessions
      .find((item) => item.id === first.sessionId)
      ?.resizes.at(-1),
  ).toEqual({ cols: resizedState.cols, rows: resizedState.rows });
  await page.locator(".term-window .term-surface:visible .xterm-screen").click();
  await page.keyboard.type(" pwd");
  await expect
    .poll(() => state.sessions.find((item) => item.id === first.sessionId)?.input)
    .toBe("echo one pwd");

  // Switching panes repeatedly does not swap or recreate sessions.
  for (let index = 0; index < 3; index += 1) {
    await page
      .locator(".term-tab-name", { hasText: "prod-01 · two" })
      .click();
    await page
      .locator(".term-tab-name", { hasText: "prod-01 · one" })
      .click();
  }
  expect(state.creates).toBe(2);

  // An in-app route change keeps both PTYs mounted in the app shell.
  await page.locator('a[href="/servers"]').first().click();
  await expect(page).toHaveURL(/\/servers$/);
  await expect(window).toBeVisible();
  await expect(page.locator(".term-tab")).toHaveCount(2);
  await page.locator('a[href="/dashboard"]').first().click();
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.locator(".term-tab")).toHaveCount(2);

  // Minimize → rail badge → restore keeps both live and both buffers.
  await page.locator(".term-window-min").click();
  await expect(window).toBeHidden();
  const rail = page.locator(".rail-btn--terminal");
  await expect(rail).toBeVisible();
  await expect(rail).toHaveAttribute("aria-label", "Terminal window, 2 open");
  await expect(rail.locator(".rail-badge")).toHaveText("2");
  await rail.click();
  await expect(window).toBeVisible();
  await expect(page.locator(".term-tab")).toHaveCount(2);
  expect(state.creates).toBe(2);

  // A real keyboard shortcut opens the confirmation, and cancel keeps both.
  await page
    .locator(".term-tab-name", { hasText: "prod-01 · one" })
    .focus();
  await page.keyboard.press("Delete");
  await expect(page.locator(".term-close-modal")).toBeVisible();
  await page.getByRole("button", { name: "Keep terminals" }).click();
  await expect(page.locator(".term-close-modal")).toBeHidden();
  await expect(page.locator(".term-tab")).toHaveCount(2);
  expect(state.deletes).toBe(0);
});

test("page → window transfer keeps the session, buffer and no new shell", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");

  const tab = (await storeCall(page, "openPageSession", {
    serverId,
    serverName: "prod-01",
    root: "volumes",
    cwd: "app",
  })) as { id: string; sessionId: string };
  expect(state.creates).toBe(1);
  // The page surface is not mounted in this package (JUS-246 owns the route);
  // the session still streams into its renderer, so the buffer is real.
  await expect.poll(() => state.sessions[0]?.socket !== null).toBe(true);

  // Transfer is one-way and reuses the tab: no POST, same live socket.
  await storeCall(page, "detachToWindow", tab.id);
  await expect(page.locator(".term-window")).toBeVisible();
  await expect(
    page.locator(".term-window .term-surface:visible .term-state"),
  ).toHaveText("Connected");
  expect(state.creates).toBe(1);
  await expect
    .poll(() => terminalText(page))
    .toContain(`welcome ${tab.sessionId}`);
  // The transfer kept the same renderer, and `opened` flushed its real size to
  // the PTY exactly as a direct window session does.
  await expect.poll(() => state.sessions[0]?.resizes.length ?? 0).toBeGreaterThan(0);
  const transferred = await tabState(page, tab.id);
  expect(state.sessions[0]?.resizes.at(-1)).toEqual({
    cols: transferred.cols,
    rows: transferred.rows,
  });

  // Returning to the page source returns the same detached tab; it must not
  // create a duplicate shell. No window → page action is offered.
  const reopened = (await storeCall(page, "openPageSession", {
    serverId,
    serverName: "prod-01",
    root: "volumes",
    cwd: "app",
  })) as { id: string; sessionId: string; mode: string };
  expect(reopened.id).toBe(tab.id);
  expect(reopened.sessionId).toBe(tab.sessionId);
  expect(reopened.mode).toBe("window");
  expect(state.creates).toBe(1);
  await expect(page.getByText("Open in window")).toHaveCount(0);
});

test("close confirmation cancel/confirm releases only the intended sessions", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");

  await openWindow(page, "one");
  await openWindow(page, "two");
  await expect(page.locator(".term-tab")).toHaveCount(2);

  // Window close → cancel keeps both PTYs running.
  await page.locator(".term-window-close").click();
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("Close all terminals?");
  await expect(dialog).toContainText("prod-01 · one");
  await expect(dialog).toContainText("prod-01 · two");
  await page.getByRole("button", { name: "Keep terminals" }).click();
  await expect(page.locator(".term-tab")).toHaveCount(2);
  expect(state.deletes).toBe(0);
  expect(state.sessions.every((item) => item.closeFrames === 0)).toBe(true);

  // The tab strip's close control is a pointer target inside the tab (the
  // tablist owns only role=tab elements) with a WCAG-sized hit box; keyboard
  // users close the focused tab with Delete, which the tab announces.
  const closeButton = page
    .locator(".term-tab", { hasText: "prod-01 · one" })
    .locator(".term-tab-close");
  await expect(closeButton).toHaveAttribute("aria-hidden", "true");
  const closeBox = await closeButton.boundingBox();
  expect(closeBox?.width ?? 0).toBeGreaterThanOrEqual(24);
  expect(closeBox?.height ?? 0).toBeGreaterThanOrEqual(24);
  await closeButton.click();
  await expect(dialog).toContainText("Close this terminal?");
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();
  await expect(page.locator(".term-tab")).toHaveCount(1);
  await expect(page.locator(".term-tab")).toContainText("prod-01 · two");
  expect(state.deletes).toBe(1);
  expect(state.sessions[0]?.closeFrames).toBe(1);
  expect(state.sessions[1]?.closeFrames).toBe(0);

  // Focus returns to the surviving tab, not to document.body.
  const remaining = await page.evaluate(() => {
    const app = (document.querySelector("#app") as unknown as {
      __vue_app__: {
        config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
      };
    }).__vue_app__;
    const store = app.config.globalProperties.$pinia._s.get("terminal") as {
      tabs: Array<{ id: string }>;
    };
    return store.tabs[0]?.id ?? "";
  });
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.id ?? ""))
    .toBe(`term-tab-${remaining}`);

  // Keyboard path: Delete on the focused tab opens the same confirmation.
  await page.locator(".term-tab-name").first().focus();
  await page.keyboard.press("Delete");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Keep terminals" }).click();
  await expect(page.locator(".term-tab")).toHaveCount(1);

  // Closing the window confirms the remaining session and removes the badge.
  await page.locator(".term-window-close").click();
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();
  await expect(page.locator(".term-window")).toBeHidden();
  await expect(page.locator(".rail-btn--terminal")).toHaveCount(0);
  expect(state.deletes).toBe(2);
  expect(state.sessions[1]?.closeFrames).toBe(1);
});

test("the window moves, resizes and stays bounded at desktop and phone sizes", async ({
  page,
}) => {
  const state = freshState();
  await mockTerminalApi(page, state);
  await page.setViewportSize({ width: 1280, height: 800 });
  await seedApp(page, "/dashboard");
  await openWindow(page, "a/deep/working/directory");
  await expect(page.locator(".term-window")).toBeVisible();
  await expectInsideViewport(page, { width: 1280, height: 800 });

  // Drag the title bar: the window follows the pointer, clamped on screen.
  const before = await windowBox(page);
  const bar = await page.locator(".term-window-bar").boundingBox();
  await page.mouse.move(bar!.x + 24, bar!.y + 20);
  await page.mouse.down();
  await page.mouse.move(bar!.x + 164, bar!.y + 80, { steps: 5 });
  await page.mouse.up();
  const after = await windowBox(page);
  expect(after.x).toBeGreaterThan(before.x);
  expect(after.y).toBeGreaterThan(before.y);
  await expectInsideViewport(page, { width: 1280, height: 800 });

  // Drag far past the bottom-right edge: it never leaves the viewport.
  const movedBar = await page.locator(".term-window-bar").boundingBox();
  await page.mouse.move(movedBar!.x + 24, movedBar!.y + 20);
  await page.mouse.down();
  await page.mouse.move(2000, 2000, { steps: 6 });
  await page.mouse.up();
  await expectInsideViewport(page, { width: 1280, height: 800 });

  // Keyboard resize through the SE slider grows the window, still bounded.
  const grip = page.locator(".term-resize--se");
  await grip.focus();
  const beforeResize = await windowBox(page);
  await page.keyboard.press("Shift+ArrowLeft");
  await page.keyboard.press("Shift+ArrowUp");
  const afterResize = await windowBox(page);
  expect(afterResize.width).toBeLessThan(beforeResize.width);
  expect(afterResize.height).toBeLessThan(beforeResize.height);
  await expectInsideViewport(page, { width: 1280, height: 800 });

  // 1366 keeps the same bound.
  await page.setViewportSize({ width: 1366, height: 800 });
  await expectInsideViewport(page, { width: 1366, height: 800 });

  // 390px phone: the window fills the width within its margins, the meta row
  // keeps the working directory readable instead of clipping it, and the
  // minimized session is restorable from the mobile rail drawer.
  await page.setViewportSize({ width: 390, height: 844 });
  await expectInsideViewport(page, { width: 390, height: 844 });
  const cwdValue = page.locator(
    ".term-window .term-surface:visible .term-cwd .mono",
  );
  await expect(cwdValue).toBeVisible();
  const cwdOverflow = await cwdValue.evaluate((element) => {
    const node = element as HTMLElement;
    return node.scrollWidth - node.clientWidth;
  });
  expect(cwdOverflow).toBeLessThanOrEqual(1);
  await page.locator(".term-window-min").click();
  await expect(page.locator(".term-window")).toBeHidden();
  await page.locator(".rail-toggle").click();
  const drawer = page.locator(".n-drawer");
  await expect(drawer).toBeVisible();
  await drawer.locator(".rail-btn--terminal").click();
  await expect(page.locator(".term-window")).toBeVisible();
  await expectInsideViewport(page, { width: 390, height: 844 });

  // 480px keeps the same contract.
  await page.setViewportSize({ width: 480, height: 900 });
  await expectInsideViewport(page, { width: 480, height: 900 });
  expect(state.creates).toBe(1);
});

test("the terminal window coexists with the code editor without replacing it", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, `/servers/${serverId}/files`);

  const label = page.locator(".fm-label", { hasText: "app.yaml" }).first();
  await expect(label).toBeVisible();
  await label.dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  await expect(page.locator(".ed-window")).toBeVisible();

  await openWindow(page, "app");
  const terminal = page.locator(".term-window");
  await expect(terminal).toBeVisible();
  // Both floating windows and both rail controls exist at once.
  await expect(page.locator(".ed-window")).toBeVisible();
  await page.locator(".term-window-min").click();
  await expect(page.locator(".rail-btn--terminal")).toBeVisible();
  await expect(page.locator(".rail-btn--editor")).toBeVisible();
  await page.locator(".rail-btn--terminal").click();
  await expect(terminal).toBeVisible();

  // Clicking the editor lowers the terminal beneath it; clicking the terminal
  // raises it again, so neither surface permanently covers the other.
  await page.locator(".ed-window-bar").click({ position: { x: 200, y: 12 } });
  const lowered = await terminal.evaluate((element) =>
    Number.parseInt(getComputedStyle(element as HTMLElement).zIndex, 10),
  );
  // The rail icon is always reachable: with the window visible but behind the
  // editor it raises it instead of merely minimizing it.
  await page.locator(".rail-btn--terminal").click();
  const raised = await terminal.evaluate((element) =>
    Number.parseInt(getComputedStyle(element as HTMLElement).zIndex, 10),
  );
  expect(raised).toBeGreaterThan(lowered);
  await expect(terminal).toBeVisible();

  // The editor still owns its buffer after all the terminal activity.
  await expect(page.locator(".cm-content")).toBeVisible();
  await page.locator(".ed-window-min").click();
  await expect(page.locator(".ed-window")).toBeHidden();
  await expect(terminal).toBeVisible();
  expect(state.creates).toBe(1);
});


test("heartbeat, disconnect, exit and error states use real frames", async ({
  page,
}) => {
  await page.clock.install();
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");

  await openWindow(page, "app");
  const status = page.locator(".term-window .term-surface:visible .term-state");
  await expect(status).toHaveText("Connected");
  const first = state.sessions[0]!;

  // The 25s client heartbeat is on the wire (0x04), not just a README claim.
  await page.clock.fastForward(25_500);
  await expect.poll(() => first.heartbeats).toBeGreaterThanOrEqual(1);

  // A transport close is reported as disconnected; nothing reconnects itself.
  await first.socket!.close({ code: 1006 });
  await expect(status).toHaveText("Disconnected");
  await page.clock.fastForward(5_000);
  expect(state.creates).toBe(1);

  // Retry is explicit and creates exactly one new session.
  await page.getByRole("button", { name: "Start new session" }).click();
  await expect.poll(() => state.creates).toBe(2);
  await expect(status).toHaveText("Connected");

  // The shell exit carries its code and offers a new session.
  const second = state.sessions[1]!;
  second.socket!.send(jsonFrame(2, { exit_code: 7, signal: "" }));
  await expect(status).toHaveText("Exited");
  await expect(page.getByText("The shell exited with code 7.")).toBeVisible();

  await page.getByRole("button", { name: "Start new session" }).click();
  await expect.poll(() => state.creates).toBe(3);
  const third = state.sessions[2]!;
  third.socket!.send(
    frame(3, Buffer.from("the node agent is unavailable", "utf8")),
  );
  await expect(status).toHaveText("Error");
  // JUS-248 r1: the overlay renders bounded catalog copy only. The relay phrase
  // can carry host detail, so the raw text must never reach the DOM.
  await expect(
    page.getByText("The node agent is unavailable. Try again once it reconnects."),
  ).toBeVisible();
  await expect(page.locator(".term-window")).not.toContainText(
    "the node agent is unavailable",
  );
});

test("shortcuts are discoverable and the documented escape leaves xterm", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");
  const tab = await openWindow(page, "app");
  await expect(
    page.locator(".term-window .term-surface:visible .term-state"),
  ).toHaveText("Connected");

  await page.locator(".term-shortcuts-btn").click();
  const shortcuts = page.locator(".term-shortcuts");
  await expect(shortcuts).toBeVisible();
  await expect(shortcuts).toContainText("Ctrl+Shift+E");
  await expect(shortcuts).toContainText("Alt+Shift+W");
  // WCAG 2.5.8: the shortcuts trigger is at least 24x24.
  const shortcutsBox = await page.locator(".term-shortcuts-btn").boundingBox();
  expect(shortcutsBox?.width ?? 0).toBeGreaterThanOrEqual(24);
  expect(shortcutsBox?.height ?? 0).toBeGreaterThanOrEqual(24);
  // Toggle the popover closed deterministically (Escape focus handling differs
  // across platforms), then drive the real xterm input textarea.
  await page.locator(".term-shortcuts-btn").click();
  await expect(shortcuts).toBeHidden();
  const input = page.locator(
    ".term-window .term-surface:visible .xterm-helper-textarea",
  );
  await input.focus();

  // The chord pulls focus out of the shell onto the window tab.
  await page.keyboard.press("Control+Shift+E");
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.id ?? ""))
    .toBe(`term-tab-${tab.id}`);

  // Inside the shell, Alt/Option chords belong to the PTY (macOptionIsMeta):
  // they must not open the close dialog or minimize the window.
  await input.focus();
  await expect
    .poll(() =>
      page.evaluate(() =>
        document.activeElement?.classList.contains("xterm-helper-textarea") ?? false,
      ),
    )
    .toBe(true);
  const inputBefore = state.sessions[0]!.input.length;
  await page.keyboard.press("Alt+Shift+W");
  await expect(page.locator(".term-close-modal")).toBeHidden();
  await expect(page.locator(".term-window")).toBeVisible();
  await expect
    .poll(() => state.sessions[0]!.input.length)
    .toBeGreaterThan(inputBefore);
  expect(state.sessions[0]!.input).toContain("\u001b");

  // The window chords are documented for the window chrome. Focus the tab and
  // use Alt+Shift+W: the shared dialog opens; cancel keeps the session.
  await page.locator(`#term-tab-${tab.id}`).focus();
  await page.keyboard.press("Alt+Shift+W");
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Keep terminals" }).click();
  await expect(page.locator(".term-tab")).toHaveCount(1);

  // Alt+M from page chrome does nothing; from the shell it types Option+M;
  // from the window chrome it minimizes to the rail.
  await page.locator(".topbar").click();
  await page.keyboard.press("Alt+M");
  await expect(page.locator(".term-window")).toBeVisible();
  await input.focus();
  const beforeMeta = state.sessions[0]!.input.length;
  await page.keyboard.press("Alt+M");
  await expect(page.locator(".term-window")).toBeVisible();
  await expect
    .poll(() => state.sessions[0]!.input.length)
    .toBeGreaterThan(beforeMeta);
  await page.locator(`#term-tab-${tab.id}`).focus();
  await page.keyboard.press("Alt+M");
  await expect(page.locator(".term-window")).toBeHidden();
  await expect(page.locator(".rail-btn--terminal")).toBeVisible();
  await page.locator(".rail-btn--terminal").click();
  await expect(page.locator(".term-window")).toBeVisible();
});

test("signing out confirms live shells and cancel aborts the logout", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");
  await openWindow(page, "app");
  await expect(
    page.locator(".term-window .term-surface:visible .term-state"),
  ).toHaveText("Connected");

  const signOut = async (): Promise<void> => {
    await page.locator(".topbar").getByRole("button", { name: "Account" }).click();
    await page
      .locator(".n-dropdown-option", { hasText: "Sign out" })
      .click();
  };

  await signOut();
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("Close this terminal?");
  await dialog.getByRole("button", { name: "Keep terminals" }).click();
  await expect(dialog).toBeHidden();
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.locator(".term-tab")).toHaveCount(1);
  expect(state.sessions[0]?.closeFrames).toBe(0);
  expect(state.deletes).toBe(0);

  // A confirmed sign-out releases the shell and lands on the login page.
  await signOut();
  await expect(dialog).toContainText("Close this terminal?");
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();
  await expect(page).toHaveURL(/\/login$/);
  expect(state.sessions[0]?.closeFrames).toBe(1);
});

test("beforeunload only warns while a session is live", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");
  await openWindow(page, "app");
  await expect(
    page.locator(".term-window .term-surface:visible .term-state"),
  ).toHaveText("Connected");

  const warnedLive = await page.evaluate(() => {
    const event = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  });
  expect(warnedLive).toBe(true);

  await page.locator(".term-window-close").click();
  await page
    .locator(".term-close-modal")
    .getByRole("button", { name: "Close terminal", exact: true })
    .click();
  await expect(page.locator(".term-window")).toBeHidden();

  const warnedClosed = await page.evaluate(() => {
    const event = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  });
  expect(warnedClosed).toBe(false);
});

test("the theme applies immediately and survives page → window without a new session", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");

  // A page-mode session has no mounted route in JUS-250 (JUS-246 owns it), but
  // its renderer and PTY are live; the preference is the public store API.
  const tab = (await storeCall(page, "openPageSession", {
    serverId,
    serverName: "prod-01",
    root: "volumes",
    cwd: "app",
  })) as { id: string; sessionId: string };
  await expect.poll(() => state.sessions[0]?.socket !== null).toBe(true);
  await storeCall(page, "setTheme", "gotham-light");

  // Account-scoped persistence, not a global key.
  expect(
    await page.evaluate(() =>
      localStorage.getItem("gotham.terminal.appearance.v1.u-1"),
    ),
  ).toContain('"theme":"gotham-light"');
  expect(
    await page.evaluate(() =>
      localStorage.getItem("gotham.terminal.appearance.v1"),
    ),
  ).toBeNull();

  // The transfer keeps the session, buffer and chosen palette.
  await storeCall(page, "detachToWindow", tab.id);
  const surface = page.locator(".term-window .term-surface:visible");
  await expect(page.locator(".term-window")).toBeVisible();
  await expect(surface).toHaveAttribute("data-theme", "gotham-light");
  // The frame around the xterm canvas follows the light theme instead of
  // leaving an 8px dark gutter.
  await expect
    .poll(() =>
      surface.evaluate((element) =>
        getComputedStyle(element as HTMLElement).backgroundColor,
      ),
    )
    .toBe("rgb(247, 248, 250)");
  await expect.poll(() => terminalText(page)).toContain("welcome session-1");
  expect(state.creates).toBe(1);

  // Choosing through the real control repaints in place: the PTY and buffer
  // are untouched and the banner was not replayed by the transfer.
  await surface.locator(".term-theme-btn").click();
  const group = page.getByRole("radiogroup", { name: "Terminal theme" });
  await expect(group).toBeVisible();
  await expect(group.getByRole("radio", { name: "Gotham Light" })).toBeChecked();
  await group.getByRole("radio", { name: "Gotham Dark" }).click();
  await expect(surface).toHaveAttribute("data-theme", "gotham-dark");
  await page.locator(".term-window-title").click();
  await expect(page.getByRole("radiogroup", { name: "Terminal theme" })).toBeHidden();
  expect(state.creates).toBe(1);
  expect((await tabState(page, tab.id)).status).toBe("connected");
  await expect
    .poll(async () => countOf(await terminalText(page), "Gotham v0.2.1-dev"))
    .toBe(1);

  // Minimize/restore is not a new session and keeps the chosen theme.
  await page.locator(".term-window-min").click();
  await expect(page.locator(".term-window")).toBeHidden();
  await page.locator(".rail-btn--terminal").click();
  await expect(page.locator(".term-window")).toBeVisible();
  await expect(surface).toHaveAttribute("data-theme", "gotham-dark");
  expect(state.creates).toBe(1);
});

test("the theme menu is keyboard operable and never types into the shell", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");
  const tab = await openWindow(page, "keys");
  await expect(
    page.locator(".term-window .term-surface:visible .term-state"),
  ).toHaveText("Connected");

  const session = state.sessions[0]!;
  // Ignore the initial fitted-size frame; any 0x01 after this is leaked input.
  session.opcodes.length = 0;
  session.input = "";
  const surface = page.locator(".term-window .term-surface:visible");
  const trigger = surface.locator(".term-theme-btn");
  const group = page.getByRole("radiogroup", { name: "Terminal theme" });

  await trigger.focus();
  await page.keyboard.press("Enter");
  await expect(group).toBeVisible();
  // Focus lands on the checked radio, not xterm's textarea.
  await expect(group.getByRole("radio", { name: "Gotham Dark" })).toBeFocused();

  // Arrows move the native radio group and apply the theme as they go.
  await page.keyboard.press("ArrowDown");
  await expect(surface).toHaveAttribute("data-theme", "gotham-light");
  await page.keyboard.press("ArrowUp");
  await expect(surface).toHaveAttribute("data-theme", "gotham-dark");
  await page.keyboard.press("Space");
  await page.keyboard.press("Enter");
  // A printable key while the menu owns focus must not become PTY input.
  await page.keyboard.press("a");
  await page.keyboard.press("Tab");
  const tabLandedInShell = await page.evaluate(
    () =>
      document.activeElement?.classList.contains("xterm-helper-textarea") ??
      false,
  );
  expect(tabLandedInShell).toBe(false);
  await trigger.focus();
  await page.keyboard.press("Escape");
  await expect(group).toBeHidden();
  await expect(trigger).toBeFocused();

  // Reopen with Space on the trigger, pick the light theme, Escape back.
  await page.keyboard.press("Space");
  await expect(group).toBeVisible();
  await page.keyboard.press("ArrowDown");
  await expect(surface).toHaveAttribute("data-theme", "gotham-light");
  await page.keyboard.press("Escape");
  await expect(group).toBeHidden();
  await expect(trigger).toBeFocused();

  // The shell never saw a keystroke from the menu, and the PTY is unchanged.
  expect(session.opcodes.filter((opcode) => opcode === 1)).toEqual([]);
  expect(session.input).toBe("");
  expect(state.creates).toBe(1);
  expect((await tabState(page, tab.id)).status).toBe("connected");
});

test("the host banner shows the exact logo, real version and server once", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");

  // The default window is ~100 columns: enough for the 63-cell logo, so a
  // fresh session must show the artwork without any resizing.
  const first = await openWindow(page, "one");
  await expect(page.locator(".term-window")).toBeVisible();
  await expect.poll(() => state.sessions[0]?.resizes.length ?? 0).toBeGreaterThan(0);
  await expect.poll(() => terminalText(page)).toContain("▓");
  const banner = await terminalText(page);
  expect(banner).toContain("Gotham v0.2.1-dev");
  expect(banner).toContain("server: prod-01");
  expect(countOf(banner, "Gotham v0.2.1-dev")).toBe(1);

  // A second tab, tab switches, a viewport resize, minimize/restore and a
  // theme change must not replay the banner into the first tab.
  await openWindow(page, "two");
  await page.locator(".term-tab-name", { hasText: "prod-01 · one" }).click();
  await expect.poll(() => surfaceToText(page)).toContain("Gotham v0.2.1-dev");
  await page.setViewportSize({ width: 1600, height: 900 });
  await expect.poll(() => state.sessions[0]?.resizes.length ?? 0).toBeGreaterThan(0);
  await page.locator(".term-window-min").click();
  await page.locator(".rail-btn--terminal").click();
  await page
    .locator(".term-window .term-surface:visible .term-theme-btn")
    .click();
  await page.getByRole("radio", { name: "Gotham Light" }).click();
  await page.locator(".term-window-title").click();
  const firstTab = page.locator(".term-tab-name", { hasText: "prod-01 · one" });
  await firstTab.click();
  await expect
    .poll(async () => countOf(await terminalText(page), "Gotham v0.2.1-dev"))
    .toBe(1);
  expect((await tabState(page, first.id)).status).toBe("connected");
  expect(state.creates).toBe(2);

  // A container target is not a host shell: no Gotham banner at all.
  const container = (await storeCall(page, "openWindowSession", {
    serverId,
    serverName: "prod-01",
    target: "container:web-1",
    cwd: "container",
  })) as { sessionId: string };
  await expect.poll(() => terminalText(page)).toContain(
    `welcome ${container.sessionId}`,
  );
  const containerText = await terminalText(page);
  expect(containerText).not.toContain("Gotham");
  expect(containerText).not.toContain("▓");

  // A phone-width window gets the compact banner: no artwork, no wrapped
  // title, and the prompt still follows it.
  await page.setViewportSize({ width: 390, height: 844 });
  const narrow = await openWindow(page, "narrow");
  await expect.poll(() => terminalText(page)).toContain(
    `welcome ${narrow.sessionId}`,
  );
  const narrowText = await terminalText(page);
  expect(narrowText).not.toContain("▓");
  expect(countOf(narrowText, "Gotham v0.2.1-dev")).toBe(1);
  expect(narrowText).toContain("server: prod-01");
  expect(state.creates).toBe(4);
});

/** surfaceToText reads the active window pane's xterm rows. */
async function surfaceToText(page: Page): Promise<string> {
  return page
    .locator(".term-window .term-surface:visible .xterm-rows")
    .innerText();
}

test("quick command suggestions survive a theme change and page → window move", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");

  // Seed one command for the signed-in account before the runtime starts.
  await page.evaluate(() => {
    window.localStorage.setItem(
      "gotham.terminal.commands.v1.u-1",
      JSON.stringify({
        version: 1,
        commands: [
          {
            id: "c1",
            label: "Git status",
            command: "git status",
            abbreviation: "gs",
          },
        ],
      }),
    );
  });

  // Start a page session, choose a theme, then transfer the live session to
  // the floating window. The suggestion must still work on the same PTY.
  const tab = (await storeCall(page, "openPageSession", {
    serverId,
    serverName: "prod-01",
    root: "volumes",
    cwd: "app",
  })) as { id: string };
  await expect.poll(() => state.sessions[0]?.socket !== null).toBe(true);
  await storeCall(page, "setTheme", "gotham-light");
  await storeCall(page, "detachToWindow", tab.id);

  const surface = page.locator(".term-window .term-surface:visible");
  await expect(surface).toHaveAttribute("data-theme", "gotham-light");
  state.sessions[0]?.socket?.send(outputFrame("\x1b]133;B\x07"));
  const input = surface.locator(".xterm-helper-textarea");
  await input.focus();
  await page.keyboard.type("gs");
  const suggestion = surface.locator(".term-suggest-option");
  await expect(suggestion).toBeVisible();

  // A theme change repaints in place and does not disturb the suggestion.
  await storeCall(page, "setTheme", "gotham-dark");
  await expect(surface).toHaveAttribute("data-theme", "gotham-dark");
  await expect(suggestion).toBeVisible();
  expect(state.creates).toBe(1);

  await page.keyboard.press("Control+Shift+Space");
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.getAttribute("role") ?? ""))
    .toBe("option");
  await page.keyboard.press("Enter");
  await expect
    .poll(() => state.sessions[0]?.input ?? "")
    .toContain("gs\x7f\x7fgit status");
  expect(state.sessions[0]?.input ?? "").not.toContain("\r");
  expect(state.creates).toBe(1);
});

test("quick commands save, insert without Enter, and suggest an abbreviation", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");
  const tab = await openWindow(page, "app");
  await expect(
    page.locator(".term-window .term-surface:visible .term-state"),
  ).toHaveText("Connected");

  // Add one command through the panel.
  await page.locator(".term-commands-btn").click();
  await expect(page.getByText("Quick commands", { exact: true })).toBeVisible();

  // The editor stays inside a phone viewport. Popper repositions after the
  // resize, so poll for the settled box instead of measuring mid-move.
  for (const width of [390, 480]) {
    await page.setViewportSize({ width, height: 800 });
    await expect
      .poll(async () => {
        const rect = await page.locator(".qc").boundingBox();
        return rect?.x ?? -1;
      })
      .toBeGreaterThanOrEqual(0);
    const box = await page.locator(".qc").boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
  }
  await page.setViewportSize({ width: 1280, height: 800 });

  await page.getByRole("button", { name: "Add command" }).click();
  await page.locator(".qc-form input").first().fill("Git status");
  await page.locator(".qc-form textarea").fill("git status");
  await page.locator(".qc-form input").nth(1).fill("gs");
  await page.getByRole("button", { name: "Save command" }).click();
  await expect(page.locator(".qc-item")).toHaveCount(1);
  await expect(page.locator(".qc-item")).toContainText("git status");

  // Selecting it inserts text only.
  await page.locator(".qc-insert").click();
  await expect.poll(() => state.sessions[0]?.input ?? "").toContain("git status");
  expect(state.sessions[0]?.input ?? "").not.toContain("\r");
  const afterInsert = state.sessions[0]?.input.length ?? 0;
  await page.waitForTimeout(50);
  expect(state.sessions[0]?.input.length).toBe(afterInsert);

  // A real prompt mark enables the abbreviation suggestion. Typing `gs` is
  // forwarded; Ctrl+Shift+Space focuses the option; Enter accepts it.
  state.sessions[0]?.socket?.send(outputFrame("\x1b]133;B\x07"));
  const input = page.locator(
    ".term-window .term-surface:visible .xterm-helper-textarea",
  );
  await input.focus();
  await page.keyboard.type("gs");
  const suggestion = page.locator(".term-window .term-suggest-option");
  await expect(suggestion).toBeVisible();
  await expect(suggestion).toContainText("Git status");
  // The live announcement is exposed to AT but visually hidden.
  const live = page.locator(".term-window .term-suggest .term-sr");
  await expect(live).toHaveAttribute("role", "status");
  await expect(live).toHaveAttribute("aria-live", "polite");
  const liveBox = await live.boundingBox();
  expect(liveBox?.width ?? 0).toBeLessThanOrEqual(1);
  expect(liveBox?.height ?? 0).toBeLessThanOrEqual(1);

  // The strip stays inside the viewport at phone and desktop widths and never
  // covers the xterm viewport (the prompt line).
  for (const width of [390, 480, 1280, 1366]) {
    await page.setViewportSize({ width, height: 800 });
    await expect(suggestion).toBeVisible();
    await expect
      .poll(async () => {
        const rect = await page.locator(".term-window .term-suggest").boundingBox();
        return rect?.x ?? -1;
      })
      .toBeGreaterThanOrEqual(0);
    // The strip is above the xterm viewport; poll because a viewport resize
    // reflows the window and the boxes settle on the next frame.
    await expect
      .poll(async () => {
        const nextBox = await page
          .locator(".term-window .term-suggest")
          .boundingBox();
        const nextScreen = await page
          .locator(".term-window .term-surface:visible .term-screen")
          .boundingBox();
        if (!nextBox || !nextScreen) {
          return false;
        }
        return (
          nextBox.x >= 0 &&
          nextBox.x + nextBox.width <= width + 1 &&
          nextBox.y + nextBox.height <= nextScreen.y + 1
        );
      })
      .toBe(true);
  }
  await page.setViewportSize({ width: 1280, height: 800 });
  await input.focus();

  await page.keyboard.press("Control+Shift+Space");
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.getAttribute("role") ?? ""))
    .toBe("option");
  await page.keyboard.press("Enter");

  await expect
    .poll(() => state.sessions[0]?.input ?? "")
    .toContain("gs\x7f\x7fgit status");
  expect(state.sessions[0]?.input ?? "").not.toContain("\r");
  await expect(suggestion).toBeHidden();

  // A fresh prompt (mark) is required after accept; then Escape is forwarded
  // to the shell (vi normal mode) and also dismisses the suggestion.
  await page.waitForTimeout(100);
  const resizesBefore = state.sessions[0]?.resizes.length ?? 0;
  const screenBefore = await page
    .locator(".term-window .term-surface:visible .term-screen")
    .boundingBox();
  state.sessions[0]?.socket?.send(outputFrame("\x1b]133;A\x07\x1b]133;B\x07"));
  await input.focus();
  await page.keyboard.type("gs");
  await expect(suggestion).toBeVisible();
  const screenShown = await page
    .locator(".term-window .term-surface:visible .term-screen")
    .boundingBox();
  expect(screenBefore).not.toBeNull();
  expect(screenShown).not.toBeNull();
  expect(state.sessions[0]?.resizes.length).toBe(resizesBefore);
  expect(screenShown?.height).toBe(screenBefore?.height);

  const beforeEscape = state.sessions[0]?.input ?? "";
  await page.keyboard.press("Escape");
  await expect(suggestion).toBeHidden();
  await expect
    .poll(() => state.sessions[0]?.input ?? "")
    .toBe(`${beforeEscape}\x1b`);
  const screenHidden = await page
    .locator(".term-window .term-surface:visible .term-screen")
    .boundingBox();
  expect(screenHidden).not.toBeNull();
  expect(state.sessions[0]?.resizes.length).toBe(resizesBefore);
  expect(screenHidden?.height).toBe(screenBefore?.height);

  // The list survives a reload for the same account. Close the live shell
  // first so the beforeunload guard does not prompt during the reload.
  await page.locator(".term-window-close").click();
  await page
    .locator(".term-close-modal")
    .getByRole("button", { name: "Close terminal", exact: true })
    .click();
  await expect(page.locator(".term-window")).toBeHidden();
  await page.reload();
  await expect(page.locator(".topbar")).toBeVisible();
  const reopened = await openWindow(page, "app");
  expect(reopened.id).toBeTruthy();
  await page.locator(".term-commands-btn").click();
  await expect(page.locator(".qc-item")).toHaveCount(1);
  await expect(page.locator(".qc-item")).toContainText("git status");
  expect(tab.id).toBeTruthy();
});
