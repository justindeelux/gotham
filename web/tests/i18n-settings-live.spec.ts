import { spawn } from "node:child_process";
import type { ChildProcess } from "node:child_process";
import net from "node:net";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Browser, BrowserContext, Page } from "@playwright/test";

/* global window:readonly, document:readonly, StorageEvent:readonly, setTimeout:readonly */
// window/document/StorageEvent/setTimeout run inside page.evaluate (browser
// context) or Node timers; the repo lint envs do not cover tests/, so the
// globals are declared here (same pattern as form-feedback.spec.ts).

// Real-component en/vi proof for I18N-8 (replaces static-replica-only
// coverage): the built SPA from vite preview, deterministic route mocks (no
// backend, no outbound delivery), locale via the real storage tab-sync path.
// Teams / Notifications / Domains pages at 390/900/1280, dialog checks, a
// live ChannelCard failure/draft switch and a retained team 409.

const testsDir = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(testsDir, "..");
const previewPort = 4273;
const previewUrl = `http://127.0.0.1:${previewPort}`;

const user = {
  id: "u1",
  email: "owner@example.com",
  created_at: "2026-01-01T00:00:00Z",
};

const personalTeam = {
  id: "t-personal",
  name: "Acme",
  is_personal: true,
  role: "owner",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

const longTeam = {
  id: "t1",
  name: "Platform Engineering",
  is_personal: false,
  role: "owner",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

const discordChannel = {
  id: "ch1",
  team_id: "t1",
  name: "Deploy alerts",
  kind: "discord",
  enabled: true,
  events: ["deploy_success", "deploy_failure"],
  resource_type: "",
  resource_id: "",
  config: { webhook_url: "https://…/••••" },
  secrets_configured: true,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

const shopApp = { id: "app-1", name: "Shop", base_domain: "shop.example.com" };

/** ApiLog counts every mocked API call for no-request-on-switch proofs. */
interface ApiLog {
  calls: string[];
}

function json(status: number, body: unknown) {
  return { status, contentType: "application/json", body: JSON.stringify(body) };
}

/** installMocks fulfills every API call the three pages can make. */
async function installMocks(
  context: BrowserContext,
  log: ApiLog,
  overrides: Record<string, () => unknown> = {},
) {
  await context.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = request.url();
    const method = request.method();
    const path = url.split("/api/v1")[1]!.split("?")[0]!;
    log.calls.push(`${method} ${path}`);
    const key = `${method} ${path}`;
    if (overrides[key] ?? overrides[path]) {
      const responder = (overrides[key] ?? overrides[path])!;
      const answer = responder() as
        | { status: number; body: unknown }
        | undefined;
      if (answer) {
        await route.fulfill(json(answer.status, answer.body));
        return;
      }
    }
    if (method === "GET" && path === "/auth/me") {
      await route.fulfill(json(200, { user }));
      return;
    }
    if (method === "GET" && path === "/auth/config") {
      await route.fulfill(json(200, { registrationOpen: false }));
      return;
    }
    if (method === "GET" && path === "/teams") {
      await route.fulfill(json(200, { teams: [personalTeam, longTeam] }));
      return;
    }
    if (method === "GET" && /\/teams\/[^/]+\/members$/.test(path)) {
      await route.fulfill(
        json(200, {
          members: [
            {
              user_id: "u1",
              email: "owner@example.com",
              role: "owner",
              created_at: "2026-01-01T00:00:00Z",
            },
          ],
        }),
      );
      return;
    }
    if (method === "GET" && /\/teams\/[^/]+\/invites$/.test(path)) {
      await route.fulfill(json(200, { invites: [] }));
      return;
    }
    if (method === "GET" && path === "/notification-channels") {
      await route.fulfill(json(200, { channels: [discordChannel] }));
      return;
    }
    if (method === "GET" && path === "/proxy/dns-providers") {
      await route.fulfill(json(200, { providers: [] }));
      return;
    }
    if (method === "GET" && path === "/proxy/certificates") {
      await route.fulfill(json(200, { certificates: [] }));
      return;
    }
    if (method === "GET" && path === "/proxy/redirects") {
      await route.fulfill(json(200, { redirects: [] }));
      return;
    }
    if (method === "GET" && path === "/applications") {
      await route.fulfill(json(200, { applications: [shopApp] }));
      return;
    }
    if (method === "GET" && path === "/databases") {
      await route.fulfill(json(200, { databases: [] }));
      return;
    }
    await route.fulfill(json(404, { message: `unmocked ${method} ${path}` }));
  });
}

/** openPage seeds session+locale, installs mocks and navigates to a route. */
async function openPage(
  browser: Browser,
  routePath: string,
  locale: "en" | "vi",
  width: number,
  log: ApiLog,
  overrides: Record<string, () => unknown> = {},
): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ viewport: { width, height: 900 } });
  await context.addInitScript(
    ({ session, lang }: { session: string; lang: string }) => {
      window.localStorage.setItem("gotham.auth.session", session);
      window.localStorage.setItem("gotham-locale", lang);
    },
    {
      session: JSON.stringify({
        user,
        accessToken: "test-token",
        refreshToken: "test-refresh",
      }),
      lang: locale,
    },
  );
  await installMocks(context, log, overrides);
  const page = await context.newPage();
  await page.goto(`${previewUrl}${routePath}`);
  return { context, page };
}

