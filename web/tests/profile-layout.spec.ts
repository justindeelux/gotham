import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, document, getComputedStyle, window:readonly */
// URL builds request URLs in the static server; document, getComputedStyle
// and window run in browser context (page.evaluate, addInitScript) — same
// pattern as form-layout.spec.ts, since the repo lint envs do not cover tests/.

// Real-browser proof for the PF-3b profile fixes (F1/F2): the committed
// webdist is served statically with the control-plane API mocked, a session
// is seeded in localStorage, and real Chromium measures the real Naive UI
// DOM. No backend is involved. Viewports mirror the r1 review measurements:
// 480/520/560/600px viewports yield identity-facts containers of
// 338/378/418/458px (below the 480px query threshold); 700px yields 538px.

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
    is_platform_admin: false,
  },
  accessToken: "test-access",
  refreshToken: "test-refresh",
};

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

/** openProfile loads /settings/profile with a seeded session and mocked API. */
async function openProfile(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: session.user });
  });
  await page.route("**/api/v1/teams**", async (route) => {
    await route.fulfill({ json: { teams: [] } });
  });
  await page.route("**/api/v1/version", async (route) => {
    await route.fulfill({ json: { version: "v0.2.1-dev" } });
  });
  await page.goto(`${baseURL}/settings/profile`);
  await page.locator(".identity-facts-wrap").waitFor();
}

/** factBoxes returns each fact row's header and value boxes in order. */
async function factBoxes(page: Page) {
  return page.locator(".identity-facts-wrap .n-descriptions-table-row").evaluateAll((rows) =>
    rows.map((row) => {
      const header = row.querySelector(".n-descriptions-table-header");
      const content = row.querySelector(".n-descriptions-table-content");
      if (header === null || content === null) {
        throw new Error("expected a header and a value cell per fact row");
      }
      const headerBox = header.getBoundingClientRect().toJSON();
      const contentBox = content.getBoundingClientRect().toJSON();
      return {
        headerDisplay: getComputedStyle(header).display,
        header: { x: headerBox.x, y: headerBox.y, width: headerBox.width },
        content: { x: contentBox.x, y: contentBox.y, width: contentBox.width },
      };
    }),
  );
}

for (const width of [480, 520, 560, 600]) {
  test(`identity facts stack label-over-value at ${width}px viewport`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await openProfile(page);
    const wrapWidth = await page.locator(".identity-facts-wrap").evaluate((wrap) => wrap.clientWidth);
    // The container sits below the 480px query threshold at these viewports.
    expect(wrapWidth).toBeLessThan(480);
    const facts = await factBoxes(page);
    expect(facts).toHaveLength(3);
    for (const fact of facts) {
      expect(fact.headerDisplay).toBe("block");
      // Label above value, both full container width, value never squeezed.
      expect(fact.content.y).toBeGreaterThanOrEqual(fact.header.y);
      expect(Math.abs(fact.header.width - wrapWidth)).toBeLessThanOrEqual(2);
      expect(Math.abs(fact.content.width - wrapWidth)).toBeLessThanOrEqual(2);
      expect(fact.content.width).toBeGreaterThanOrEqual(200);
    }
  });
}

test("identity facts sit side by side at a wide viewport", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await openProfile(page);
  const wrapWidth = await page.locator(".identity-facts-wrap").evaluate((wrap) => wrap.clientWidth);
  expect(wrapWidth).toBeGreaterThanOrEqual(480);
  const facts = await factBoxes(page);
  expect(facts).toHaveLength(3);
  for (const fact of facts) {
    // Same row: label left, value right, vertically overlapping.
    expect(Math.abs(fact.header.y - fact.content.y)).toBeLessThanOrEqual(2);
    expect(fact.header.x).toBeLessThan(fact.content.x);
  }
});

test("display-name input sits right under the card title", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await openProfile(page);
  const gaps = await page.evaluate(() => {
    const gapOf = (cardSelector: string, firstSelector: string) => {
      const card = document.querySelector(cardSelector);
      const title = card?.querySelector(".n-card-header__main");
      const content = card?.querySelector(".n-card-content");
      const first = card?.querySelector(firstSelector);
      if (title === null || title === undefined || content === null || first === null) {
        throw new Error(`expected title, content and first child in ${cardSelector}`);
      }
      const titleBottom = title.getBoundingClientRect().bottom;
      return {
        titleToFirst: first.getBoundingClientRect().top - titleBottom,
        contentToFirst: first.getBoundingClientRect().top - content.getBoundingClientRect().top,
      };
    };
    return {
      display: gapOf(".n-card:has(#profile-display-name)", "#profile-display-name"),
      account: gapOf(".n-card:has(.identity-facts-wrap)", ".identity-row"),
    };
  });
  // No reserved label row: the input is the first thing in the card content.
  expect(gaps.display.contentToFirst).toBeLessThanOrEqual(2);
  // Same title-to-content rhythm as the Account card: only shared card chrome.
  expect(Math.abs(gaps.display.titleToFirst - gaps.account.titleToFirst)).toBeLessThanOrEqual(1);
});
