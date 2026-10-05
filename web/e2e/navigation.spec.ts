import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

// Start every navigation scenario already authenticated (session from global
// setup), so the walk does not spend an extra login against the rate limit.
test.use({ storageState: storageStatePath });

/**
 * QA-4.1b (b) + FE-6.1: walk the core pages and prove each heading and
 * empty state renders, with no console errors and no failed /api/v1/* request.
 * The console/5xx guardrail is enforced by the fixture teardown; here we also
 * assert the 4xx bucket is empty for the whole walk.
 *
 * PE-4 (JUS-33): the sidebar links Projects instead of the flat Applications
 * and Databases entries. PE-5 (JUS-34) removed the flat routes: reaching them
 * by URL redirects to Projects, which the walk proves below.
 */
test.describe("core navigation", () => {
  test("walks Dashboard, Servers, Projects and Domains cleanly", async ({
    page,
    guardrails,
  }) => {
    await page.goto("/dashboard");
    await expect(
      page.getByRole("heading", { name: "Dashboard", level: 1 }),
    ).toBeVisible();

    // Sidebar-scoped: the dashboard renders its own links whose names contain
    // these labels ("View applications", "view all servers"), so a page-wide
    // non-exact lookup is a strict-mode violation once that data exists.
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

    // PE-5 removed the flat resource routes: the old URLs redirect once
    // to the Projects page instead of rendering.
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
