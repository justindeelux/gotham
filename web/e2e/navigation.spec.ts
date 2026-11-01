import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

test.describe("core navigation", () => {
  test("walks Dashboard, Servers, Projects and Domains cleanly", async ({
    page,
    guardrails,
  }) => {
    await page.goto("/dashboard");
    await expect(
      page.getByRole("heading", { name: "Dashboard", level: 1 }),
    ).toBeVisible();

    const sidebar = page.locator("#app-nav");
    await sidebar.getByRole("link", { name: "Servers" }).click();
    await expect(page).toHaveURL(/\/servers$/);
    await expect(
      page.getByRole("heading", { name: "Servers", level: 1 }),
    ).toBeVisible();

    await sidebar.getByRole("link", { name: "Projects" }).click();
    await expect(page).toHaveURL(/\/projects$/);
    await expect(
      page.getByRole("heading", { name: "Projects", level: 1 }),
    ).toBeVisible();

    await sidebar.getByRole("link", { name: "Domains & SSL" }).click();
    await expect(page).toHaveURL(/\/domains$/);
    await expect(
      page.getByRole("heading", { name: "Domains & SSL", level: 1 }),
    ).toBeVisible();

    for (const flat of ["/applications", "/services", "/databases"]) {
      await page.goto(flat);
      await expect(page).toHaveURL(/\/projects$/);
      await expect(
        page.getByRole("heading", { name: "Projects", level: 1 }),
      ).toBeVisible();
    }

    expect(
      guardrails.apiFailures,
      `failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });

  test("expands grouped submenus and highlights the active child", async ({
    page,
    guardrails,
  }) => {
    await page.goto("/dashboard");
    const sidebar = page.locator("#app-nav");
    const security = sidebar.getByRole("button", { name: "Security" });

    await expect(security).toHaveAttribute("aria-expanded", "false");
    await security.click();
    await expect(security).toHaveAttribute("aria-expanded", "true");

    await sidebar.getByRole("link", { name: "API tokens" }).click();
    await expect(page).toHaveURL(/\/security\/api-tokens$/);
    await expect(security).toHaveAttribute("aria-expanded", "true");
    await expect(
      sidebar.locator('a[href="/security/api-tokens"]'),
    ).toHaveClass(/is-active/);

    await security.focus();
    await page.keyboard.press("Enter");
    await expect(security).toHaveAttribute("aria-expanded", "false");

    const integrations = sidebar.getByRole("button", {
      name: "Integrations",
    });
    await integrations.click();
    await sidebar.getByRole("link", { name: "Storages" }).click();
    await expect(page).toHaveURL(/\/integrations\/storages$/);
    await expect(
      page.getByRole("heading", { name: "Storages", level: 1 }),
    ).toBeVisible();
    await expect(
      sidebar.locator('a[href="/integrations/storages"]'),
    ).toHaveClass(/is-active/);

    expect(
      guardrails.apiFailures,
      `failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });

  test("opens the group that holds the active route", async ({ page }) => {
    await page.goto("/settings/general");
    const sidebar = page.locator("#app-nav");
    await expect(
      sidebar.getByRole("button", { name: "Settings" }),
    ).toHaveAttribute("aria-expanded", "true");
    await expect(
      sidebar.locator('a[href="/settings/general"]'),
    ).toHaveClass(/is-active/);
  });

  test("old settings URLs redirect to the grouped routes", async ({ page }) => {
    const cases = [
      ["/settings", /\/notifications$/, "Notifications"],
      ["/settings/notifications", /\/notifications$/, "Notifications"],
      ["/settings/git-sources", /\/integrations\/git-sources$/, "Git sources"],
      ["/settings/mcp", /\/integrations\/mcp$/, "MCP"],
      ["/settings/instance", /\/settings\/general$/, "General"],
      ["/settings/updates", /\/updates$/, "Updates"],
    ] as const;

    for (const [path, url, heading] of cases) {
      await page.goto(path);
      await expect(page, path).toHaveURL(url);
      await expect(
        page.getByRole("heading", { name: heading, level: 1 }),
        path,
      ).toBeVisible();
    }
  });

  test("submenus work inside the mobile drawer", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 780 });
    await page.goto("/dashboard");
    await page.getByRole("button", { name: "Toggle navigation" }).click();

    const sidebar = page.locator("#app-nav");
    const integrations = sidebar.getByRole("button", {
      name: "Integrations",
    });
    await expect(integrations).toHaveAttribute("aria-expanded", "false");
    await integrations.click();
    await expect(integrations).toHaveAttribute("aria-expanded", "true");

    await sidebar.getByRole("link", { name: "Storages" }).click();
    await expect(page).toHaveURL(/\/integrations\/storages$/);
    await expect(sidebar).toBeHidden();
  });
});
