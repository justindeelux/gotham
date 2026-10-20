import { Buffer } from "node:buffer";
import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page, WebSocketRoute } from "@playwright/test";

/* global document:readonly, URL:readonly, window:readonly */
// URL/window run inside the page (browser globals); the repo lint envs do not
// cover tests/, so they are declared.

// Real-browser proof for JUS-248: "Open terminal here" in the File manager
// opens a WINDOW-mode host session in the intended directory through the
// toolbar, the empty-space menu and a directory row menu. The committed
// webdist is served statically, the control-plane file + terminal APIs are
// mocked faithfully (create/delete recorded, the PTY socket answers with the
// real binary framing), and every assertion checks the exact create-session
// payload, target and session identity — not just a visible label.

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

const user = {
  id: "u-1",
  email: "ada@gotham.dev",
  created_at: "2026-03-04T12:00:00Z",
  display_name: "Ada",
  has_password: true,
  is_platform_admin: true,
};

const team = {
  id: "team-1",
  name: "Acme",
  is_personal: false,
  role: "owner",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

const serverId = "44444444-4444-4444-8444-444444444444";

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

/** oddName is a legal directory name with every quoting hazard. */
const oddName = `odd dir; touch pwned $(id) \`whoami\` ' " ünïcode & |`;

/** listing is the mixed folder the browser renders for every path. */
function listing() {
  return [
    {
      name: "app.yaml",
      kind: "file",
      size: 2458,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"a"',
    },
    {
      name: "config",
      kind: "directory",
      size: 0,
      mode: 0o040755,
      modified_at: "2026-10-12T09:14:00Z",
      etag: '"b"',
    },
    {
      name: "data",
      kind: "directory",
      size: 0,
      mode: 0o040755,
      modified_at: "2026-10-12T08:51:00Z",
      etag: '"c"',
    },
    {
      name: oddName,
      kind: "directory",
      size: 0,
      mode: 0o040755,
      modified_at: "2026-10-12T08:52:00Z",
      etag: '"d"',
    },
    {
      name: "run.sock",
      kind: "special",
      size: 0,
      mode: 0o140777,
      modified_at: "2026-10-12T09:19:00Z",
      etag: '"e"',
    },
  ];
}

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
  body: CreateBody;
  socket: WebSocketRoute | null;
}

/** State is the per-test mock ledger. */
interface State {
  creates: CreateBody[];
  sessions: FakeSession[];
  deletes: number;
  seq: number;
  /** createError answers the create call with an HTTP failure instead. */
  createError: { status: number; message: string } | null;
  /** streamError ends the PTY stream with this bounded error-frame phrase. */
  streamError: string | null;
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

/** errorFrame sends the bounded phrase the relay uses for a failed cwd. */
function errorFrame(message: string): Buffer {
  return frame(3, Buffer.from(message, "utf8"));
}

/** state builds an empty ledger. */
function newState(): State {
  return {
    creates: [],
    sessions: [],
    deletes: 0,
    seq: 0,
    createError: null,
    streamError: null,
  };
}

/** mockApi wires the file manager and terminal API for one page. */
async function mockApi(page: Page, state: State): Promise<void> {
  const session = {
    user,
    accessToken: "test-access",
    refreshToken: "test-refresh",
  };
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));

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
  await page.route(`**/api/v1/servers/${serverId}/files/roots`, async (route) => {
    await route.fulfill({ json: { roots: [root] } });
  });
  await page.route(`**/api/v1/servers/${serverId}/files/list*`, async (route) => {
    await route.fulfill({
      json: { entries: listing(), truncated: false, cursor: "" },
    });
  });

  await page.route(
    `**/api/v1/servers/${serverId}/terminal/sessions**`,
    async (route) => {
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
      const body = request.postDataJSON() as CreateBody;
      state.creates.push(body);
      if (state.createError !== null) {
        await route.fulfill({
          status: state.createError.status,
          json: { message: state.createError.message },
        });
        return;
      }
      state.seq += 1;
      const id = `session-${state.seq}`;
      state.sessions.push({ id, body, socket: null });
      await route.fulfill({
        status: 201,
        json: {
          id,
          server_id: serverId,
          target: "host",
          root: body.root ?? "",
          cwd: body.cwd ?? "",
          cols: body.cols ?? 120,
          rows: body.rows ?? 32,
          expires_at: "2026-10-20T12:01:00Z",
          websocket_path: `/api/v1/servers/${serverId}/terminal/sessions/${id}/ws`,
          subprotocol: "gotham.terminal.v1",
        },
      });
    },
  );

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
      if (state.streamError !== null) {
        ws.send(errorFrame(state.streamError));
        return;
      }
      ws.send(openFrame());
      ws.onMessage(() => {
        // Input is irrelevant to this proof; keep the socket live.
      });
    },
  );
}

