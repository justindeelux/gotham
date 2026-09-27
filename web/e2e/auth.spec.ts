import { expect, test } from "./fixtures";
import { loadAccount } from "./support";

/**
 * QA-4.1b (a): the account is created through the real API in global setup;
 * this scenario proves the browser can sign in with it through the login form
 * and land on the dashboard without an error toast.
 */
test.describe("authentication", () => {
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

    // No server-side login failure rendered above the form.
    await expect(
      page.locator(".n-alert").filter({ hasText: /invalid credentials|Too many attempts/i }),
    ).toHaveCount(0);

    expect(guardrails.apiFailures).toEqual([]);
  });
});
