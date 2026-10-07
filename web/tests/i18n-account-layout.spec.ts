import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global URL, document, window, HTMLElement, HTMLInputElement, requestAnimationFrame:readonly */
// Real-browser proof for the I18N-2 shell/auth/profile surfaces: the
// committed webdist is served statically with the control-plane API mocked,
// a session and locale are seeded in localStorage, and real Chromium
// measures the real Naive UI DOM (no backend involved). This replaces the
// earlier source-regex topbar check with committed layout assertions.

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
  is_platform_admin: false,
};

const CHROME_UA =
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
  "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36";

const sessions = [
  {
    id: "s-1",
    user_agent: CHROME_UA,
    ip: "203.0.113.7",
    created_at: "2026-10-01T10:00:00Z",
    last_used_at: "2026-10-05T10:00:00Z",
    current: true,
  },
  {
    id: "s-2",
    user_agent: "Mozilla/5.0 Firefox/127.0",
    ip: "",
    created_at: "2026-09-20T10:00:00Z",
    last_used_at: "2026-10-02T10:00:00Z",
    current: false,
  },
];

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

interface MockState {
  apiCalls: number;
}

/** mockApi counts every API call and answers the owned auth/profile routes. */
async function mockApi(page: Page, state: MockState): Promise<void> {
  await page.route("**/api/v1/**", async (route) => {
    state.apiCalls += 1;
    const url = new URL(route.request().url());
    const path = url.pathname.replace("/api/v1", "");
    const json = (data: unknown) =>
      route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(data) });
    if (path === "/auth/config") {
      await json({ registrationOpen: false });
      return;
    }
    if (path === "/auth/me" && route.request().method() === "GET") {
      await json({ user });
      return;
    }
    if (path === "/auth/me/sessions") {
      await json({ sessions });
      return;
    }
    if (path === "/teams") {
      await json({
        teams: [
          {
            id: "t-1",
            name: "core",
            role: "admin",
            member_count: 1,
            created_at: "2026-01-01T00:00:00Z",
          },
        ],
      });
      return;
    }
    if (path === "/version") {
      await json({ version: "v0.2.1-dev" });
      return;
    }
    await json({});
  });
}

/** seedStorage writes the locale and (optionally) a signed-in session. */
async function seedStorage(
  page: Page,
  locale: string,
  signedIn: boolean,
): Promise<void> {
  await page.addInitScript(
    ({ storedLocale, session }) => {
      window.localStorage.setItem("gotham-locale", storedLocale);
      if (session !== null) {
        window.localStorage.setItem(
          "gotham.auth.session",
          JSON.stringify(session),
        );
      }
    },
    {
      storedLocale: locale,
      session: signedIn
        ? { user, accessToken: "test-access", refreshToken: "test-refresh" }
        : null,
    },
  );
}

/** overflowOf reports horizontal overflow of the whole document. */
async function overflowOf(page: Page): Promise<{ scroll: number; inner: number }> {
  return page.evaluate(() => ({
    scroll: document.documentElement.scrollWidth,
    inner: window.innerWidth,
  }));
}

interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

/**
 * settledBox measures an element only after webfonts load and its box is
 * stable across animation frames. Popup entrance transitions and font swaps
 * can otherwise report pre-final geometry; polling for stability (bounded,
 * no arbitrary sleeps) makes the measurement deterministic.
 */
async function settledBox(
  page: Page,
  selector: string,
): Promise<Box> {
  await page.evaluate(() => document.fonts.ready);
  return page.locator(selector).evaluate((node) => {
    const element = node as HTMLElement;
    return new Promise<Box>((resolve) => {
      let last = "";
      let stable = 0;
      let guard = 0;
      const tick = (): void => {
        const rect = element.getBoundingClientRect();
        const key = `${rect.x}|${rect.y}|${rect.width}|${rect.height}`;
        stable = key === last ? stable + 1 : 0;
        last = key;
        guard += 1;
        if (stable >= 2 || guard > 120) {
          resolve({ x: rect.x, y: rect.y, width: rect.width, height: rect.height });
        } else {
          requestAnimationFrame(tick);
        }
      };
      tick();
    });
  });
}