/** openFiles seeds the session and lands on the file browser. */
async function openFiles(
  page: Page,
  state: State,
  options: { width?: number } = {},
): Promise<void> {
  await page.setViewportSize({ width: options.width ?? 1440, height: 900 });
  await mockApi(page, state);
  await page.goto(`${baseURL}/servers/${serverId}/files`);
  await expect(page.locator(".fm-label b").first()).toBeVisible();
}

/** rowFor returns the browser row whose entry name is exactly `name`. */
function rowFor(page: Page, name: string) {
  return page
    .locator(".fm-table .fm-row")
    .filter({ has: page.getByText(name, { exact: true }) });
}

/** openDirectory double-clicks a folder row and waits for the new path. */
async function openDirectory(page: Page, name: string): Promise<void> {
  await rowFor(page, name).dblclick();
  await expect(page.locator(".fm-foot-path")).toContainText(`volumes/${name}`);
}

/** terminalWindow is the floating window region. */
function terminalWindow(page: Page) {
  return page.getByRole("region", { name: "Terminal window" });
}

// Every test that opens a shell leaves a live PTY behind. Close them through
// the app's own confirmation path so the page never holds a beforeunload guard
// (and an open WebSocket) when Playwright tears the context down.
test.afterEach(async ({ page }) => {
  await page
    .evaluate(() => {
      const app = (
        document.querySelector("#app") as unknown as {
          __vue_app__?: {
            config: {
              globalProperties: { $pinia: { _s: Map<string, unknown> } };
            };
          };
        } | null
      )?.__vue_app__;
      const terminal = app?.config.globalProperties.$pinia._s.get("terminal") as
        | { closeAll: () => void }
        | undefined;
      terminal?.closeAll();
    })
    .catch(() => {
      // The page may already be gone after a failure; nothing to clean up.
    });
});

test("toolbar opens a window session in the folder on screen", async ({ page }) => {
  const state = newState();
  await openFiles(page, state);

  await openDirectory(page, "data");
  await page.getByRole("button", { name: "Open terminal here" }).click();

  await expect.poll(() => state.creates.length).toBe(1);
  // Exactly the create contract: the relative path is data, never a command.
  expect(state.creates[0]).toEqual({
    target: "host",
    root: "volumes",
    cwd: "data",
    cols: 120,
    rows: 32,
  });
  expect(state.creates[0]).not.toHaveProperty("command");
  expect(state.sessions).toHaveLength(1);
  expect(state.sessions[0]).toMatchObject({
    id: "session-1",
    body: { cwd: "data" },
  });

  await expect(terminalWindow(page)).toBeVisible();
  await expect(page.getByRole("tab", { name: "prod-01 · data" })).toBeVisible();
  await expect(terminalWindow(page).getByText("Connected")).toBeVisible();
});

test("the empty-space menu targets the current directory", async ({ page }) => {
  const state = newState();
  await openFiles(page, state);
  await openDirectory(page, "data");

  const wrap = page.locator(".table-wrap");
  const box = await wrap.boundingBox();
  expect(box).not.toBeNull();
  await wrap.click({
    button: "right",
    position: { x: box!.width / 2, y: box!.height - 24 },
  });
  const menu = page.getByRole("menu", { name: "Folder actions" });
  await expect(menu).toBeVisible();
  await menu.getByRole("menuitem", { name: "Open terminal here" }).click();

  await expect.poll(() => state.creates.length).toBe(1);
  expect(state.creates[0]).toEqual({
    target: "host",
    root: "volumes",
    cwd: "data",
    cols: 120,
    rows: 32,
  });
  await expect(page.getByRole("tab", { name: "prod-01 · data" })).toBeVisible();
});

test("a directory row opens its own folder; files and special rows never offer it", async ({
  page,
}) => {
  const state = newState();
  await openFiles(page, state);

  // A file has no shell action.
  await rowFor(page, "app.yaml").click({ button: "right" });
  const fileMenu = page.getByRole("menu", { name: "More actions for app.yaml" });
  await expect(fileMenu).toBeVisible();
  await expect(
    fileMenu.getByRole("menuitem", { name: /Open terminal/ }),
  ).toHaveCount(0);
  await page.keyboard.press("Escape");

  // A special entry (socket) has no actions at all.
  await rowFor(page, "run.sock").click({ button: "right" });
  await expect(page.getByRole("menu")).toHaveCount(0);

  // A real directory targets itself, not the folder on screen.
  await rowFor(page, "config").click({ button: "right" });
  const folderMenu = page.getByRole("menu", {
    name: "More actions for config",
  });
  await expect(folderMenu).toBeVisible();
  await folderMenu
    .getByRole("menuitem", { name: "Open terminal in config" })
    .click();

  await expect.poll(() => state.creates.length).toBe(1);
  expect(state.creates[0]).toMatchObject({ root: "volumes", cwd: "config" });
  await expect(page.getByRole("tab", { name: "prod-01 · config" })).toBeVisible();
});