/** switchLocale flips language through the real storage tab-sync path. */
async function switchLocale(page: Page, locale: "en" | "vi"): Promise<void> {
  await page.evaluate((lang: string) => {
    window.localStorage.setItem("gotham-locale", lang);
    window.dispatchEvent(
      new StorageEvent("storage", { key: "gotham-locale", newValue: lang }),
    );
  }, locale);
}

/** expectNoOverflow asserts the document never scrolls sideways. */
async function expectNoOverflow(page: Page, label: string): Promise<void> {
  const overflow = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(
    overflow.scrollWidth,
    `${label}: sideways scroll ${overflow.scrollWidth} > ${overflow.clientWidth}`,
  ).toBeLessThanOrEqual(overflow.clientWidth + 1);
}

let preview: ChildProcess | null = null;

test.beforeAll(async () => {
  preview = spawn("npx", ["vite", "preview", "--port", String(previewPort), "--strictPort", "--host", "127.0.0.1"], {
    cwd: webRoot,
    stdio: "pipe",
  });
  const deadline = Date.now() + 30_000;
  for (;;) {
    const open = await new Promise<boolean>((done) => {
      const socket = net.connect(previewPort, "127.0.0.1");
      socket.once("connect", () => {
        socket.end();
        done(true);
      });
      socket.once("error", () => done(false));
    });
    if (open) {
      return;
    }
    if (Date.now() > deadline) {
      throw new Error("vite preview did not open port 4273 in 30s");
    }
    await new Promise((done) => setTimeout(done, 200));
  }
});

test.afterAll(async () => {
  preview?.kill("SIGTERM");
  preview = null;
});

