import type { Response } from "@playwright/test";

import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

function isServerListPoll(response: Response): boolean {
  return (
    response.request().method() === "GET" &&
    response.url().includes("/api/v1/servers") &&
    !response.url().includes("/api/v1/servers/")
  );
}

test("the Add server deep link opens the wizard and clears the flag", async ({
  page,
}) => {
  await page.goto("/servers?add=1");

  await expect(page.getByRole("dialog")).toBeVisible();
  await expect(page).toHaveURL(/\/servers$/);
});

test("the server rail keeps polling after leaving the dashboard", async ({
  page,
}) => {
  await page.goto("/dashboard");
  await page.waitForResponse(isServerListPoll);

  await page.getByRole("link", { name: "Projects", exact: true }).click();
  await expect(page).toHaveURL(/\/projects$/);

  await page.waitForResponse(isServerListPoll, { timeout: 15_000 });
  await page.waitForResponse(isServerListPoll, { timeout: 15_000 });
});