test("keyboard-only menu opens a folder session", async ({ page }) => {
  const state = newState();
  await openFiles(page, state);

  await rowFor(page, "data").locator(".fm-label").focus();
  await page.keyboard.press("Shift+F10");
  const menu = page.getByRole("menu", { name: "More actions for data" });
  await expect(menu).toBeVisible();
  await menu.getByRole("menuitem", { name: "Open terminal in data" }).click();

  await expect.poll(() => state.creates.length).toBe(1);
  expect(state.creates[0]).toMatchObject({ cwd: "data" });
});

test("a pathological folder name travels verbatim, never as a command", async ({
  page,
}) => {
  const state = newState();
  await openFiles(page, state);

  await rowFor(page, oddName).click({ button: "right" });
  const menu = page.getByRole("menu", {
    name: `More actions for ${oddName}`,
  });
  await expect(menu).toBeVisible();
  await menu
    .getByRole("menuitem", { name: `Open terminal in ${oddName}` })
    .click();

  await expect.poll(() => state.creates.length).toBe(1);
  expect(state.creates[0]).toEqual({
    target: "host",
    root: "volumes",
    cwd: oddName,
    cols: 120,
    rows: 32,
  });
  const body = JSON.stringify(state.creates[0]);
  expect(body).not.toContain("cd ");
  expect(body).not.toContain("touch pwned;");
  // The whole body is the session contract: no `command` or shell text field.
  expect(Object.keys(state.creates[0]!).sort()).toEqual([
    "cols",
    "cwd",
    "root",
    "rows",
    "target",
  ]);
  expect(page.getByRole("tab", { name: `prod-01 · ${oddName}` })).toBeVisible();
});