for (const width of [390, 900, 1280] as const) {
  test(`I18N-8 teams page fits en/vi at ${width}px with a readable selected-team header`, async ({
    browser,
  }) => {
    const log: ApiLog = { calls: [] };
    const { context, page } = await openPage(browser, "/teams", "en", width, log);
    try {
      await expect(page.getByRole("heading", { name: "Teams", exact: true })).toBeVisible();
      // Select the long-named team so the header-extra pressure is real.
      await page.getByRole("button", { name: "Platform Engineering" }).click();
      await expect(page.getByText("your role: owner")).toBeVisible();
      await expectNoOverflow(page, `teams en ${width}`);
      const titleWidthEn = await page
        .locator(".team-card .n-card-header__main")
        .evaluate((element) => element.getBoundingClientRect().width);
      expect(titleWidthEn, `teams en ${width} title crushed`).toBeGreaterThan(80);

      await switchLocale(page, "vi");
      await expect(page.getByRole("heading", { name: "Nhóm", exact: true })).toBeVisible();
      await expect(
        page.getByText("vai trò của bạn: chủ sở hữu"),
      ).toBeVisible();
      await expectNoOverflow(page, `teams vi ${width}`);
      // Regression guard for the 390px crush (was a 28px title column).
      const titleWidth = await page
        .locator(".team-card .n-card-header__main")
        .evaluate((element) => element.getBoundingClientRect().width);
      expect(titleWidth, `teams vi ${width} title crushed`).toBeGreaterThan(80);
      await page.screenshot({ path: `test-results/i18n8-teams-vi-${width}.png` });
    } finally {
      await context.close();
    }
  });

  test(`I18N-8 notifications page fits en/vi at ${width}px`, async ({ browser }) => {
    const log: ApiLog = { calls: [] };
    const { context, page } = await openPage(
      browser,
      "/settings/notifications",
      "en",
      width,
      log,
    );
    try {
      await expect(
        page.getByRole("heading", { name: "Notification channels", exact: true }),
      ).toBeVisible();
      await expect(page.getByText("Deploy alerts")).toBeVisible();
      await expectNoOverflow(page, `notifications en ${width}`);

      await switchLocale(page, "vi");
      await expect(
        page.getByRole("heading", { name: "Kênh thông báo", exact: true }),
      ).toBeVisible();
      await expect(page.locator("span.n-tag__content", { hasText: /^Webhook Discord$/ })).toBeVisible();
      await expectNoOverflow(page, `notifications vi ${width}`);
      await page.screenshot({ path: `test-results/i18n8-notifications-vi-${width}.png` });
    } finally {
      await context.close();
    }
  });

  test(`I18N-8 domains page fits en/vi at ${width}px`, async ({ browser }) => {
    const log: ApiLog = { calls: [] };
    const { context, page } = await openPage(browser, "/domains", "en", width, log);
    try {
      await expect(page.getByRole("heading", { name: "Domains & SSL", exact: true })).toBeVisible();
      await expectNoOverflow(page, `domains en ${width}`);

      await switchLocale(page, "vi");
      await expect(page.getByRole("heading", { name: "Tên miền & SSL", exact: true })).toBeVisible();
      await expect(page.locator("span.n-tabs-tab__label", { hasText: /^Nhà cung cấp DNS$/ })).toBeVisible();
      await expectNoOverflow(page, `domains vi ${width}`);
      await page.screenshot({ path: `test-results/i18n8-domains-vi-${width}.png` });
    } finally {
      await context.close();
    }
  });
}

test("I18N-8 channel draft survives a switch with zero API calls", async ({
  browser,
}) => {
  const log: ApiLog = { calls: [] };
  const { context, page } = await openPage(
    browser,
    "/settings/notifications",
    "en",
    900,
    log,
  );
  try {
    await expect(page.getByText("Deploy alerts")).toBeVisible();
    await page.getByRole("button", { name: "New channel" }).click();
    await expect(page.getByText("Team-wide (all resources)")).toBeVisible();

    await page.locator('[aria-label="Channel name"] input').fill("Typed channel");
    await page.locator('[aria-label="Webhook URL"] input').fill("https://typed/hook");
    await page.getByLabel("Resource scope").click();
    await page.locator(".n-base-select-option", { hasText: /^Application$/ }).click();
    await page.getByLabel("Resource", { exact: true }).click();
    await page.locator(".n-base-select-option", { hasText: /^Shop$/ }).click();

    const callsBefore = log.calls.length;
    expect(callsBefore).toBeGreaterThan(0);
    await switchLocale(page, "vi");
    await expect(page.getByText("Kênh thông báo mới")).toBeVisible();
    // Draft, scope pick and gating survive; the switch sends nothing.
    await expect(page.locator('[aria-label="Tên kênh"] input')).toHaveValue("Typed channel");
    await expect(page.locator('[aria-label="URL webhook"] input')).toHaveValue("https://typed/hook");
    await expect(page.getByRole("button", { name: "Tạo kênh" })).toBeEnabled();
    await page.screenshot({ path: "test-results/i18n8-dialog-channel-vi-900.png" });
    await new Promise((done) => setTimeout(done, 300));
    expect(log.calls.length, "API calls during switch").toBe(callsBefore);
  } finally {
    await context.close();
  }
});