for (const locale of ["en", "vi"]) {
  for (const width of [390, 900, 1280]) {
    test(`login shell fits at ${width}px in ${locale}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      const state: MockState = { apiCalls: 0 };
      await mockApi(page, state);
      await seedStorage(page, locale, false);
      await page.goto(`${baseURL}/login`);
      await page.locator(".auth-card").waitFor();

      const overflow = await overflowOf(page);
      expect(overflow.scroll).toBeLessThanOrEqual(overflow.inner);

      // The same selector lives in the auth shell with a readable label.
      const selector = page.locator(".auth-main .language-select");
      await expect(selector).toBeVisible();
      const heading = await page.locator(".auth-title").textContent();
      expect(heading?.trim().length).toBeGreaterThan(0);
      const placeholder = await page
        .locator("#login-email")
        .getAttribute("placeholder");
      expect(placeholder?.trim().length).toBeGreaterThan(0);
      if (locale === "en") {
        expect(heading).toContain("Sign in");
        expect(await page.title()).toBe("Sign in — Gotham");
      } else {
        expect(heading).toContain("Đăng nhập");
        expect(await page.title()).toBe("Đăng nhập — Gotham");
      }
    });

    test(`authenticated topbar fits at ${width}px in ${locale}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      const state: MockState = { apiCalls: 0 };
      await mockApi(page, state);
      await seedStorage(page, locale, true);
      await page.goto(`${baseURL}/settings/profile`);
      await page.locator(".profile-page").waitFor();

      const overflow = await overflowOf(page);
      expect(overflow.scroll).toBeLessThanOrEqual(overflow.inner);

      // Essential controls stay visible and inside the viewport.
      const rects = await page
        .locator(".topbar-right .language-select, .topbar-right [aria-label]")
        .evaluateAll((nodes) =>
          nodes.map((node) => node.getBoundingClientRect().toJSON()),
        );
      expect(rects.length).toBeGreaterThan(0);
      for (const rect of rects) {
        if (rect.width === 0) {
          continue;
        }
        expect(rect.right).toBeLessThanOrEqual(width + 1);
      }
      const selector = page.locator(".topbar-right .language-select");
      await expect(selector).toBeVisible();

      if (width === 390) {
        // Stubs yield room on phones; selector and account stay usable.
        expect(await page.locator(".topbar .search").count()).toBe(1);
        expect(await page.locator(".topbar .search").isVisible()).toBe(false);
        const stubVisible = await page
          .locator(".topbar .is-stub")
          .evaluateAll((nodes) =>
            nodes.filter(
              (node) => (node as HTMLElement).offsetParent !== null,
            ).length,
          );
        expect(stubVisible).toBe(0);
      } else {
        // Wide layouts keep every control visible.
        const stubVisible = await page
          .locator(".topbar .is-stub")
          .evaluateAll((nodes) =>
            nodes.filter(
              (node) => (node as HTMLElement).offsetParent !== null,
            ).length,
          );
        expect(stubVisible).toBe(2);
      }
    });

    test(`profile password pair stacks at ${width}px in ${locale}`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      const state: MockState = { apiCalls: 0 };
      await mockApi(page, state);
      await seedStorage(page, locale, true);
      await page.goto(`${baseURL}/settings/profile`);
      await page.locator("#profile-new-password").waitFor();

      // Authenticated identity actually resolves: the account card shows
      // the real display name and email, never the signed-in fallback or
      // empty facts from a degraded envelope mock.
      expect(await page.locator(".identity-name").textContent()).toContain(
        "Ada",
      );
      const facts = await page
        .locator(".identity-facts-wrap")
        .textContent();
      expect(facts).toContain("ada@gotham.dev");

      const pair = await page.evaluate(() => {
        const first = document.querySelector(
          ".n-card:has(#profile-new-password) .form-row > :first-child",
        );
        const second = document.querySelector(
          ".n-card:has(#profile-new-password) .form-row > :last-child",
        );
        const input = document.querySelector(
          "#profile-new-password",
        ) as HTMLInputElement | null;
        if (first === null || second === null || input === null) {
          throw new Error("expected the stacked password pair and input");
        }
        const firstBox = first.getBoundingClientRect().toJSON();
        const secondBox = second.getBoundingClientRect().toJSON();
        const inputBox = input.getBoundingClientRect().toJSON();
        return {
          stacked: secondBox.y >= firstBox.bottom - 1,
          inputWidth: inputBox.width,
          placeholderFits: input.scrollWidth <= input.clientWidth + 1,
        };
      });
      if (width === 390) {
        // Narrow containers stack the pair: full-width usable inputs.
        expect(pair.stacked).toBe(true);
        expect(pair.inputWidth).toBeGreaterThanOrEqual(200);
        expect(pair.placeholderFits).toBe(true);
      } else {
        expect(pair.inputWidth).toBeGreaterThanOrEqual(200);
        expect(pair.placeholderFits).toBe(true);
      }
    });
  }
}

