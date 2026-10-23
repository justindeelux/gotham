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
});
