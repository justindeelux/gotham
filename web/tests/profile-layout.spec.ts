import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, document, getComputedStyle, window:readonly */

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
    role: "member",
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

for (const width of [420, 480, 540]) {
  test(`identity facts stack label-over-value at ${width}px viewport`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await openProfile(page);
    const wrapWidth = await page.locator(".identity-facts-wrap").evaluate((wrap) => wrap.clientWidth);
    expect(wrapWidth).toBeLessThan(480);
    const facts = await factBoxes(page);
    expect(facts).toHaveLength(3);
    for (const fact of facts) {
      expect(fact.headerDisplay).toBe("block");
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
  expect(gaps.display.contentToFirst).toBeLessThanOrEqual(2);
  expect(Math.abs(gaps.display.titleToFirst - gaps.account.titleToFirst)).toBeLessThanOrEqual(1);
});
