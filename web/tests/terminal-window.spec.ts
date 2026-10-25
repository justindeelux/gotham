import { Buffer } from "node:buffer";
import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page, WebSocketRoute } from "@playwright/test";

/* global Element:readonly, Event:readonly, document:readonly, getComputedStyle:readonly, HTMLElement:readonly, localStorage:readonly, URL:readonly, window:readonly */

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

interface FakeSession {
  id: string;
  cwd: string;
  input: string;
  socket: WebSocketRoute | null;
  closeFrames: number;
  opcodes: number[];
  resizes: Array<{ cols: number; rows: number }>;
  heartbeats: number;
}

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

function frame(opcode: number, payload: Buffer = Buffer.alloc(0)): Buffer {
  return Buffer.concat([Buffer.from([opcode]), payload]);
}

function jsonFrame(opcode: number, value: unknown): Buffer {
  return frame(opcode, Buffer.from(JSON.stringify(value), "utf8"));
}

function openFrame(): Buffer {
  return jsonFrame(4, {
    shell: "/bin/bash",
    cols: 120,
    rows: 32,
    service_user: "gotham-agent",
  });
}

function outputFrame(text: string): Buffer {
  return frame(1, Buffer.from(text, "utf8"));
}

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

async function seedApp(page: Page, path: string): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
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

function countOf(text: string, marker: string): number {
  return text.split(marker).length - 1;
}

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

async function terminalText(page: Page): Promise<string> {
  return page
    .locator(".term-window .term-surface:visible .xterm-rows")
    .innerText();
}

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

async function expectInsideViewport(
  page: Page,
  viewport: { width: number; height: number },
): Promise<void> {
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

async function floatingShadow(
  page: Page,
  selector: string,
): Promise<{ actual: string; expected: string; raised: string }> {
  return page.evaluate((target) => {
    const element = document.querySelector(target);
    if (!(element instanceof HTMLElement)) {
      throw new Error(`${target} is missing`);
    }
    const resolve = (name: string): string => {
      const raw = getComputedStyle(document.documentElement)
        .getPropertyValue(name)
        .trim();
      if (!raw) {
        throw new Error(`${name} is missing`);
      }
      const probe = document.createElement("div");
      probe.style.boxShadow = raw;
      document.body.append(probe);
      const value = getComputedStyle(probe).boxShadow;
      probe.remove();
      return value;
    };
    return {
      actual: getComputedStyle(element).boxShadow,
      expected: resolve("--elev-floating"),
      raised: resolve("--elev-raised"),
    };
  }, selector);
}

async function expectDialogActionGap(page: Page): Promise<void> {
  await expect
    .poll(async () =>
      page.evaluate(() => {
        const button = document.querySelector(
          ".term-close-modal .dialog-foot button",
        );
        if (!button) {
          return 0;
        }
        const rect = button.getBoundingClientRect();
        const width = Number.parseFloat(getComputedStyle(button).width);
        return width === 0 ? 0 : rect.width / width;
      }),
    )
    .toBeGreaterThan(0.99);

  const gap = await page.evaluate(() => {
    const buttons = [
      ...document.querySelectorAll(".term-close-modal .dialog-foot button"),
    ];
    const first = buttons[0]?.getBoundingClientRect();
    const last = buttons.at(-1)?.getBoundingClientRect();
    const token = Number.parseFloat(
      getComputedStyle(document.documentElement).getPropertyValue("--space-2"),
    );
    return {
      actual: first && last ? last.left - first.right : -1,
      token,
    };
  });
  expect(gap.actual).toBeGreaterThanOrEqual(gap.token - 0.5);
}

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

  await page.locator(".term-window .term-surface:visible .xterm-screen").click();
  await page.keyboard.type("echo two");
  await expect
    .poll(() => state.sessions.find((item) => item.id === second.sessionId)?.input)
    .toBe("echo two");

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

  for (let index = 0; index < 3; index += 1) {
    await page
      .locator(".term-tab-name", { hasText: "prod-01 · two" })
      .click();
    await page
      .locator(".term-tab-name", { hasText: "prod-01 · one" })
      .click();
  }
  expect(state.creates).toBe(2);

  await page.locator('a[href="/servers"]').first().click();
  await expect(page).toHaveURL(/\/servers$/);
  await expect(window).toBeVisible();
  await expect(page.locator(".term-tab")).toHaveCount(2);
  await page.locator('a[href="/dashboard"]').first().click();
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.locator(".term-tab")).toHaveCount(2);

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
  await expect.poll(() => state.sessions[0]?.socket !== null).toBe(true);

  await storeCall(page, "detachToWindow", tab.id);
  await expect(page.locator(".term-window")).toBeVisible();
  await expect(
    page.locator(".term-window .term-surface:visible .term-state"),
  ).toHaveText("Connected");
  expect(state.creates).toBe(1);
  await expect
    .poll(() => terminalText(page))
    .toContain(`welcome ${tab.sessionId}`);
  await expect.poll(() => state.sessions[0]?.resizes.length ?? 0).toBeGreaterThan(0);
  const transferred = await tabState(page, tab.id);
  expect(state.sessions[0]?.resizes.at(-1)).toEqual({
    cols: transferred.cols,
    rows: transferred.rows,
  });

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

  await page.locator(".term-window-close").click();
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("Close all terminals?");
  await expect(dialog).toContainText("prod-01 · one");
  await expect(dialog).toContainText("prod-01 · two");
  await expectDialogActionGap(page);
  await page.getByRole("button", { name: "Keep terminals" }).click();
  await expect(page.locator(".term-tab")).toHaveCount(2);
  expect(state.deletes).toBe(0);
  expect(state.sessions.every((item) => item.closeFrames === 0)).toBe(true);

  const closeButton = page
    .locator(".term-tab", { hasText: "prod-01 · one" })
    .locator(".term-tab-close");
  await expect(closeButton).toHaveAttribute("aria-hidden", "true");
  const closeBox = await closeButton.boundingBox();
  expect(closeBox?.width ?? 0).toBeGreaterThanOrEqual(24);
  expect(closeBox?.height ?? 0).toBeGreaterThanOrEqual(24);
  await closeButton.click();
  await expect(dialog).toContainText("Close this terminal?");
  await expectDialogActionGap(page);
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();
  await expect(page.locator(".term-tab")).toHaveCount(1);
  await expect(page.locator(".term-tab")).toContainText("prod-01 · two");
  expect(state.deletes).toBe(1);
  expect(state.sessions[0]?.closeFrames).toBe(1);
  expect(state.sessions[1]?.closeFrames).toBe(0);

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

  await page.locator(".term-tab-name").first().focus();
  await page.keyboard.press("Delete");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Keep terminals" }).click();
  await expect(page.locator(".term-tab")).toHaveCount(1);

  await page.locator(".term-window-close").click();
  await dialog.getByRole("button", { name: "Close terminal", exact: true }).click();
  await expect(page.locator(".term-window")).toBeHidden();
  await expect(page.locator(".rail-btn--terminal")).toHaveCount(0);
  expect(state.deletes).toBe(2);
  expect(state.sessions[1]?.closeFrames).toBe(1);
});

