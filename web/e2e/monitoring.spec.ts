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

function ruleCard(page: import("@playwright/test").Page, title: string) {
  return page
    .locator(".n-card")
    .filter({ has: page.getByRole("heading", { name: title, exact: true }) })
    .first();
}

async function selectScope(
  page: import("@playwright/test").Page,
  label: string,
): Promise<void> {
  await page.locator(".scope-select").click();
  await page.locator(".n-base-select-option").filter({ hasText: label }).first().click();
}

async function createServer(
  request: import("@playwright/test").APIRequestContext,
  headers: Record<string, string>,
  prefix: string,
): Promise<{ id: string; name: string }> {
  const name = `${prefix}-${uniqueSuffix()}`;
  const created = await request.post("/api/v1/servers", {
    headers,
    data: { name, ip: "10.255.0.1", port: 22, ssh_user: "root" },
  });
  expect(created.status()).toBe(201);
  const { server } = (await created.json()) as { server: { id: string } };
  return { id: server.id, name };
}

test.describe("monitoring rules", () => {
  test("saves, rejects invalid ordering and restores defaults", async ({ page }) => {
    await page.goto("/settings/monitoring");
    await expect(page.getByRole("heading", { name: "Monitoring rules" })).toBeVisible();

    let writes = 0;
    page.on("request", (request) => {
      if (request.method() === "PUT" && request.url().includes("/monitoring/settings")) {
        writes++;
      }
    });
    const cpuCard = ruleCard(page, "CPU");
    await cpuCard.getByRole("textbox").nth(0).fill("92");
    await page.getByRole("button", { name: "Save rules" }).click();
    await expect(page.getByText("Monitoring rules saved")).toBeVisible();

    await page.reload();
    const reloadedCard = ruleCard(page, "CPU");
    await expect(reloadedCard.getByRole("textbox").nth(0)).toHaveValue("92");

    writes = 0;
    await reloadedCard.getByRole("textbox").nth(1).fill("92");
    await expect(page.getByText("Values must be ordered")).toBeVisible();
    await expect(page.getByRole("button", { name: "Save rules" })).toBeDisabled();
    await reloadedCard.getByRole("textbox").nth(1).press("Enter");
    await page.getByRole("button", { name: "Save rules" }).click({ force: true });
    await page.waitForTimeout(150);
    expect(writes).toBe(0);

    await page.getByRole("button", { name: "Restore defaults" }).click();
    await expect(page.getByText("Monitoring rules restored")).toBeVisible();
    const restoredCard = ruleCard(page, "CPU");
    await expect(restoredCard.getByRole("textbox").nth(0)).toHaveValue("90");
  });

  test("keeps scope loads isolated and disables Save on failure", async ({
    page,
    request,
  }) => {
    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const nodeA = await createServer(request, headers, "scope-a");
    const nodeB = await createServer(request, headers, "scope-b");
    try {
      const current = await request.get("/api/v1/monitoring/settings", { headers });
      const { settings } = (await current.json()) as {
        settings: { rules: Record<string, unknown> };
      };
      const rulesA = JSON.parse(JSON.stringify(settings.rules)) as {
        cpu: { warning: number; clear: number };
      };
      rulesA.cpu.warning = 80;
      rulesA.cpu.clear = 70;
      const savedA = await request.put("/api/v1/monitoring/settings", {
        headers,
        data: { node_id: nodeA.id, rules: rulesA },
      });
      expect(savedA.status()).toBe(200);

      let failB = false;
      await page.route("**/api/v1/monitoring/settings?*", async (route) => {
        const url = new URL(route.request().url());
        const nodeID = url.searchParams.get("node_id");
        if (nodeID === nodeA.id) {
          await new Promise((resolve) => setTimeout(resolve, 700));
        }
        if (failB && nodeID === nodeB.id) {
          await route.fulfill({
            status: 500,
            contentType: "application/json",
            body: JSON.stringify({ message: "boom" }),
          });
          return;
        }
        await route.continue();
      });

      await page.goto("/settings/monitoring");
      await expect(ruleCard(page, "CPU").getByRole("textbox").nth(0)).toHaveValue("90");
      await selectScope(page, nodeA.name);
      await selectScope(page, nodeB.name);
      await expect(ruleCard(page, "CPU").getByRole("textbox").nth(0)).toHaveValue("90");
      await page.waitForTimeout(900);
      await expect(ruleCard(page, "CPU").getByRole("textbox").nth(0)).toHaveValue("90");

      await ruleCard(page, "CPU").getByRole("textbox").nth(0).fill("93");
      await ruleCard(page, "Filesystem space").getByRole("textbox").nth(0).fill("91");
      await ruleCard(page, "Filesystem inodes").getByRole("textbox").nth(0).fill("91");
      await page.getByRole("button", { name: "Save rules" }).click();
      await expect(page.getByText("Monitoring rules saved")).toBeVisible();

      const savedB = await request.get(
        `/api/v1/monitoring/settings?node_id=${nodeB.id}`,
        { headers },
      );
      const bodyB = (await savedB.json()) as {
        settings: {
          rules: { cpu: { warning: number }; space: { warning: number }; inode: { warning: number } };
        };
      };
      expect(bodyB.settings.rules.cpu.warning).toBe(93);
      expect(bodyB.settings.rules.space.warning).toBe(91);
      expect(bodyB.settings.rules.inode.warning).toBe(91);

      failB = true;
      await selectScope(page, "Instance defaults");
      await selectScope(page, nodeB.name);
      await expect(page.getByText("boom")).toBeVisible();
      await expect(page.getByRole("button", { name: "Save rules" })).toBeDisabled();
    } finally {
      await request.delete(`/api/v1/servers/${nodeA.id}`, { headers });
      await request.delete(`/api/v1/servers/${nodeB.id}`, { headers });
    }
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
  test("shows a seeded firing incident and its filesystem", async ({ page, request }) => {
    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const node = await createServer(request, headers, "pressure-live");
    const observedAt = new Date().toISOString();
    await page.route(`**/api/v1/monitoring/nodes/${node.id}/status`, async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          status: {
            node_id: node.id,
            observed_at: observedAt,
            coverage: "fresh",
            active: [
              {
                detector: "filesystem_space",
                severity: "critical",
                lifecycle: "firing",
                resource_name: "/mnt/backup",
                evidence: "/mnt/backup 96.0% used, 80 bytes available of 2000 bytes, purpose=backup",
                first_observed_at: observedAt,
                confirmed_at: observedAt,
              },
            ],
            filesystems: [
              {
                path: "/mnt/backup",
                purpose: "backup",
                status: "ok",
                total_bytes: 2000,
                available_bytes: 80,
                used_percent: 96,
                inode_status: "ok",
                total_inodes: 1000,
                free_inodes: 800,
                inode_used_percent: 20,
              },
            ],
          },
        }),
      });
    });
    try {
      await page.goto(`/servers/${node.id}`);
      await expect(page.getByText("Host pressure")).toBeVisible();
      await expect(page.getByText("Filesystem space · /mnt/backup")).toBeVisible();
      await expect(page.getByText("Critical").first()).toBeVisible();
      await expect(page.getByText("96.0% used", { exact: false })).toBeVisible();
      const row = page
        .locator(".n-data-table-base-table-body .n-data-table-tbody tr")
        .filter({ hasText: "/mnt/backup" })
        .first();
      await expect(row).toBeVisible();
      await expect(row).toContainText("96%");
      await expect(row).toContainText("backup");
      await expect(page.getByText("No active CPU, memory or filesystem pressure.")).toHaveCount(0);
      for (const width of widths) {
        await expectNoHorizontalOverflow(page, width);
      }
    } finally {
      await request.delete(`/api/v1/servers/${node.id}`, { headers });
    }
  });

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

test.describe("filesystem forecast", () => {
  test("owner can toggle the forecast and no raw keys reach the page", async ({ page }) => {
    await page.goto("/settings/monitoring");
    await expect(page.getByRole("heading", { name: "Monitoring rules" })).toBeVisible();

    const spaceCard = ruleCard(page, "Filesystem space");
    const toggle = spaceCard.getByRole("switch", { name: "Forecast exhaustion" });
    await expect(toggle).toBeVisible();
    await expect(
      spaceCard.getByText(/needs 24 hours of history|24 giờ lịch sử/),
    ).toBeVisible();

    const text = await page.locator("body").innerText();
    expect(text).not.toMatch(/monitoring\.[A-Za-z]+\./);
    expect(text).not.toMatch(/undefined|NaN/);

    for (const width of widths) {
      await expectNoHorizontalOverflow(page, width);
    }
  });
});
