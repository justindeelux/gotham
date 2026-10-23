import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect } from "@playwright/test";
import type {
  APIRequestContext,
  BrowserContext,
  Page,
  WebSocket,
} from "@playwright/test";

const e2eDir = dirname(fileURLToPath(import.meta.url));
const repoRoot = resolve(e2eDir, "../..");

const CLIENT_INPUT = 0x01;
const CLIENT_RESIZE = 0x02;
const CLIENT_CLOSE = 0x03;
const CLIENT_HEARTBEAT = 0x04;

export const TERMINAL_LOGO_SHA256 =
  "76a2f2e19ef0f58326c1f79c5ac837cc25df96f40b7a6d606d292f27b177a274";

export interface CreateRequest {
  method: string;
  url: string;
  body: Record<string, unknown> | null;
  status: number;
  sessionId: string;
}

export interface FrameLog {
  opcode: number;
  text: string;
  resize?: [number, number];
}

export interface SocketLog {
  url: string;
  sessionId: string;
  frames: FrameLog[];
  closed: boolean;
}

export interface TerminalTraffic {
  creates: CreateRequest[];
  deletes: string[];
  sockets: SocketLog[];
  currentSocket: () => SocketLog;
  inputFrames: () => number;
  createCount: () => number;
}

function sessionIdFromPath(url: string): string {
  const match = /\/terminal\/sessions\/([0-9a-f-]{36})(?:\/ws)?(?:\?|$)/.exec(
    url,
  );
  return match?.[1] ?? "";
}

export function watchTerminal(page: Page): TerminalTraffic {
  const creates: CreateRequest[] = [];
  const deletes: string[] = [];
  const sockets: SocketLog[] = [];

  page.on("response", (response) => {
    const request = response.request();
    const url = response.url();
    if (!/\/terminal\/sessions/.test(url) || /\/ws(?:\?|$)/.test(url)) {
      return;
    }
    if (request.method() === "DELETE") {
      deletes.push(url);
      return;
    }
    if (request.method() !== "POST") {
      return;
    }
    const entry: CreateRequest = {
      method: request.method(),
      url,
      body: (request.postDataJSON() as Record<string, unknown> | null) ?? null,
      status: response.status(),
      sessionId: "",
    };
    creates.push(entry);
    void response
      .json()
      .then((body: { id?: string }) => {
        entry.sessionId = body.id ?? "";
      })
      .catch(() => {
        entry.sessionId = "";
      });
  });

  page.on("websocket", (socket: WebSocket) => {
    if (!/\/terminal\/sessions/.test(socket.url())) {
      return;
    }
    const log: SocketLog = {
      url: socket.url(),
      sessionId: sessionIdFromPath(socket.url()),
      frames: [],
      closed: false,
    };
    sockets.push(log);
    socket.on("framesent", (payload) => {
      const bytes = toBytes(payload);
      if (bytes.length === 0) {
        return;
      }
      const opcode = bytes[0]!;
      if (opcode === CLIENT_INPUT) {
        log.frames.push({
          opcode,
          text: bytes.subarray(1).toString("utf8"),
        });
        return;
      }
      if (opcode === CLIENT_RESIZE) {
        log.frames.push({
          opcode,
          resize: [bytes.readUInt16BE(1), bytes.readUInt16BE(3)],
        });
        return;
      }
      if (opcode === CLIENT_CLOSE || opcode === CLIENT_HEARTBEAT) {
        log.frames.push({ opcode, text: "" });
      }
    });
    socket.on("close", () => {
      log.closed = true;
    });
  });

  return {
    creates,
    deletes,
    sockets,
    currentSocket: () => {
      const socket = sockets[sockets.length - 1];
      if (socket === undefined) {
        throw new Error("no terminal WebSocket was opened");
      }
      return socket;
    },
    inputFrames: () =>
      sockets.reduce(
        (total, socket) =>
          total + socket.frames.filter((frame) => frame.opcode === CLIENT_INPUT).length,
        0,
      ),
    createCount: () => creates.length,
  };
}

function toBytes(payload: unknown): Buffer {
  if (typeof payload === "string") {
    return Buffer.from(payload, "binary");
  }
  if (payload instanceof ArrayBuffer) {
    return Buffer.from(new Uint8Array(payload));
  }
  if (ArrayBuffer.isView(payload)) {
    return Buffer.from(payload.buffer, payload.byteOffset, payload.byteLength);
  }
  if (
    payload !== null &&
    typeof payload === "object" &&
    "payload" in payload
  ) {
    return toBytes((payload as { payload: unknown }).payload);
  }
  return Buffer.alloc(0);
}

