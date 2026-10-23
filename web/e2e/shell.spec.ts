import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";
import {
  loadAccount,
  seedNodeAddress,
  storageStatePath,
  uniqueSuffix,
} from "./support";

async function stampSpaMarker(page: Page): Promise<void> {
  await page.evaluate(() => {
    (window as Window & { __spa?: number }).__spa = 1;
  });
}

async function spaMarkerSurvived(page: Page): Promise<boolean> {
  return page.evaluate(
    () => (window as Window & { __spa?: number }).__spa === 1,
  );
}

test.describe("auth shell", () => {
  test("the auth screens do not overflow the mockup viewport height", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/login");

    const overflow = await page.evaluate(
      () => document.documentElement.scrollHeight - window.innerHeight,
    );
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test("the password-reset guidance tells the user what to do", async ({
    page,
  }) => {
    await page.goto("/login");
    await expect(
      page.getByText("Forgot your password? Contact your admin to reset it.", {
        exact: true,
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /forgot password/i }),
    ).toHaveCount(0);
  });
});

test.describe("app shell", () => {
  test.use({ storageState: storageStatePath });

  test("the content region is a main landmark", async ({ page }) => {
    await page.goto("/dashboard");
    await expect(page.getByRole("main")).toBeVisible();
  });

  test("the mobile drawer owns focus, closes on Escape and returns focus", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 780 });
    await page.goto("/dashboard");

    const toggle = page.getByRole("button", { name: "Toggle navigation" });
    await expect(toggle).toHaveAttribute("aria-expanded", "false");

    await toggle.click();
    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    await expect(page.locator("#app-nav")).toBeVisible();
    await expect(
      page.locator("#app-nav a.nav-item").first(),
    ).toBeFocused();

    await page.keyboard.press("Escape");
    await expect(toggle).toHaveAttribute("aria-expanded", "false");
    await expect(toggle).toBeFocused();
    await expect(page.locator(".backdrop")).toHaveCount(0);
  });

  test("the phone drawer entry is a 44px touch target", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 780 });
    await page.goto("/dashboard");
    await page.getByRole("button", { name: "Toggle navigation" }).click();

    const height = await page
      .locator("#app-nav a.nav-item")
      .first()
      .evaluate((element) => element.getBoundingClientRect().height);
    expect(height).toBeGreaterThanOrEqual(44);
  });

  test("growing past the breakpoint unmounts the drawer backdrop", async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 780 });
    await page.goto("/dashboard");
    await page.getByRole("button", { name: "Toggle navigation" }).click();
    await expect(page.locator(".backdrop")).toBeVisible();

    await page.setViewportSize({ width: 1280, height: 800 });
    await expect(page.locator(".backdrop")).toHaveCount(0);
  });

  test("the rail Add server button opens the wizard", async ({ page }) => {
    await page.goto("/dashboard");
    await page.getByRole("link", { name: "Add server", exact: true }).click();
    await expect(page).toHaveURL(/\/servers$/);
    await expect(page.getByRole("dialog")).toBeVisible();
  });

  test("a failed logout still redirects in-app from the topbar", async ({
    page,
  }) => {
    await page.route("**/api/v1/auth/logout", (route) =>
      route.fulfill({ status: 400, contentType: "application/json", body: "{}" }),
    );
    await page.goto("/dashboard");
    await stampSpaMarker(page);

    await page.locator(".topbar").getByRole("button", { name: "Account" }).click();
    await page.getByText("Sign out", { exact: true }).click();
    await expect(page).toHaveURL(/\/login$/);
    expect(await spaMarkerSurvived(page)).toBe(true);
  });

  test("a failed logout still redirects in-app from the sidebar card", async ({
    page,
  }) => {
    await page.route("**/api/v1/auth/logout", (route) =>
      route.fulfill({ status: 400, contentType: "application/json", body: "{}" }),
    );
    await page.goto("/dashboard");
    await stampSpaMarker(page);

    await page
      .locator(".sidebar-foot")
      .getByRole("button", { name: "Account" })
      .click();
    await page.getByText("Sign out", { exact: true }).click();
    await expect(page).toHaveURL(/\/login$/);
    expect(await spaMarkerSurvived(page)).toBe(true);
  });

  test("a rail avatar links to that server's detail route", async ({
    page,
    request,
  }) => {
    const account = loadAccount();
    const name = `ui-e2e-node-${uniqueSuffix()}`;
    const created = await request.post("/api/v1/servers", {
      headers: { Authorization: `Bearer ${account.accessToken}` },
      data: { name, ip: seedNodeAddress, ssh_user: "root" },
    });
    expect(created.status(), await created.text()).toBe(201);
    const { server } = (await created.json()) as { server: { id: string } };

    await page.goto("/dashboard");
    const avatar = page.getByRole("link", { name: new RegExp(name) });
    await expect(avatar).toHaveAttribute(
      "href",
      new RegExp(`/servers/${server.id}$`),
    );
    await avatar.click();
    await expect(page).toHaveURL(new RegExp(`/servers/${server.id}$`));
  });

  test("servers filter with no matches says so instead of claiming empty", async ({
    page,
    request,
  }) => {
    const account = loadAccount();
    const created = await request.post("/api/v1/servers", {
      headers: { Authorization: `Bearer ${account.accessToken}` },
      data: {
        name: `ui-e2e-node-${uniqueSuffix()}`,
        ip: seedNodeAddress,
        ssh_user: "root",
      },
    });
    expect(created.status(), await created.text()).toBe(201);

    await page.goto("/servers");
    await page.getByPlaceholder("Search by name, IP, OS…").fill("no-such-node-zzzz");
    await expect(
      page.getByText("No nodes match the current filters", { exact: true }),
    ).toBeVisible();
  });

  test("servers first-run state says no servers yet", async ({ page }) => {
    await page.route("**/api/v1/servers", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ servers: [] }),
      }),
    );
    await page.goto("/servers");
    await expect(page.getByText("No servers yet", { exact: true })).toBeVisible();
    await expect(
      page.getByText("Add your first server over SSH to begin.", { exact: true }),
    ).toBeVisible();
  });
});

test.describe("app shell detail routes", () => {
  test.use({
    storageState: storageStatePath,
    expectedConsoleErrors: ["Failed to load resource"],
  });

  test("a server detail URL keeps the Servers entry highlighted", async ({
    page,
  }) => {
    await page.goto("/servers/does-not-exist");
    await expect(
      page.locator('#app-nav a[href="/servers"]'),
    ).toHaveClass(/is-active/);
  });
});