test("window actions stay pinned and tabs keep their own scrollable strip", async ({
  page,
}) => {
  const state = freshState();
  await mockTerminalApi(page, state);
  await page.setViewportSize({ width: 1366, height: 900 });
  await seedApp(page, "/dashboard");

  const titles = [
    "one", "two", "three", "four", "five",
    "six", "seven", "eight", "nine", "ten",
  ];
  for (const cwd of titles) {
    await openWindow(page, cwd);
  }
  await expect(page.locator(".term-tab")).toHaveCount(titles.length);

  const context = page.locator(
    ".term-window .term-surface:visible .term-context",
  );
  await expect(context).toHaveAccessibleName(/host/);
  await expect(context).not.toHaveAttribute("aria-label", /.+/);

  for (const width of [1366, 1280, 480, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect.poll(async () => (await windowBox(page)).width).toBeLessThanOrEqual(width);

    const geometry = await page.evaluate(() => {
      const box = (element: Element) => {
        const rect = element.getBoundingClientRect();
        return { left: rect.left, right: rect.right, top: rect.top, bottom: rect.bottom };
      };
      const window_ = document.querySelector(".term-window") as HTMLElement;
      const bar = document.querySelector(".term-window-bar") as HTMLElement;
      const strip = document.querySelector(".term-tabstrip") as HTMLElement;
      const tabs = document.querySelector(".term-tabs") as HTMLElement;
      const add = document.querySelector(".term-add-tab") as HTMLElement;
      const min = document.querySelector(".term-window-min") as HTMLElement;
      const close = document.querySelector(".term-window-close") as HTMLElement;
      const addBox = box(add);
      const minBox = box(min);
      const closeBox = box(close);
      const windowBox = box(window_);
      return {
        bar: box(bar),
        strip: box(strip),
        window: windowBox,
        paddingRight: Number.parseFloat(getComputedStyle(bar).paddingRight),
        tabsInBar: bar.contains(tabs),
        tabs: box(tabs),
        scroll: {
          width: tabs.scrollWidth,
          client: tabs.clientWidth,
        },
        tabRects: [...document.querySelectorAll(".term-tab")].map(box),
        add: addBox,
        min: minBox,
        close: closeBox,
        addHit: document
          .elementsFromPoint((addBox.left + addBox.right) / 2, (addBox.top + addBox.bottom) / 2)
          .some((element) => element === add || add.contains(element)),
        minHit: document
          .elementsFromPoint((minBox.left + minBox.right) / 2, (minBox.top + minBox.bottom) / 2)
          .some((element) => element === min || min.contains(element)),
        closeHit: document
          .elementsFromPoint((closeBox.left + closeBox.right) / 2, (closeBox.top + closeBox.bottom) / 2)
          .some((element) => element === close || close.contains(element)),
      };
    });

    expect(geometry.tabsInBar).toBe(false);
    expect(geometry.strip.top).toBeGreaterThanOrEqual(geometry.bar.bottom - 0.5);
    expect(geometry.strip.right).toBeLessThanOrEqual(geometry.window.right + 0.5);
    expect(
      Math.abs(geometry.close.right - (geometry.bar.right - geometry.paddingRight)),
    ).toBeLessThanOrEqual(1);
    expect(geometry.min.right).toBeLessThanOrEqual(geometry.close.left);
    expect(geometry.tabs.right).toBeLessThanOrEqual(geometry.add.left + 0.5);
    expect(geometry.add.right).toBeLessThanOrEqual(geometry.strip.right + 0.5);
    expect(geometry.addHit).toBe(true);
    expect(geometry.minHit).toBe(true);
    expect(geometry.closeHit).toBe(true);

    if (width <= 480) {
      expect(geometry.scroll.width).toBeGreaterThan(geometry.scroll.client);
    } else {
      for (const tab of geometry.tabRects) {
        expect(tab.right).toBeLessThanOrEqual(geometry.add.left + 0.5);
      }
    }
  }

  await page.setViewportSize({ width: 390, height: 900 });
  await page.evaluate(() => {
    const tabs = document.querySelector(".term-tabs") as HTMLElement;
    tabs.scrollLeft = tabs.scrollWidth;
  });
  const scrolled = await page.evaluate(() => {
    const tabs = document.querySelector(".term-tabs") as HTMLElement;
    const add = document.querySelector(".term-add-tab") as HTMLElement;
    const last = [...document.querySelectorAll(".term-tab")].at(-1) as HTMLElement;
    return {
      tabsRight: tabs.getBoundingClientRect().right,
      lastRight: last.getBoundingClientRect().right,
      addLeft: add.getBoundingClientRect().left,
    };
  });
  expect(scrolled.lastRight).toBeLessThanOrEqual(scrolled.tabsRight + 0.5);
  expect(scrolled.tabsRight).toBeLessThanOrEqual(scrolled.addLeft + 0.5);

  await page.locator('.term-tab-name[aria-selected="true"]').focus();
  await page.keyboard.press("Tab");
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.className ?? ""))
    .toContain("term-add-tab");
  await page.keyboard.press("Shift+Tab");
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.getAttribute("role") ?? ""))
    .toBe("tab");
  await page.locator(".term-window-min").focus();
  await page.keyboard.press("Tab");
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.className ?? ""))
    .toContain("term-window-close");
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

  const movedBar = await page.locator(".term-window-bar").boundingBox();
  await page.mouse.move(movedBar!.x + 24, movedBar!.y + 20);
  await page.mouse.down();
  await page.mouse.move(2000, 2000, { steps: 6 });
  await page.mouse.up();
  await expectInsideViewport(page, { width: 1280, height: 800 });

  const grip = page.locator(".term-resize--se");
  await grip.focus();
  const beforeResize = await windowBox(page);
  await page.keyboard.press("Shift+ArrowLeft");
  await page.keyboard.press("Shift+ArrowUp");
  const afterResize = await windowBox(page);
  expect(afterResize.width).toBeLessThan(beforeResize.width);
  expect(afterResize.height).toBeLessThan(beforeResize.height);
  await expectInsideViewport(page, { width: 1280, height: 800 });

  await page.setViewportSize({ width: 1366, height: 800 });
  await expectInsideViewport(page, { width: 1366, height: 800 });

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
  await expect(page.locator(".ed-window")).toBeVisible();
  await page.locator(".term-window-min").click();
  await expect(page.locator(".rail-btn--terminal")).toBeVisible();
  await expect(page.locator(".rail-btn--editor")).toBeVisible();
  await page.locator(".rail-btn--terminal").click();
  await expect(terminal).toBeVisible();

  await page.locator(".ed-window-bar").click({ position: { x: 200, y: 12 } });
  const lowered = await terminal.evaluate((element) =>
    Number.parseInt(getComputedStyle(element as HTMLElement).zIndex, 10),
  );
  await page.locator(".rail-btn--terminal").click();
  const raised = await terminal.evaluate((element) =>
    Number.parseInt(getComputedStyle(element as HTMLElement).zIndex, 10),
  );
  expect(raised).toBeGreaterThan(lowered);
  await expect(terminal).toBeVisible();

  await expect(page.locator(".cm-content")).toBeVisible();
  await page.locator(".ed-window-min").click();
  await expect(page.locator(".ed-window")).toBeHidden();
  await expect(terminal).toBeVisible();
  expect(state.creates).toBe(1);
});