test("auth footnote renders exact sentence per locale", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state: MockState = { apiCalls: 0 };
  await mockApi(page, state);
  const exact = {
    en: "Passwords are hashed with argon2id · 15-minute JWT access tokens with 30-day rotating refresh tokens · GitHub OAuth via the OAuthProvider interface.",
    vi: "Mật khẩu được băm bằng argon2id · JWT truy cập 15 phút với refresh token xoay vòng 30 ngày · GitHub OAuth qua giao diện OAuthProvider.",
  };
  for (const locale of ["en", "vi"] as const) {
    await seedStorage(page, locale, false);
    await page.goto(`${baseURL}/login`);
    await page.locator(".auth-footnote").waitFor();
    const text = await page.locator(".auth-footnote").evaluate((node) =>
      (node.textContent ?? "").replace(/\s+/g, " ").trim(),
    );
    // Exact match: no joined words in EN, no stray space before the
    // period in VI.
    expect(text).toBe(exact[locale]);
    await page.evaluate(() => window.localStorage.clear());
  }
});

test("locale switch on login issues no API call and keeps the draft", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  const state: MockState = { apiCalls: 0 };
  await mockApi(page, state);
  await seedStorage(page, "en", false);
  await page.goto(`${baseURL}/login`);
  await page.locator(".auth-card").waitFor();

  await page.locator("#login-email").fill("not-an-email");
  await page.locator("#login-password").fill("secret");
  const callsBefore = state.apiCalls;
  expect(callsBefore).toBeGreaterThan(0);
  await page.locator(".language-select").click();
  await page.getByRole("menuitemradio").nth(1).click();
  await page.locator(".auth-title", { hasText: "Đăng nhập" }).waitFor();
  expect(state.apiCalls).toBe(callsBefore);
  expect(await page.locator("#login-email").inputValue()).toBe("not-an-email");
  expect(await page.locator("#login-password").inputValue()).toBe("secret");
  expect(page.url()).toContain("/login");
});

