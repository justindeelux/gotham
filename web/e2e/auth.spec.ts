import { expect, test } from "./fixtures";
import { loadAccount } from "./support";

test.describe("authentication", () => {
  test("root path lands on the sign-in form with a create-account tab", async ({
    page,
  }) => {
    await page.goto("/");

    await expect(page).toHaveURL(/\/login$/);
    await expect(
      page.getByRole("button", { name: "Sign in", exact: true }),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Create account", exact: true }),
    ).toBeVisible();
  });

  test("signs in through the login form and renders the dashboard", async ({
    page,
    guardrails,
  }) => {
    const account = loadAccount();

    await page.goto("/login");
    await expect(page).toHaveURL(/\/login$/);

    await page.getByPlaceholder("you@gotham.dev").fill(account.email);
    await page.getByPlaceholder("Your password").fill(account.password);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();

    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(
      page.getByRole("heading", { name: "Dashboard", level: 1 }),
    ).toBeVisible();

    await expect(
      page.locator(".n-alert").filter({ hasText: /invalid credentials|Too many attempts/i }),
    ).toHaveCount(0);

    expect(guardrails.apiFailures).toEqual([]);
  });
});