export async function waitForCreate(
  traffic: TerminalTraffic,
  count = 1,
): Promise<CreateRequest> {
  await expect
    .poll(() => traffic.creates.length, { timeout: 20_000 })
    .toBeGreaterThanOrEqual(count);
  return traffic.creates[count - 1]!;
}

export async function pickServer(page: Page, name: string): Promise<void> {
  const card = page
    .locator(".server-pick")
    .filter({ has: page.getByText(name, { exact: true }) });
  await expect(card.first()).toBeVisible();
  await card.first().click();
}

export async function screenText(
  page: Page,
  scope = ".term-page",
): Promise<string> {
  const rows = page.locator(`${scope} .xterm-rows`).first();
  return rows.innerText();
}

export async function waitForScreenText(
  page: Page,
  pattern: RegExp,
  scope = ".term-page",
  timeout = 20_000,
): Promise<string> {
  let text = "";
  await expect
    .poll(
      async () => {
        text = await screenText(page, scope).catch(() => "");
        return pattern.test(text);
      },
      { timeout, intervals: [100, 250, 500] },
    )
    .toBe(true);
  return text;
}

export async function waitForScreenStable(
  page: Page,
  scope = ".term-page",
  quietMs = 1_200,
  timeout = 15_000,
): Promise<string> {
  let last = "";
  let stableSince = Date.now();
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const text = await screenText(page, scope).catch(() => "");
    if (text !== last) {
      last = text;
      stableSince = Date.now();
    } else if (Date.now() - stableSince >= quietMs) {
      return text;
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 150));
  }
  return last;
}

export async function focusTerminal(
  page: Page,
  scope = ".term-page",
): Promise<void> {
  await page.locator(`${scope} .xterm-helper-textarea`).first().focus();
}

export async function typeReal(
  context: BrowserContext,
  page: Page,
  text: string,
): Promise<void> {
  const cdp = await context.newCDPSession(page);
  try {
    for (const character of [...text]) {
      await cdp.send("Input.dispatchKeyEvent", {
        type: "keyDown",
        key: character,
        text: character,
        unmodifiedText: character,
      });
      await cdp.send("Input.dispatchKeyEvent", { type: "keyUp", key: character });
    }
  } finally {
    await cdp.detach();
  }
}

export async function pressEnter(page: Page): Promise<void> {
  await page.keyboard.press("Enter");
}

export async function pasteText(page: Page, text: string): Promise<void> {
  await page.evaluate((value) => {
    const textarea = document.querySelector<HTMLTextAreaElement>(
      ".xterm-helper-textarea",
    );
    if (textarea === null) {
      throw new Error("xterm textarea not found");
    }
    textarea.focus();
    const data = new DataTransfer();
    data.setData("text/plain", value);
    textarea.dispatchEvent(
      new ClipboardEvent("paste", {
        clipboardData: data,
        bubbles: true,
        cancelable: true,
      }),
    );
  }, text);
}

export async function runCommand(
  context: BrowserContext,
  page: Page,
  command: string,
  scope = ".term-page",
): Promise<void> {
  await focusTerminal(page, scope);
  await typeReal(context, page, command);
  await pressEnter(page);
}

export function lastResize(socket: SocketLog): [number, number] | null {
  for (let index = socket.frames.length - 1; index >= 0; index -= 1) {
    const frame = socket.frames[index]!;
    if (frame.opcode === CLIENT_RESIZE && frame.resize !== undefined) {
      return frame.resize;
    }
  }
  return null;
}

export function terminalLogoLines(): string[] {
  const path = resolve(
    repoRoot,
    "web/src/features/terminal/assets/gotham-ascii-logo.txt",
  );
  return readFileSync(path, "utf8")
    .split("\n")
    .filter((line) => line.trim() !== "");
}

export function terminalLogoSHA256(): string {
  const path = resolve(
    repoRoot,
    "web/src/features/terminal/assets/gotham-ascii-logo.txt",
  );
  return createHash("sha256").update(readFileSync(path)).digest("hex");
}

export interface ManagedServer {
  id: string;
  name: string;
  status: string;
  node_id: string | null;
  os: string;
}

export interface E2EAccount {
  email: string;
  password: string;
  accessToken: string;
  userId: string;
}

export interface FreshSession {
  user: { id: string; email: string };
  accessToken: string;
  refreshToken: string;
}

let cachedSession: { email: string; session: FreshSession } | null = null;