for (const locale of ["en", "vi"] as const) {
  for (const width of [390, 900, 1280]) {
    test(`session popconfirms stay inside the viewport at ${width}px in ${locale}`, async ({
      page,
    }) => {
      const labels =
        locale === "en"
          ? {
              bulk: "Sign out all other devices",
              row: "Sign out",
              positiveBulk: "Sign out others",
              positiveRow: "Sign out",
              negative: "Keep",
            }
          : {
              bulk: "Đăng xuất mọi thiết bị khác",
              row: "Đăng xuất",
              positiveBulk: "Đăng xuất các phiên khác",
              positiveRow: "Đăng xuất",
              negative: "Giữ lại",
            };
      await page.setViewportSize({ width, height: 900 });
      const state: MockState = { apiCalls: 0 };
      await mockApi(page, state);
      await seedStorage(page, locale, true);
      await page.goto(`${baseURL}/settings/profile`);
      await page.locator(".session-row").first().waitFor();

      // Bulk confirm: buttons visible and fully inside the viewport.
      await page.getByRole("button", { name: labels.bulk }).click();
      const bulk = page.locator(".n-popconfirm");
      await bulk.waitFor();
      const bulkBox = await settledBox(page, ".n-popconfirm");
      expect(bulkBox.x).toBeGreaterThanOrEqual(-1);
      expect(bulkBox.x + bulkBox.width).toBeLessThanOrEqual(width + 1);
      for (const name of [labels.positiveBulk, labels.negative]) {
        const button = bulk.getByRole("button", { name });
        await expect(button).toBeVisible();
        const box = await button.boundingBox();
        expect(box, `expected ${name} to measure`).not.toBeNull();
        expect(box!.x).toBeGreaterThanOrEqual(-1);
        expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
      }
      // Dismissing via the negative action closes without any API call.
      const callsBefore = state.apiCalls;
      await bulk.getByRole("button", { name: labels.negative }).click();
      await bulk.waitFor({ state: "detached" });
      expect(state.apiCalls).toBe(callsBefore);

      // Row confirms: every row (first/current and non-current) contains
      // its popconfirm and buttons fully inside the viewport.
      const rowCount = await page.locator(".session-row").count();
      expect(rowCount).toBeGreaterThanOrEqual(2);
      for (let index = 0; index < rowCount; index += 1) {
        await page
          .locator(".session-row")
          .nth(index)
          .getByRole("button", { name: labels.row })
          .click();
        const row = page.locator(".n-popconfirm");
        await row.waitFor();
        const rowBox = await settledBox(page, ".n-popconfirm");
        expect(rowBox.x).toBeGreaterThanOrEqual(-1);
        expect(rowBox.x + rowBox.width).toBeLessThanOrEqual(width + 1);
        const rowPositive = row.getByRole("button", { name: labels.positiveRow });
        await expect(rowPositive).toBeVisible();
        const rowPositiveBox = await rowPositive.boundingBox();
        expect(rowPositiveBox!.x).toBeGreaterThanOrEqual(-1);
        expect(rowPositiveBox!.x + rowPositiveBox!.width).toBeLessThanOrEqual(
          width + 1,
        );
        // Dismiss via negative: closes with no API call. On the last row,
        // additionally switch locale through the dropdown menu: selecting
        // from the menu layer dismisses the open confirm (Naive popover
        // layering) with zero API calls, and the re-opened confirm renders
        // the flipped text inside the viewport with stable focus.
        const rowCallsBefore = state.apiCalls;
        let dismissName = labels.negative;
        if (index === rowCount - 1) {
          const other = locale === "en" ? "vi" : "en";
          const flipped = other === "vi"
            ? "sẽ phải đăng nhập lại"
            : "will need to sign in again";
          const selector = page.locator(".language-select");
          await selector.click();
          await page.getByRole("menuitemradio").nth(other === "vi" ? 1 : 0).click();
          await expect(selector).toBeFocused();
          await row.waitFor({ state: "detached" });
          expect(state.apiCalls).toBe(rowCallsBefore);
          // Re-open in the new locale: flipped text, contained, dismissible.
          await page
            .locator(".session-row")
            .nth(index)
            .getByRole("button", { name: other === "vi" ? "Đăng xuất" : "Sign out" })
            .click();
          await row.waitFor();
          await expect(row).toContainText(flipped);
          const switchedBox = await settledBox(page, ".n-popconfirm");
          expect(switchedBox.x).toBeGreaterThanOrEqual(-1);
          expect(switchedBox.x + switchedBox.width).toBeLessThanOrEqual(
            width + 1,
          );
          expect(state.apiCalls).toBe(rowCallsBefore);
          dismissName = other === "vi" ? "Giữ lại" : "Keep";
        }
        await row.getByRole("button", { name: dismissName }).click();
        await row.waitFor({ state: "detached" });
        expect(state.apiCalls).toBe(rowCallsBefore);
      }
    });
  }
}

for (const authenticated of [false, true]) {
  test(`language dropdown supports keyboard selection on mobile (authenticated: ${authenticated})`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 800 });
    const state: MockState = { apiCalls: 0 };
    await mockApi(page, state);
    await seedStorage(page, "en", authenticated);
    await page.goto(`${baseURL}/${authenticated ? "settings/profile" : "login"}`);
    const selector = page.locator(".language-select");
    await expect(selector).toBeVisible();
    await selector.focus();
    await page.keyboard.press("Enter");
    await expect(selector).toHaveAttribute("aria-expanded", "true");
    const options = page.getByRole("menuitemradio");
    await expect(options).toHaveCount(2);
    await expect(options.nth(0)).toHaveAttribute("aria-checked", "true");
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Enter");
    await expect(page.locator("html")).toHaveAttribute("lang", "vi");
    expect(await page.evaluate(() => window.localStorage.getItem("gotham-locale"))).toBe("vi");
    await expect(selector).toHaveAttribute("aria-expanded", "false");
    await expect(selector).toBeFocused();
    await page.keyboard.press("Space");
    await expect(options.nth(1)).toHaveAttribute("aria-checked", "true");
    await page.getByRole("menuitemradio").nth(1).click();
    await expect(selector).toHaveAttribute("aria-expanded", "false");
    await selector.click();
    await expect(options.nth(1)).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(selector).toHaveAttribute("aria-expanded", "false");
    await expect(selector).toBeFocused();
  });
}
