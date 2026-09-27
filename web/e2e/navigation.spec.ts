import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

// Start every navigation scenario already authenticated (session from global
// setup), so the walk does not spend an extra login against the rate limit.
test.use({ storageState: storageStatePath });

/**
 * QA-4.1b (b): walk the four core pages and prove each heading and empty
 * state renders, with no console errors and no failed /api/v1/* request. The
 * console/5xx guardrail is enforced by the fixture teardown; here we also
 * assert the 4xx bucket is empty for the whole walk.
 */
test.describe("core navigation", () => {
  test("walks Dashboard, Servers, Applications and Databases cleanly", async ({
    page,
    guardrails,
  }) => {
    await page.goto("/dashboard");
    await expect(
      page.getByRole("heading", { name: "Dashboard", level: 1 }),
    ).toBeVisible();

    await page.getByRole("link", { name: "Servers" }).click();
    await expect(page).toHaveURL(/\/servers$/);
    await expect(
      page.getByRole("heading", { name: "Servers", level: 1 }),
    ).toBeVisible();

    await page.getByRole("link", { name: "Applications" }).click();
    await expect(page).toHaveURL(/\/applications$/);
    await expect(
      page.getByRole("heading", { name: "Applications", level: 1 }),
    ).toBeVisible();
    // The page renders its content (provider list and application list cards)
    // rather than a blank shell. The list itself is asserted in the seeded
    // applications scenario.
    await expect(page.getByText("Source providers", { exact: true })).toBeVisible();

    await page.getByRole("link", { name: "Databases" }).click();
    await expect(page).toHaveURL(/\/databases$/);
    await expect(
      page.getByRole("heading", { name: "Databases", level: 1 }),
    ).toBeVisible();

    expect(
      guardrails.apiFailures,
      `failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});
