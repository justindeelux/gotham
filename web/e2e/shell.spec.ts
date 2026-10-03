import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";
import {
  loadAccount,
  seedNodeAddress,
  storageStatePath,
  uniqueSuffix,
} from "./support";

/** stampSpaMarker tags the JS context so a full-page reload is detectable. */
async function stampSpaMarker(page: Page): Promise<void> {
  await page.evaluate(() => {
    (window as Window & { __spa?: number }).__spa = 1;
  });
}

/** spaMarkerSurvived is false only after a real document reload. */
async function spaMarkerSurvived(page: Page): Promise<boolean> {
  return page.evaluate(
    () => (window as Window & { __spa?: number }).__spa === 1,
  );
}

/**
 * FX-15a regression suite for the app shell, navigation and auth-screen states.
 *
 * The shell scenarios start already authenticated (session from global setup);
 * the auth scenario runs signed out so the public-only guard keeps it on
 * /login. Each check targets one finding and fails if the old behavior returns.
 */
test.describe("auth shell", () => {
  test("the auth screens do not overflow the mockup viewport height", async ({
    page,
  }) => {
    // B3-1: the stale `.auth-page` wrapper added a second 100vh minimum plus
    // padding inside `.auth-main`, so the 900px mockup scrolled by 64px.
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/login");

    const overflow = await page.evaluate(
      () => document.documentElement.scrollHeight - window.innerHeight,
    );
    expect(overflow).toBeLessThanOrEqual(0);
  });

  test("the disabled password-reset control states why it is inert", async ({
    page,
  }) => {
    // B4-17: the reason must be visible text, not a hover-only tooltip.
    await page.goto("/login");
    await expect(
      page.getByText("Password reset is not available yet.", { exact: true }),
    ).toBeVisible();
  });
});

test.describe("app shell", () => {
  test.use({ storageState: storageStatePath });

  test("the content region is a main landmark", async ({ page }) => {
    // B3-16.
    await page.goto("/dashboard");
    await expect(page.getByRole("main")).toBeVisible();
  });

  test("the mobile drawer owns focus, closes on Escape and returns focus", async ({
    page,
  }) => {
    // B3-4 (and B3-13's parent surface).
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
    // B3-13.
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
    // B3-17.
    await page.setViewportSize({ width: 390, height: 780 });
    await page.goto("/dashboard");
    await page.getByRole("button", { name: "Toggle navigation" }).click();
    await expect(page.locator(".backdrop")).toBeVisible();

    await page.setViewportSize({ width: 1280, height: 800 });
    await expect(page.locator(".backdrop")).toHaveCount(0);
  });

  test("the rail Add server button opens the wizard", async ({ page }) => {
    // B3-6: the rail add button must deep-link into the wizard, not the list.
    await page.goto("/dashboard");
    await page.getByRole("link", { name: "Add server", exact: true }).click();
    await expect(page).toHaveURL(/\/servers$/);
    await expect(page.getByRole("dialog")).toBeVisible();
  });

  test("a failed logout still redirects in-app from the topbar", async ({
    page,
  }) => {
    // B3-3: a failed revoke must not block navigation or degrade into a hard
    // document reload. `window.__spa` survives only a client-side redirect; a
    // `location.assign("/login")` (the 401 expire fallback) would wipe it.
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
    // B3-3 covers both sign-out handlers: the sidebar MeCard owns its own
    // logout path, so it needs the same protection and its own check.
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
    // B3-6: the avatar used to point at the list. Seed a node row (no agent
    // contact) and follow its rail link.
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
    // B4-16: with servers present, a filter that matches none must not reuse
    // the first-run "No servers yet" copy.
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
    // B4-16: the zero-servers branch must not reuse the filtered-miss copy.
    // Intercepting the list makes the branch deterministic regardless of rows
    // seeded by other specs on a shared database.
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

// The detail route is exercised with an unknown id: the page stays on the
// route and renders its error state, which is enough to prove the sidebar
// section stays highlighted. The resulting 404 resource log is expected.
test.describe("app shell detail routes", () => {
  test.use({
    storageState: storageStatePath,
    expectedConsoleErrors: ["Failed to load resource"],
  });

  test("a server detail URL keeps the Servers entry highlighted", async ({
    page,
  }) => {
    // B2-12 / B3-5.
    await page.goto("/servers/does-not-exist");
    await expect(
      page.locator('#app-nav a[href="/servers"]'),
    ).toHaveClass(/is-active/);
  });
});
