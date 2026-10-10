import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, window:readonly */
// Real-browser proof for the JUS-91 progress-card spacing: the committed
// webdist is served statically with the control-plane API mocked, a session
// is seeded in localStorage, and task frames are injected at the mocked
// WebSocket endpoint. Real Chromium screenshots the stacked cards at
// desktop and phone widths and measures the gaps (no overlap, header to
// content >= 12px, body rows >= 8px, stack >= 12px). No backend is involved.

const testsDir = dirname(fileURLToPath(import.meta.url));
const webdistRoot = resolve(testsDir, "../../internal/server/webdist");
const shotsDir = resolve(testsDir, "../../.playwright-mcp");

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
    is_platform_admin: false,
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

function taskFrame(event: Record<string, unknown>, channel: string): string {
  return JSON.stringify({ channel, type: "task", data: JSON.stringify(event) });
}

function seedEvent(taskId: string, status: string, extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    task_id: taskId,
    kind: "deploy",
    name: `app-${taskId}`,
    status,
    step: status === "running" ? "building" : "",
    progress: status === "running" ? 50 : 0,
    seq: 2,
    app_id: "app-1",
    deployment_id: taskId,
    server_id: "srv-1",
    project_id: "proj-1",
    environment_id: "env-1",
    team_id: "team-1",
    error: status === "failed" ? "container reported unhealthy" : "",
    ...extra,
  };
}

let server: Server;
let baseURL = "";

test.beforeAll(async () => {
  server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url ?? "/", "http://127.0.0.1");
      let path = decodeURIComponent(url.pathname);
      if (path === "/" || !extname(path)) {
        // SPA fallback: client-side routes serve index.html.
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

/** mockApi seeds the session, answers the shell calls, and injects tasks. */
async function mockApi(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
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
    await route.fulfill({ json: { servers: [] } });
  });
  await page.route("**/api/v1/projects", async (route) => {
    await route.fulfill({ json: { projects: [] } });
  });
  await page.routeWebSocket(/\/api\/v1\/ws/, (ws) => {
    ws.onMessage((message) => {
      const text = typeof message === "string" ? message : "";
      const channel = text.includes("tasks:")
        ? text.split('"').find((part) => part.startsWith("tasks:"))
        : undefined;
      if (channel) {
        ws.send(taskFrame(seedEvent("dep-running", "running"), channel));
        ws.send(taskFrame(seedEvent("dep-failed", "failed"), channel));
        ws.send(taskFrame(seedEvent("dep-queued", "queued"), channel));
        ws.send(JSON.stringify({ channel, type: "task_snapshot" }));
      }
    });
  });
}

interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

async function boxOf(selector: string, page: Page): Promise<Box> {
  const box = await page.locator(selector).first().boundingBox();
  expect(box, `missing box for ${selector}`).not.toBeNull();
  return box as Box;
}

test("stacked cards keep token gaps at 1280px", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects`);
  await expect(page.locator('[data-task-id="dep-running"]')).toBeVisible();
  await expect(page.locator('[data-task-id="dep-failed"]')).toBeVisible();
  await expect(page.locator('[data-task-id="dep-queued"]')).toBeVisible();

  // Three stacked cards, left-aligned, separated by the stack token (12px).
  const ids = ["dep-running", "dep-failed", "dep-queued"];
  const cards: Box[] = [];
  for (const id of ids) {
    cards.push(await boxOf(`[data-task-id="${id}"]`, page));
  }
  for (const card of cards) {
    expect(Math.abs(card.x - cards[0].x)).toBeLessThanOrEqual(1);
  }
  for (let i = 1; i < cards.length; i++) {
    const gap = cards[i].y - (cards[i - 1].y + cards[i - 1].height);
    expect(gap, `stack gap before ${ids[i]}`).toBeGreaterThanOrEqual(12);
  }

  // Header to content keeps its own larger gap; body rows breathe evenly.
  const head = await boxOf('[data-task-id="dep-running"] .task-head', page);
  const body = await boxOf('[data-task-id="dep-running"] .task-body', page);
  expect(body.y - (head.y + head.height)).toBeGreaterThanOrEqual(12);
  const rows = await page.locator('[data-task-id="dep-running"] .task-body > *').all();
  expect(rows.length).toBeGreaterThanOrEqual(2);
  let previous = -1;
  for (const row of rows) {
    const box = await row.boundingBox();
    expect(box).not.toBeNull();
    if (previous >= 0 && box) {
      expect(box.y - previous).toBeGreaterThanOrEqual(8);
    }
    if (box) {
      previous = box.y + box.height;
    }
  }

  await page.locator(".task-cards").screenshot({ path: `${shotsDir}/tasks-cards-1280.png` });
});

test("stacked cards fit without overflow at 480px", async ({ page }) => {
  await page.setViewportSize({ width: 480, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/projects`);
  await expect(page.locator('[data-task-id="dep-running"]')).toBeVisible();
  await expect(page.locator('[data-task-id="dep-queued"]')).toBeVisible();

  const stack = await boxOf(".task-cards", page);
  expect(stack.x + stack.width).toBeLessThanOrEqual(480);
  const cards = await page.locator(".task-card").all();
  expect(cards.length).toBe(3);
  let previous = -1;
  for (const card of cards) {
    const box = await card.boundingBox();
    expect(box).not.toBeNull();
    if (box) {
      expect(box.x + box.width).toBeLessThanOrEqual(480);
      if (previous >= 0) {
        // No overlap between stacked cards at phone width either.
        expect(box.y - previous).toBeGreaterThanOrEqual(12);
      }
      previous = box.y + box.height;
    }
  }

  await page.locator(".task-cards").screenshot({ path: `${shotsDir}/tasks-cards-480.png` });
});
