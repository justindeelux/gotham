import { expect, test } from "./fixtures";
import { loadAccount } from "./support";

/**
 * QA-4.1b (a): the account is created through the real API in global setup;
 * this scenario proves the browser can sign in with it through the login form
 * and land on the dashboard without an error toast.
 */
test.describe("authentication", () => {
  // QA-4.1b regression: the bare root used to render the auth shell with an
  // empty router-view, so `http://<host>:8000/` showed no sign-in form and no
  // way to reach the create-account tab. Assert the root lands on the form.
  test("root path lands on the sign-in form with a create-account tab", async ({
    page,
  }) => {
    await page.goto("/");

    await expect(page).toHaveURL(/\/login$/);
    await expect(
      page.getByRole("button", { name: "Sign in", exact: true }),
    ).toBeVisible();
    // The create-account control is a RouterLink with an explicit role="tab",
    // so it must be located by its tab role, not as a link.
    await expect(
      page.getByRole("tab", { name: "Create account", exact: true }),
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

    // No server-side login failure rendered above the form.
    await expect(
      page.locator(".n-alert").filter({ hasText: /invalid credentials|Too many attempts/i }),
    ).toHaveCount(0);

    expect(guardrails.apiFailures).toEqual([]);
  });
});