test("both floating tools render through one window frame and elevation", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1440, height: 900 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, `/servers/${serverId}/files`);

  await page.locator(".fm-label", { hasText: "app.yaml" }).first().dblclick();
  await expect(page.locator(".cm-content")).toBeVisible({ timeout: 15_000 });
  await openWindow(page, "app");
  await expect(page.locator(".term-window")).toBeVisible();

  const chrome = await page.evaluate(() => {
    const shadow = (element: Element) => getComputedStyle(element).boxShadow;
    const resolve = (name: string) => {
      const probe = document.createElement("div");
      probe.style.boxShadow = getComputedStyle(document.documentElement)
        .getPropertyValue(name)
        .trim();
      document.body.append(probe);
      const value = getComputedStyle(probe).boxShadow;
      probe.remove();
      return value;
    };
    const size = (selector: string) => {
      const rect = document.querySelector(selector)?.getBoundingClientRect();
      return { width: rect?.width ?? 0, height: rect?.height ?? 0 };
    };
    const style = (selector: string, property: string) =>
      getComputedStyle(document.querySelector(selector) as Element).getPropertyValue(property);
    return {
      floating: resolve("--elev-floating"),
      editor: {
        shadow: shadow(document.querySelector(".ed-window") as Element),
        radius: style(".ed-window", "border-top-left-radius"),
        border: style(".ed-window", "border-top-width"),
        barHeight:
          document.querySelector(".ed-window-bar")?.getBoundingClientRect().height ?? 0,
        min: size(".ed-window-min"),
        close: size(".ed-window-close"),
        sharedBar: document.querySelector(".ed-window-bar")?.classList.contains("floating-window-bar"),
      },
      terminal: {
        shadow: shadow(document.querySelector(".term-window") as Element),
        radius: style(".term-window", "border-top-left-radius"),
        border: style(".term-window", "border-top-width"),
        barHeight:
          document.querySelector(".term-window-bar")?.getBoundingClientRect().height ?? 0,
        min: size(".term-window-min"),
        close: size(".term-window-close"),
        sharedBar: document.querySelector(".term-window-bar")?.classList.contains("floating-window-bar"),
      },
    };
  });

  expect(chrome.floating).not.toBe("none");
  expect(chrome.editor.shadow).toBe(chrome.floating);
  expect(chrome.terminal.shadow).toBe(chrome.floating);
  expect(chrome.editor.radius).toBe(chrome.terminal.radius);
  expect(chrome.editor.border).toBe(chrome.terminal.border);
  expect(Math.abs(chrome.editor.barHeight - chrome.terminal.barHeight)).toBeLessThanOrEqual(0.5);
  expect(chrome.editor.sharedBar).toBe(true);
  expect(chrome.terminal.sharedBar).toBe(true);
  expect(Math.abs(chrome.editor.min.width - chrome.terminal.min.width)).toBeLessThanOrEqual(0.5);
  expect(Math.abs(chrome.editor.min.height - chrome.terminal.min.height)).toBeLessThanOrEqual(0.5);
  expect(Math.abs(chrome.editor.close.width - chrome.terminal.close.width)).toBeLessThanOrEqual(0.5);
  expect(Math.abs(chrome.editor.close.height - chrome.terminal.close.height)).toBeLessThanOrEqual(0.5);
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

  await page.clock.fastForward(25_500);
  await expect.poll(() => first.heartbeats).toBeGreaterThanOrEqual(1);

  await first.socket!.close({ code: 1006 });
  await expect(status).toHaveText("Disconnected");
  await page.clock.fastForward(5_000);
  expect(state.creates).toBe(1);

  await page.getByRole("button", { name: "Start new session" }).click();
  await expect.poll(() => state.creates).toBe(2);
  await expect(status).toHaveText("Connected");

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
  const shortcutsBox = await page.locator(".term-shortcuts-btn").boundingBox();
  expect(shortcutsBox?.width ?? 0).toBeGreaterThanOrEqual(24);
  expect(shortcutsBox?.height ?? 0).toBeGreaterThanOrEqual(24);
  await page.locator(".term-shortcuts-btn").click();
  await expect(shortcuts).toBeHidden();
  await page.locator(".term-commands-btn").hover();
  await expect(
    page.locator(".n-tooltip", { hasText: "Quick commands" }),
  ).toBeVisible();
  const input = page.locator(
    ".term-window .term-surface:visible .xterm-helper-textarea",
  );
  await input.focus();

  await page.keyboard.press("Control+Shift+E");
  await expect
    .poll(() => page.evaluate(() => document.activeElement?.id ?? ""))
    .toBe(`term-tab-${tab.id}`);

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

  await page.locator(`#term-tab-${tab.id}`).focus();
  await page.keyboard.press("Alt+Shift+W");
  const dialog = page.locator(".term-close-modal");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Keep terminals" }).click();
  await expect(page.locator(".term-tab")).toHaveCount(1);

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

  const tab = (await storeCall(page, "openPageSession", {
    serverId,
    serverName: "prod-01",
    root: "volumes",
    cwd: "app",
  })) as { id: string; sessionId: string };
  await expect.poll(() => state.sessions[0]?.socket !== null).toBe(true);
  await storeCall(page, "setTheme", "gotham-light");

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

  await storeCall(page, "detachToWindow", tab.id);
  const surface = page.locator(".term-window .term-surface:visible");
  await expect(page.locator(".term-window")).toBeVisible();
  await expect(surface).toHaveAttribute("data-theme", "gotham-light");
  await expect
    .poll(() =>
      surface.evaluate((element) =>
        getComputedStyle(element as HTMLElement).backgroundColor,
      ),
    )
    .toBe("rgb(247, 248, 250)");
  await expect.poll(() => terminalText(page)).toContain("welcome session-1");
  expect(state.creates).toBe(1);

  await surface.locator(".term-theme-btn").click();
  const group = page.getByRole("radiogroup", { name: "Terminal theme" });
  await expect(group).toBeVisible();
  await expect(group.getByRole("radio", { name: "Gotham Light" })).toBeChecked();
  await group.getByRole("radio", { name: "Gotham Dark" }).click();
  await expect(surface).toHaveAttribute("data-theme", "gotham-dark");
  await page.locator(".floating-window-title").click();
  await expect(page.getByRole("radiogroup", { name: "Terminal theme" })).toBeHidden();
  expect(state.creates).toBe(1);
  expect((await tabState(page, tab.id)).status).toBe("connected");
  await expect
    .poll(async () => countOf(await terminalText(page), "Gotham v0.2.1-dev"))
    .toBe(1);

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
  session.opcodes.length = 0;
  session.input = "";
  const surface = page.locator(".term-window .term-surface:visible");
  const trigger = surface.locator(".term-theme-btn");
  const group = page.getByRole("radiogroup", { name: "Terminal theme" });

  await trigger.focus();
  await page.keyboard.press("Enter");
  await expect(group).toBeVisible();
  await expect(group.getByRole("radio", { name: "Gotham Dark" })).toBeFocused();

  await page.keyboard.press("ArrowDown");
  await expect(surface).toHaveAttribute("data-theme", "gotham-light");
  await page.keyboard.press("ArrowUp");
  await expect(surface).toHaveAttribute("data-theme", "gotham-dark");
  await page.keyboard.press("Space");
  await page.keyboard.press("Enter");
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

  await page.keyboard.press("Space");
  await expect(group).toBeVisible();
  await page.keyboard.press("ArrowDown");
  await expect(surface).toHaveAttribute("data-theme", "gotham-light");
  await page.keyboard.press("Escape");
  await expect(group).toBeHidden();
  await expect(trigger).toBeFocused();

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

  const first = await openWindow(page, "one");
  await expect(page.locator(".term-window")).toBeVisible();
  await expect.poll(() => state.sessions[0]?.resizes.length ?? 0).toBeGreaterThan(0);
  await expect.poll(() => terminalText(page)).toContain("▓");
  const banner = await terminalText(page);
  expect(banner).toContain("Gotham v0.2.1-dev");
  expect(banner).toContain("server: prod-01");
  expect(countOf(banner, "Gotham v0.2.1-dev")).toBe(1);

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
  await page.locator(".floating-window-title").click();
  const firstTab = page.locator(".term-tab-name", { hasText: "prod-01 · one" });
  await firstTab.click();
  await expect
    .poll(async () => countOf(await terminalText(page), "Gotham v0.2.1-dev"))
    .toBe(1);
  expect((await tabState(page, first.id)).status).toBe("connected");
  expect(state.creates).toBe(2);

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