test("I18N-8 channel test failure rederives with exactly one POST per attempt", async ({
  browser,
}) => {
  const log: ApiLog = { calls: [] };
  const failWith = (status: number) => ({
    [`POST /notification-channels/ch1/test`]: () => ({ status, body: { message: "" } }),
  });
  const { context, page } = await openPage(
    browser,
    "/settings/notifications",
    "en",
    900,
    log,
    failWith(403),
  );
  try {
    await expect(page.getByText("Deploy alerts")).toBeVisible();
    await page.getByRole("button", { name: "Send test" }).click();
    await expect(page.locator('[data-test-channel-result]')).toContainText(
      "Test failed: Your team role does not allow this action.",
    );
    const posts = (): number =>
      log.calls.filter((call) => call.startsWith("POST /notification-channels/ch1/test")).length;
    expect(posts()).toBe(1);

    await switchLocale(page, "vi");
    await expect(page.locator('[data-test-channel-result]')).toContainText(
      "Gửi thử thất bại: Vai trò nhóm của bạn không cho phép hành động này.",
    );
    expect(posts(), "resent on switch").toBe(1);
  } finally {
    await context.close();
  }
});

test("I18N-8 team rename 409 keeps the raw refusal identical in both locales", async ({
  browser,
}) => {
  const log: ApiLog = { calls: [] };
  const { context, page } = await openPage(browser, "/teams", "en", 900, log, {
    [`PATCH /teams/t-personal`]: () => ({ status: 409, body: { message: "teams: name taken" } }),
    [`PATCH /teams/t1`]: () => ({ status: 409, body: { message: "teams: name taken" } }),
  });
  try {
    await expect(page.getByRole("heading", { name: "Teams", exact: true })).toBeVisible();
    // The personal team is selected first; rename targets it.
    await page.getByRole("button", { name: "Rename" }).first().click();
    await page.locator('[aria-label="Team name"] input').fill("Taken");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByText("name taken")).toBeVisible();
    expect(page.locator('[aria-label="Team name"] input')).toHaveValue("Taken");

    await switchLocale(page, "vi");
    // Known refusal: identical raw text, typed value and open dialog kept.
    await expect(page.getByText("name taken")).toBeVisible();
    await expect(page.locator('[aria-label="Tên nhóm"] input')).toHaveValue("Taken");
    await expect(page.getByText("Đổi tên nhóm")).toBeVisible();
    await page.screenshot({ path: "test-results/i18n8-dialog-rename-vi-900.png" });
  } finally {
    await context.close();
  }
});

test("I18N-8 open delete confirm refreshes with zero DELETE sent", async ({
  browser,
}) => {
  const log: ApiLog = { calls: [] };
  const { context, page } = await openPage(
    browser,
    "/settings/notifications",
    "en",
    900,
    log,
  );
  try {
    await expect(page.getByText("Deploy alerts")).toBeVisible();
    await page
      .locator('[data-channel="Deploy alerts"]')
      .getByRole("button", { name: "Delete" })
      .click();
    await expect(
      page.getByText('Delete channel "Deploy alerts"?', { exact: false }),
    ).toBeVisible();

    await switchLocale(page, "vi");
    await expect(page.getByText(/Xóa kênh "Deploy alerts"/)).toBeVisible();
    await page.screenshot({ path: "test-results/i18n8-popconfirm-delete-vi-900.png" });
    expect(
      log.calls.filter((call) => call.startsWith("DELETE")),
      "DELETE sent on switch",
    ).toHaveLength(0);
  } finally {
    await context.close();
  }
});
