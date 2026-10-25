import { spawn } from "node:child_process";
import type { ChildProcess } from "node:child_process";
import net from "node:net";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Browser, BrowserContext, Page } from "@playwright/test";

/* global window:readonly, StorageEvent:readonly, setTimeout:readonly */
// globals are declared here (same pattern as form-feedback.spec.ts).

const testsDir = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(testsDir, "..");
const previewPort = 4273;
const previewUrl = `http://127.0.0.1:${previewPort}`;

const user = {
  id: "u1",
  email: "owner@example.com",
  created_at: "2026-01-01T00:00:00Z",
  role: "owner",
};

const discordChannel = {
  id: "ch1",
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

const shopApp = { id: "app-1", name: "Shop", primary_domain: "shop.example.com" };

interface ApiLog {
  calls: string[];
}

function json(status: number, body: unknown) {
  return { status, contentType: "application/json", body: JSON.stringify(body) };
}

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

async function switchLocale(page: Page, locale: "en" | "vi"): Promise<void> {
  await page.evaluate((lang: string) => {
    window.localStorage.setItem("gotham-locale", lang);
    window.dispatchEvent(
      new StorageEvent("storage", { key: "gotham-locale", newValue: lang }),
    );
  }, locale);
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


test("I18N-8 counts render exactly en/vi with zero API on switch", async ({
  browser,
}) => {
  const log: ApiLog = { calls: [] };
  const redirects = [
    {
      id: "r1",
      application_id: "app-1",
      source_domain: "go.example.com",
      target_domain: "checkout.example.com",
      code: 301,
      preserve_path: true,
      enabled: true,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-02T00:00:00Z",
    },
    {
      id: "r2",
      application_id: "app-1",
      source_domain: "old.example.com",
      target_domain: "new.example.com",
      code: 302,
      preserve_path: false,
      enabled: false,
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-02T00:00:00Z",
    },
  ];
  const redirectOverride = {
    ["GET /proxy/redirects"]: () => ({ status: 200, body: { redirects } }),
  };
  const domainsCtx = await openPage(
    browser,
    "/domains",
    "en",
    900,
    log,
    redirectOverride,
  );
  try {
    await expect(
      domainsCtx.page.getByRole("heading", { name: "Domains & SSL", exact: true }),
    ).toBeVisible();
    await domainsCtx.page.locator(".n-tabs-tab", { hasText: /^Redirects$/ }).click();
    await expect(
      domainsCtx.page.getByText(
        "2 rules · 1 enabled. GET answers the stored code; other methods " +
          "answer 308/307 so they keep their method.",
      ),
    ).toBeVisible();
    const domainCalls = log.calls.length;
    await switchLocale(domainsCtx.page, "vi");
    await expect(
      domainsCtx.page.getByText(
        "2 quy tắc · 1 đang bật. GET trả lời mã đã lưu; các phương thức " +
          "khác trả lời 308/307 để giữ phương thức.",
      ),
    ).toBeVisible();
    await new Promise((done) => setTimeout(done, 300));
    expect(log.calls.length, "API calls during domains switch").toBe(domainCalls);
  } finally {
    await domainsCtx.context.close();
  }
});

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
    await expect(page.getByLabel("Resource scope").getByText("All resources")).toBeVisible();

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
      "Test failed: You do not have permission for this action.",
    );
    const posts = (): number =>
      log.calls.filter((call) => call.startsWith("POST /notification-channels/ch1/test")).length;
    expect(posts()).toBe(1);

    await switchLocale(page, "vi");
    await expect(page.locator('[data-test-channel-result]')).toContainText(
      "Gửi thử thất bại: Bạn không có quyền thực hiện thao tác này.",
    );
    expect(posts(), "resent on switch").toBe(1);
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
