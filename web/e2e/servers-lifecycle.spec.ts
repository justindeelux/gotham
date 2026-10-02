import type { Response } from "@playwright/test";

import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

// FX-13b: the dashboard's "Add server" deep link must open the wizard, and the
// persistent server rail must keep polling after a page unmounts (B4-3, B4-7).
test.use({ storageState: storageStatePath });

/** isServerListPoll matches the rail's list poll, not a single-server read. */
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
  // The flag is consumed with a replace, so a refresh does not reopen it.
  await expect(page).toHaveURL(/\/servers$/);
});

test("the server rail keeps polling after leaving the dashboard", async ({
  page,
}) => {
  await page.goto("/dashboard");
  // The dashboard starts the shared poll; wait for one list poll to prove it.
  await page.waitForResponse(isServerListPoll);

  await page.getByRole("link", { name: "Applications" }).click();
  await expect(page).toHaveURL(/\/applications$/);

  // ApplicationsPage never starts the poll, and the rail owns the interval, so
  // two further list polls prove navigation did not freeze it (the round-0
  // regression had the outgoing dashboard stop the shared timer).
  await page.waitForResponse(isServerListPoll, { timeout: 15_000 });
  await page.waitForResponse(isServerListPoll, { timeout: 15_000 });
});
