import { createServer } from "node:http";
import type { Server } from "node:http";
import { readFile, stat } from "node:fs/promises";
import { dirname, extname, join, normalize, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global getComputedStyle:readonly */
// getComputedStyle runs inside page.evaluate (browser context); the repo
// lint envs do not cover tests/, so the global is declared here.

// Behavioural proof for the sessions panel (JUS-28 fix round 1): real
// Chromium boots the REAL built app (committed webdist, served locally),
// the session routes are mocked by interception, and every row is measured.
// Created and Last active must be fully inside the row box at every width,
// and the stack keys off the PANEL container width (480px threshold), not
// the viewport.

const testsDir = dirname(fileURLToPath(import.meta.url));
const distDir = resolve(testsDir, "../../internal/server/webdist");

const mime: Record<string, string> = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".svg": "image/svg+xml",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".ttf": "font/ttf",
};

let server: Server;
let baseUrl = "";

test.beforeAll(async () => {
  server = createServer(async (request, response) => {
    try {
      const url = new globalThis.URL(request.url ?? "/", "http://local");
      let file = normalize(join(distDir, decodeURIComponent(url.pathname)));
      if (!file.startsWith(distDir)) {
        response.writeHead(403);
        response.end();
        return;
      }
      try {
        if ((await stat(file)).isDirectory()) {
          file = join(file, "index.html");
        }
      } catch {
        file = join(distDir, "index.html");
      }
      const body = await readFile(file);
      response.writeHead(200, {
        "content-type": mime[extname(file)] ?? "application/octet-stream",
      });
      response.end(body);
    } catch {
      response.writeHead(500);
      response.end();
    }
  });
  await new Promise<void>((done) => server.listen(0, "127.0.0.1", done));
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  baseUrl = `http://127.0.0.1:${port}`;
});

test.afterAll(async () => {
  await new Promise<void>((done, failed) => {
    server.close((error) => (error ? failed(error) : done()));
  });
});

const user = {
  id: "u-1",
  email: "ada@gotham.dev",
  created_at: "2026-03-04T12:00:00Z",
  display_name: "Ada",
  has_password: true,
  is_platform_admin: false,
};

// Long IPv6 plus a long agent stress the phone layout; the blank ip/agent
// row covers the "unknown"/"Unknown device" fallbacks.
const sessions = [
  {
    id: "s-current",
    user_agent:
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
      "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
    ip: "203.0.113.7",
    created_at: "2026-10-01T10:00:00Z",
    last_used_at: "2026-10-03T10:00:00Z",
    current: true,
  },
  {
    id: "s-ipv6",
    user_agent:
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:127.0) Gecko/20100101 " +
      "Firefox/127.0",
    ip: "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
    created_at: "2026-09-20T10:00:00Z",
    last_used_at: "2026-10-02T10:00:00Z",
    current: false,
  },
  {
    id: "s-blank",
    user_agent: "",
    ip: "",
    created_at: "2026-09-01T10:00:00Z",
    last_used_at: "2026-09-15T10:00:00Z",
    current: false,
  },
];

/** openProfile boots the built app at width with the API mocked. */
async function openProfile(page: Page, width: number): Promise<void> {
  await page.setViewportSize({ width, height: 900 });
  await page.addInitScript((seed: string) => {
    globalThis.window.localStorage.setItem("gotham.auth.session", seed);
  }, JSON.stringify({ user, accessToken: "test", refreshToken: "test" }));
  await page.route("**/api/v1/**", (route) => {
    const url = route.request().url();
    if (url.endsWith("/auth/me/sessions")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ sessions }),
      });
    }
    if (url.endsWith("/auth/me")) {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ user }),
      });
    }
    return route.abort();
  });
  await page.goto(`${baseUrl}/settings/profile`);
  await page.locator(".session-row").first().waitFor();
}

for (const width of [375, 480, 600, 700, 900, 1280]) {
  test(`sessions rows keep Created and Last active visible at ${width}px`, async ({
    page,
  }) => {
    await openProfile(page, width);
    const rows = page.locator(".session-row");
    await expect(rows).toHaveCount(3);

    // The stack follows the panel container, not the viewport.
    const panelWidth = await page
      .locator(".sessions-panel")
      .evaluate((panel) => panel.clientWidth);
    const direction = await rows
      .first()
      .evaluate((row) => getComputedStyle(row).flexDirection);
    expect(direction, `panel ${panelWidth}px at viewport ${width}px`).toBe(
      panelWidth <= 480 ? "column" : "row",
    );

    for (let index = 0; index < 3; index += 1) {
      const row = rows.nth(index);
      // Both <time> values sit fully inside the row box (no clipping).
      const boxes = await row.evaluate((element) => {
        const times = element.querySelectorAll("time");
        const rowBox = element.getBoundingClientRect();
        return [0, 1].map((slot) => {
          const timeBox = times[slot]?.getBoundingClientRect();
          return {
            rowLeft: rowBox.left,
            rowRight: rowBox.right,
            timeLeft: timeBox?.left ?? 0,
            timeRight: timeBox?.right ?? 0,
          };
        });
      });
      expect(boxes).toHaveLength(2);
      for (const box of boxes) {
        expect(box.timeLeft, `row ${index} at ${width}px`).toBeGreaterThanOrEqual(
          box.rowLeft - 1,
        );
        expect(box.timeRight, `row ${index} at ${width}px`).toBeLessThanOrEqual(
          box.rowRight + 1,
        );
      }
      // In the stacked layout the facts wrap instead of ellipsizing.
      if (direction === "column") {
        const meta = await row
          .locator(".session-meta")
          .evaluate((fact) => ({
            scroll: fact.scrollWidth,
            client: fact.clientWidth,
          }));
        expect(meta.scroll, `meta clipped at ${width}px`).toBeLessThanOrEqual(
          meta.client + 1,
        );
      }
    }
  });
}