async function surfaceToText(page: Page): Promise<string> {
  return page
    .locator(".term-window .term-surface:visible .xterm-rows")
    .innerText();
}

test("an idle terminal reserves no suggestion row and a match never resizes the PTY", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");

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

  await openWindow(page, "app");
  const surface = page.locator(".term-window .term-surface:visible");
  await expect(surface.locator(".term-state")).toHaveText("Connected");

  await expect(surface.locator(".term-suggest")).toHaveCount(0);
  const idle = await surface.locator(".term-screen").boundingBox();
  const idleResizes = state.sessions[0]?.resizes.length ?? 0;

  state.sessions[0]?.socket?.send(outputFrame("\x1b]133;B\x07"));
  await surface.locator(".xterm-helper-textarea").focus();
  await page.keyboard.type("gs");
  await expect(surface.locator(".term-suggest-option")).toBeVisible();
  await expect
    .poll(async () => Math.round((await surface.locator(".term-screen").boundingBox())?.height ?? 0))
    .toBe(Math.round(idle?.height ?? 0));
  await expect
    .poll(async () => {
      const strip = await surface.locator(".term-suggest").boundingBox();
      const screen = await surface.locator(".term-screen").boundingBox();
      const cursor = await surface.locator(".xterm-cursor").boundingBox();
      if (!strip || !screen || !cursor) {
        return "missing";
      }
      if (strip.y < screen.y - 1) {
        return "above box";
      }
      if (strip.y + strip.height > screen.y + screen.height + 1) {
        return "below box";
      }
      const clear =
        strip.y + strip.height <= cursor.y + 1 ||
        strip.y >= cursor.y + cursor.height - 1;
      return clear ? "ok" : "covers cursor";
    })
    .toBe("ok");
  const under = await page.evaluate(() => {
    const element = document.querySelector(
      ".term-window .term-suggest",
    );
    if (!(element instanceof HTMLElement)) {
      return null;
    }
    const rect = element.getBoundingClientRect();
    const x = rect.left + 8;
    const y = rect.top + rect.height / 2;
    return document
      .elementsFromPoint(x, y)
      .some((node) => node === element || element.contains(node));
  });
  expect(under).toBe(false);

  await surface.getByRole("button", { name: "Dismiss suggestion" }).click();
  await expect(surface.locator(".term-suggest")).toHaveCount(0);
  await expect
    .poll(async () => Math.round((await surface.locator(".term-screen").boundingBox())?.height ?? 0))
    .toBe(Math.round(idle?.height ?? 0));
  expect(state.sessions[0]?.resizes.length ?? 0).toBe(idleResizes);
});

