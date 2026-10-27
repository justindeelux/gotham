import { expect, test } from "./fixtures";
import { loadAccount, storageStatePath, uniqueSuffix } from "./support";

test.use({ storageState: storageStatePath });

const widths = [390, 480, 1280, 1366];

async function expectNoHorizontalOverflow(
  page: import("@playwright/test").Page,
  width: number,
): Promise<void> {
  await page.setViewportSize({ width, height: 900 });
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(1);
}

test.describe("monitoring rules", () => {
  test("saves, rejects invalid ordering and restores defaults", async ({ page }) => {
    await page.goto("/settings/monitoring");
    await expect(page.getByRole("heading", { name: "Monitoring rules" })).toBeVisible();

    const cpuCard = page
      .locator(".n-card")
      .filter({ has: page.getByRole("heading", { name: "CPU", exact: true }) })
      .first();
    await cpuCard.getByRole("textbox").nth(0).fill("92");
    await page.getByRole("button", { name: "Save rules" }).click();
    await expect(page.getByText("Monitoring rules saved")).toBeVisible();

    await page.reload();
    const reloadedCard = page
      .locator(".n-card")
      .filter({ has: page.getByRole("heading", { name: "CPU", exact: true }) })
      .first();
    await expect(reloadedCard.getByRole("textbox").nth(0)).toHaveValue("92");

    await reloadedCard.getByRole("textbox").nth(1).fill("92");
    await expect(page.getByText("Values must be ordered")).toBeVisible();
    await expect(page.getByRole("button", { name: "Save rules" })).toBeDisabled();

    await page.getByRole("button", { name: "Restore defaults" }).click();
    await expect(page.getByText("Monitoring rules restored")).toBeVisible();
    const restoredCard = page
      .locator(".n-card")
      .filter({ has: page.getByRole("heading", { name: "CPU", exact: true }) })
      .first();
    await expect(restoredCard.getByRole("textbox").nth(0)).toHaveValue("90");
  });

  test("fits 390, 480, 1280 and 1366px viewports", async ({ page }) => {
    await page.goto("/settings/monitoring");
    await expect(page.getByRole("heading", { name: "Monitoring rules" })).toBeVisible();
    for (const width of widths) {
      await expectNoHorizontalOverflow(page, width);
      await expect(page.getByRole("button", { name: "Save rules" })).toBeVisible();
    }
  });
});

test.describe("server host pressure", () => {
  test("shows the unknown state and fits every viewport", async ({ page, request }) => {
    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const created = await request.post("/api/v1/servers", {
      headers,
      data: {
        name: `pressure-${uniqueSuffix()}`,
        ip: "10.255.0.1",
        port: 22,
        ssh_user: "root",
      },
    });
    expect(created.status()).toBe(201);
    const { server } = (await created.json()) as { server: { id: string } };

    try {
      await page.goto(`/servers/${server.id}`);
      await expect(page.getByText("Host pressure")).toBeVisible();
      await expect(page.getByText("No observation")).toBeVisible();
      await expect(page.getByText("No active CPU, memory or filesystem pressure.")).toBeVisible();
      await expect(page.getByRole("button", { name: "Open metrics" })).toBeVisible();
      for (const width of widths) {
        await expectNoHorizontalOverflow(page, width);
      }
    } finally {
      await request.delete(`/api/v1/servers/${server.id}`, { headers });
    }
  });
});