test("an old session keeps its cwd when another directory opens", async ({
  page,
}) => {
  const state = newState();
  await openFiles(page, state);

  await rowFor(page, "config").click({ button: "right" });
  await page
    .getByRole("menu", { name: "More actions for config" })
    .getByRole("menuitem", { name: "Open terminal in config" })
    .click();
  await expect.poll(() => state.creates.length).toBe(1);

  // The floating window covers the list; minimize it like a user would.
  await page.getByRole("button", { name: "Minimize terminal window" }).click();
  await expect(terminalWindow(page)).toBeHidden();

  await rowFor(page, "data").click({ button: "right" });
  await page
    .getByRole("menu", { name: "More actions for data" })
    .getByRole("menuitem", { name: "Open terminal in data" })
    .click();
  await expect.poll(() => state.creates.length).toBe(2);

  // Two independent tabs, each labelled with its own directory and node.
  expect(state.creates.map((body) => body.cwd)).toEqual(["config", "data"]);
  await expect(page.getByRole("tab", { name: "prod-01 · config" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "prod-01 · data" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Terminal" })).toHaveCount(0);
});

test("restores a minimized terminal window for the new tab", async ({ page }) => {
  const state = newState();
  await openFiles(page, state);

  // Open one session, then really minimize the window through its control.
  await page.getByRole("button", { name: "Open terminal here" }).click();
  await expect.poll(() => state.creates.length).toBe(1);
  await expect(terminalWindow(page)).toBeVisible();
  await page.getByRole("button", { name: "Minimize terminal window" }).click();
  await expect(terminalWindow(page)).toBeHidden();

  // The next directory's action must restore it, not open a hidden tab.
  await openDirectory(page, "data");
  await page.getByRole("button", { name: "Open terminal here" }).click();
  await expect.poll(() => state.creates.length).toBe(2);
  expect(state.creates[1]).toEqual({
    target: "host",
    root: "volumes",
    cwd: "data",
    cols: 120,
    rows: 32,
  });
  await expect(terminalWindow(page)).toBeVisible();
  await expect(page.getByRole("tab", { name: "prod-01 · data" })).toBeVisible();
  // The first session was not retargeted: it keeps the root cwd.
  expect(state.creates[0]).toEqual({
    target: "host",
    root: "volumes",
    cols: 120,
    rows: 32,
  });
});

test("non-operators never see the action (file read access is not a shell)", async ({
  page,
}) => {
  const state = newState();
  await openFiles(page, state);

  // The file manager route itself is operator-gated. Simulate a live demotion
  // (the account is read-only for files now) and prove the shell action reacts
  // to the operator capability, not to the file role.
  await page.evaluate(() => {
    const app = (
      document.querySelector("#app") as unknown as {
        __vue_app__: {
          config: { globalProperties: { $pinia: { _s: Map<string, unknown> } } };
        };
      }
    ).__vue_app__;
    const auth = app.config.globalProperties.$pinia._s.get("auth") as {
      user: Record<string, unknown> | null;
    };
    auth.user = { ...(auth.user ?? {}), is_platform_admin: false };
  });

  await expect(
    page.getByRole("button", { name: "Open terminal here" }),
  ).toHaveCount(0);

  await rowFor(page, "config").click({ button: "right" });
  const menu = page.getByRole("menu", { name: "More actions for config" });
  await expect(menu).toBeVisible();
  await expect(
    menu.getByRole("menuitem", { name: /Open terminal/ }),
  ).toHaveCount(0);
  await page.keyboard.press("Escape");

  const wrap = page.locator(".table-wrap");
  const box = await wrap.boundingBox();
  await wrap.click({
    button: "right",
    position: { x: box!.width / 2, y: box!.height - 24 },
  });
  const folderMenu = page.getByRole("menu", { name: "Folder actions" });
  await expect(folderMenu).toBeVisible();
  await expect(
    folderMenu.getByRole("menuitem", { name: /Open terminal/ }),
  ).toHaveCount(0);
  expect(state.creates).toHaveLength(0);
});

test("surfaces a server-side permission denial", async ({ page }) => {
  const state = newState();
  state.createError = {
    status: 403,
    message: "platform operator required",
  };
  await openFiles(page, state);

  await page.getByRole("button", { name: "Open terminal here" }).click();

  await expect(terminalWindow(page)).toBeVisible();
  await expect(
    terminalWindow(page).getByText("Only a platform operator can open a host terminal."),
  ).toBeVisible();
  expect(state.sessions).toHaveLength(0);
});

test("surfaces a deleted folder from the PTY stream", async ({ page }) => {
  const state = newState();
  state.streamError = "the working directory no longer exists";
  await openFiles(page, state);

  await openDirectory(page, "data");
  await page.getByRole("button", { name: "Open terminal here" }).click();

  await expect.poll(() => state.creates.length).toBe(1);
  expect(state.creates[0]).toMatchObject({ cwd: "data" });
  await expect(
    terminalWindow(page).getByText("That folder no longer exists on the server."),
  ).toBeVisible();
});

test("surfaces an invalid working directory without a host path", async ({
  page,
}) => {
  const state = newState();
  state.streamError = "invalid working directory";
  await openFiles(page, state);

  await rowFor(page, oddName).click({ button: "right" });
  await page
    .getByRole("menu", { name: `More actions for ${oddName}` })
    .getByRole("menuitem", { name: `Open terminal in ${oddName}` })
    .click();

  await expect.poll(() => state.creates.length).toBe(1);
  expect(state.creates[0]).toMatchObject({ cwd: oddName });
  await expect(
    terminalWindow(page).getByText(
      "The working directory is not allowed. Open a folder inside the server's allowed root.",
    ),
  ).toBeVisible();
  // The bounded phrase is the only detail; no host path is leaked.
  await expect(terminalWindow(page)).not.toContainText("/var/lib/gotham");
});

test("surfaces an unknown node and an unsupported agent", async ({ page }) => {
  const state = newState();
  state.createError = { status: 404, message: "not found" };
  await openFiles(page, state);

  await page.getByRole("button", { name: "Open terminal here" }).click();
  await expect(
    terminalWindow(page).getByText(
      "The node was not found or is no longer managed.",
    ),
  ).toBeVisible();

  // A second attempt answers 501 from an old agent.
  state.createError = { status: 501, message: "unsupported" };
  await page.getByRole("button", { name: "Open terminal here" }).click();
  await expect(
    terminalWindow(page).getByText(
      "This node agent does not support terminal sessions.",
    ),
  ).toBeVisible();
  expect(state.sessions).toHaveLength(0);
});

test("the toolbar action stays reachable at phone and tablet widths", async ({
  page,
}) => {
  for (const width of [390, 480]) {
    await page.setViewportSize({ width, height: 780 });
    const state = newState();
    await mockApi(page, state);
    await page.goto(`${baseURL}/servers/${serverId}/files`);
    await expect(page.locator(".fm-label b").first()).toBeVisible();

    const button = page.getByRole("button", { name: "Open terminal here" });
    await expect(button).toBeVisible();
    await button.click();
    await expect.poll(() => state.creates.length).toBe(1);
    expect(state.creates[0]).toEqual({
      target: "host",
      root: "volumes",
      cols: 120,
      rows: 32,
    });
    await expect(terminalWindow(page)).toBeVisible();
  }
});