test("a suggestion never covers a prompt that sits on the last row", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");
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

  const tab = await openWindow(page, "app");
  const surface = page.locator(".term-window .term-surface:visible");
  await expect(surface.locator(".term-state")).toHaveText("Connected");

  const rows = (await tabState(page, tab.id)).rows;
  const filler = Array.from({ length: rows + 8 }, (_, index) => `line ${index}`)
    .join("\r\n");
  state.sessions[0]?.socket?.send(outputFrame(`${filler}\r\n`));
  state.sessions[0]?.socket?.send(outputFrame("\x1b]133;B\x07"));
  await expect
    .poll(async () => (await surfaceToText(page)).includes(`line ${rows + 7}`))
    .toBe(true);

  const resizesBefore = state.sessions[0]?.resizes.length ?? 0;
  const screenBefore = await surface.locator(".term-screen").boundingBox();
  const input = surface.locator(".xterm-helper-textarea");
  await input.focus();
  await page.keyboard.type("gs");
  await expect(surface.locator(".term-suggest-option")).toBeVisible();

  const geometry = await page.evaluate(() => {
    const rect = (selector: string) => {
      const box = document
        .querySelector(selector)
        ?.getBoundingClientRect();
      return box ? { x: box.x, y: box.y, width: box.width, height: box.height } : null;
    };
    return {
      screen: rect(".term-window .term-screen"),
      cursor: rect(".term-window .xterm-cursor"),
      strip: rect(".term-window .term-suggest"),
      status: rect(".term-window .term-statusbar"),
      statusHit: (() => {
        const bar = document.querySelector(
          ".term-window .term-statusbar",
        );
        const box = bar?.getBoundingClientRect();
        if (!bar || !box) {
          return false;
        }
        return document
          .elementsFromPoint(box.x + box.width / 2, box.y + box.height / 2)
          .some((node) => node === bar || bar.contains(node));
      })(),
    };
  });
  expect(geometry.screen).not.toBeNull();
  expect(geometry.cursor).not.toBeNull();
  expect(geometry.strip).not.toBeNull();

  expect(
    geometry.cursor!.y + geometry.cursor!.height,
  ).toBeGreaterThanOrEqual(
    geometry.screen!.y + geometry.screen!.height - 16,
  );

  await expect
    .poll(async () => Math.round((await surface.locator(".term-screen").boundingBox())?.height ?? 0))
    .toBe(Math.round(screenBefore?.height ?? 0));
  expect(state.sessions[0]?.resizes.length ?? 0).toBe(resizesBefore);

  await expect
    .poll(async () => {
      const strip = await surface.locator(".term-suggest").boundingBox();
      const cursor = await surface.locator(".xterm-cursor").boundingBox();
      const screen = await surface.locator(".term-screen").boundingBox();
      if (!strip || !cursor || !screen) {
        return "missing";
      }
      if (strip.y < screen.y - 1) {
        return "above box";
      }
      return strip.y + strip.height <= cursor.y + 1 ? "clear" : "covers";
    })
    .toBe("clear");

  expect(geometry.status).not.toBeNull();
  expect(geometry.statusHit).toBe(true);
  const passThrough = await page.evaluate(() => {
    const strip = document.querySelector(
      ".term-window .term-suggest",
    );
    const box = strip?.getBoundingClientRect();
    if (!strip || !box) {
      return null;
    }
    return document
      .elementsFromPoint(box.x + 8, box.y + box.height / 2)
      .some((node) => node === strip || strip.contains(node));
  });
  expect(passThrough).toBe(false);

  state.sessions[0]?.socket?.close({ code: 1006 });
  const overlay = surface.locator(".term-overlay");
  await expect(overlay).toBeVisible();
  const retryBox = await overlay.getByRole("button").boundingBox();
  expect(retryBox).not.toBeNull();
  const retryHit = await page.evaluate(
    (point) =>
      document
        .elementsFromPoint(point.x, point.y)
        .some((node) => node.closest(".term-overlay") !== null),
    {
      x: retryBox!.x + retryBox!.width / 2,
      y: retryBox!.y + retryBox!.height / 2,
    },
  );
  expect(retryHit).toBe(true);
});