export async function loginFresh(
  request: APIRequestContext,
  account: E2EAccount,
): Promise<FreshSession> {
  if (cachedSession !== null && cachedSession.email === account.email) {
    return cachedSession.session;
  }
  const response = await request.post("/api/v1/auth/login", {
    data: { email: account.email, password: account.password },
  });
  if (!response.ok()) {
    throw new Error(`login: ${response.status()} ${await response.text()}`);
  }
  const body = (await response.json()) as {
    user: { id: string; email: string };
    access_token: string;
    refresh_token: string;
  };
  const session: FreshSession = {
    user: body.user,
    accessToken: body.access_token,
    refreshToken: body.refresh_token,
  };
  cachedSession = { email: account.email, session };
  return session;
}

export async function installSession(
  page: Page,
  session: FreshSession,
): Promise<void> {
  await page.addInitScript(
    ({ key, value }) => {
      window.localStorage.setItem(key, value);
    },
    {
      key: "gotham.auth.session",
      value: JSON.stringify({
        user: session.user,
        accessToken: session.accessToken,
        refreshToken: session.refreshToken,
      }),
    },
  );
}

export function authHeaders(account: E2EAccount): Record<string, string> {
  return { Authorization: `Bearer ${account.accessToken}` };
}

export async function listServers(
  request: APIRequestContext,
  account: E2EAccount,
): Promise<ManagedServer[]> {
  const response = await request.get("/api/v1/servers", {
    headers: authHeaders(account),
  });
  expect(response.ok(), `list servers: ${response.status()}`).toBe(true);
  const body = (await response.json()) as { servers?: ManagedServer[] };
  return body.servers ?? [];
}

export async function terminalServer(
  request: APIRequestContext,
  account: E2EAccount,
): Promise<ManagedServer | null> {
  const wanted = process.env.GOTHAM_E2E_TERMINAL_NODE ?? "";
  const servers = await listServers(request, account);
  const ready = servers.filter((server) => server.status === "ready");
  if (wanted !== "") {
    return ready.find((server) => server.node_id === wanted) ?? null;
  }
  return ready[0] ?? null;
}

export async function isPlatformAdmin(
  request: APIRequestContext,
  account: E2EAccount,
): Promise<boolean> {
  const response = await request.get("/api/v1/auth/me", {
    headers: authHeaders(account),
  });
  if (!response.ok()) {
    return false;
  }
  const body = (await response.json()) as { user?: { is_platform_admin?: boolean } };
  return body.user?.is_platform_admin === true;
}

export function requireTerminal(): boolean {
  return process.env.GOTHAM_E2E_REQUIRE_TERMINAL === "1";
}

export function skipOrFail(
  condition: boolean,
  reason: string,
  testSkip: (_condition: boolean, _reason: string) => void,
): void {
  if (!condition) {
    return;
  }
  if (requireTerminal()) {
    throw new Error(`GOTHAM_E2E_REQUIRE_TERMINAL=1: ${reason}`);
  }
  testSkip(true, reason);
}

export function agentPid(): number | null {
  const raw = process.env.GOTHAM_E2E_AGENT_PID ?? "";
  const parsed = Number.parseInt(raw, 10);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
}

export function ptmxCount(pid: number): number {
  if (process.platform === "linux") {
    const dir = `/proc/${pid}/fd`;
    const targets = execFileSync("ls", ["-l", dir], { encoding: "utf8" });
    return targets.split("\n").filter((line) => line.includes("ptmx")).length;
  }
  const output = execFileSync("lsof", ["-p", String(pid), "-Fn"], {
    encoding: "utf8",
  });
  return output.split("\n").filter((line) => line.includes("/dev/ptmx")).length;
}

export function processRunning(needle: string): boolean {
  const output = execFileSync("ps", ["-eo", "args="], { encoding: "utf8" });
  return output
    .split("\n")
    .some((line) => line.includes(needle) && !line.includes("ps -eo"));
}

export async function waitForProcessGone(
  needle: string,
  timeout = 15_000,
): Promise<boolean> {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    if (!processRunning(needle)) {
      return true;
    }
    await new Promise((resolveWait) => setTimeout(resolveWait, 200));
  }
  return false;
}

export function uniqueToken(prefix: string): string {
  return `${prefix}-${Date.now().toString(36)}-${Math.random()
    .toString(36)
    .slice(2, 8)}`;
}

export function serverTemporaryRoot(os: string): string {
  return os.toLowerCase().includes("darwin") ? "private/tmp" : "tmp";
}

export function fileManagerURL(serverId: string): string {
  return `/servers/${serverId}/files`;
}

export const terminalPageURL = "/terminal";