test("quick command suggestions survive a theme change and page → window move", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");

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

  await page.locator(".term-commands-btn").click();
  await expect(page.getByText("Quick commands", { exact: true })).toBeVisible();

  await expect
    .poll(() =>
      page.evaluate(
        () =>
          document
            .querySelector(".term-quick-commands")
            ?.className.includes("enter-active") ?? true,
      ),
    )
    .toBe(false);
  for (const width of [390, 480]) {
    await page.setViewportSize({ width, height: 800 });
    await expect
      .poll(async () => {
        const rect = await page.locator(".term-quick-commands-body").boundingBox();
        if (!rect) {
          return "missing";
        }
        return rect.x >= 0 && rect.x + rect.width <= width + 1
          ? "inside"
          : `outside:${Math.round(rect.x)}:${Math.round(rect.width)}`;
      })
      .toBe("inside");
  }
  await page.setViewportSize({ width: 1280, height: 800 });

  await page.getByRole("button", { name: "Add command" }).click();
  await expect(page.getByText("Abbreviation (optional)")).toBeVisible();
  await expect(
    page.getByText(/Type this at a supported prompt/),
  ).toBeVisible();
  await page.locator(".term-quick-commands-form input").first().fill("Git status");
  await page.locator(".term-quick-commands-form textarea").fill("git status");
  await page.locator(".term-quick-commands-form input").nth(1).fill("gs");
  await page.getByRole("button", { name: "Save command" }).click();
  await expect(page.locator(".term-quick-commands-item")).toHaveCount(1);
  await expect(page.locator(".term-quick-commands-item")).toContainText("git status");

  await page.locator(".term-quick-commands-insert").click();
  await expect.poll(() => state.sessions[0]?.input ?? "").toContain("git status");
  expect(state.sessions[0]?.input ?? "").not.toContain("\r");
  const afterInsert = state.sessions[0]?.input.length ?? 0;
  await page.waitForTimeout(50);
  expect(state.sessions[0]?.input.length).toBe(afterInsert);

  state.sessions[0]?.socket?.send(outputFrame("\x1b]133;B\x07"));
  const input = page.locator(
    ".term-window .term-surface:visible .xterm-helper-textarea",
  );
  await input.focus();
  await page.keyboard.type("gs");
  const suggestion = page.locator(".term-window .term-suggest-option");
  await expect(suggestion).toBeVisible();
  await expect(suggestion).toContainText("Git status");
  const live = page.locator(".term-window .term-suggest .term-sr");
  await expect(live).toHaveAttribute("role", "status");
  await expect(live).toHaveAttribute("aria-live", "polite");
  const liveBox = await live.boundingBox();
  expect(liveBox?.width ?? 0).toBeLessThanOrEqual(1);
  expect(liveBox?.height ?? 0).toBeLessThanOrEqual(1);

  for (const width of [390, 480, 1280, 1366]) {
    await page.setViewportSize({ width, height: 800 });
    await expect(suggestion).toBeVisible();
    await expect
      .poll(async () => {
        const nextBox = await page
          .locator(".term-window .term-suggest")
          .boundingBox();
        const nextScreen = await page
          .locator(".term-window .term-surface:visible .term-screen")
          .boundingBox();
        const nextCursor = await page
          .locator(".term-window .term-surface:visible .xterm-cursor")
          .boundingBox();
        if (!nextBox || !nextScreen || !nextCursor) {
          return false;
        }
        const clearOfCursor =
          nextBox.y + nextBox.height <= nextCursor.y + 1 ||
          nextBox.y >= nextCursor.y + nextCursor.height - 1;
        return (
          nextBox.x >= 0 &&
          nextBox.x + nextBox.width <= width + 1 &&
          nextBox.y >= nextScreen.y - 1 &&
          nextBox.y + nextBox.height <= nextScreen.y + nextScreen.height + 1 &&
          clearOfCursor
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
  expect(screenShown?.height).toBe(screenBefore?.height);
  expect(state.sessions[0]?.resizes.length ?? 0).toBe(resizesBefore);

  const beforeEscape = state.sessions[0]?.input ?? "";
  await page.keyboard.press("Escape");
  await expect(suggestion).toBeHidden();
  await expect
    .poll(() => state.sessions[0]?.input ?? "")
    .toBe(`${beforeEscape}\x1b`);
  expect(state.sessions[0]?.resizes.length ?? 0).toBe(resizesBefore);
  await expect
    .poll(async () =>
      Math.round(
        (await page
          .locator(".term-window .term-surface:visible .term-screen")
          .boundingBox())?.height ?? 0,
      ),
    )
    .toBe(Math.round(screenBefore?.height ?? 0));

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
  await expect(page.locator(".term-quick-commands-item")).toHaveCount(1);
  await expect(page.locator(".term-quick-commands-item")).toContainText("git status");
  expect(tab.id).toBeTruthy();
});

test("the terminal window floats on the soft elevation shadow in both themes", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state = freshState();
  await mockTerminalApi(page, state);
  await seedApp(page, "/dashboard");
  await openWindow(page, "app");

  const terminalWindow = page.locator(".term-window");
  await expect(terminalWindow).toBeVisible();

  const dark = await floatingShadow(page, ".term-window");
  expect(dark.expected).not.toBe("none");
  expect(dark.actual).toBe(dark.expected);
  expect(dark.actual).not.toBe(dark.raised);

  const surface = page.locator(".term-window .term-surface:visible");
  const darkBox = await windowBox(page);
  await storeCall(page, "setTheme", "gotham-light");
  await expect(surface).toHaveAttribute("data-theme", "gotham-light");
  await expect
    .poll(() =>
      surface.evaluate((element) =>
        getComputedStyle(element as HTMLElement).backgroundColor,
      ),
    )
    .toBe("rgb(247, 248, 250)");
  const light = await floatingShadow(page, ".term-window");
  expect(light.actual).toBe(light.expected);
  const lightBox = await windowBox(page);
  expect(Math.round(lightBox.x)).toBe(Math.round(darkBox.x));
  expect(Math.round(lightBox.y)).toBe(Math.round(darkBox.y));
  expect(Math.round(lightBox.width)).toBe(Math.round(darkBox.width));
  expect(Math.round(lightBox.height)).toBe(Math.round(darkBox.height));

  await page.setViewportSize({ width: 390, height: 844 });
  const phone = await floatingShadow(page, ".term-window");
  expect(phone.actual).toBe(phone.expected);
  await expectInsideViewport(page, { width: 390, height: 844 });
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow).toBeLessThanOrEqual(0);
});
