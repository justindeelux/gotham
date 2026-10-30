import type { APIRequestContext, Page } from "@playwright/test";

import { expect, test } from "./fixtures";
import { loadAccount, seedNodeAddress, storageStatePath, uniqueSuffix } from "./support";

test.use({ storageState: storageStatePath });

function authHeaders(): Record<string, string> {
  return { Authorization: `Bearer ${loadAccount().accessToken}` };
}

async function seedServer(
  request: APIRequestContext,
  name: string,
): Promise<{ id: string; name: string }> {
  const response = await request.post("/api/v1/servers", {
    headers: authHeaders(),
    data: { name, ip: seedNodeAddress, ssh_user: "root" },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { server } = (await response.json()) as { server: { id: string; name: string } };
  return server;
}

function hostState(hostname: string) {
  return {
    capability: "ready",
    reason: "",
    repair: false,
    network: {
      dns_servers: ["1.1.1.1"],
      ipv4: { mode: "dhcp", address: "", gateway: "" },
      ipv6: { enabled: false, mode: "dhcp", address: "", gateway: "" },
    },
    system: {
      hostname,
      ntp_enabled: true,
      ntp_servers: [],
      timezone: "Europe/Berlin",
    },
    pending: null,
  };
}

async function openSettingsTab(page: Page): Promise<void> {
  await page.locator(".n-tabs-tab").filter({ hasText: "Node settings" }).click();
}

test("Node settings is read-only with a plain reason when the agent is unreachable", async ({
  page,
  request,
}) => {
  const node = await seedServer(request, `ui-e2e-host-offline-${uniqueSuffix()}`);
  await page.route(`**/api/v1/servers/${node.id}/host`, (route) =>
    route.fulfill({
      json: { host: { ...hostState("gotham-offline"), capability: "offline" } },
    }),
  );

  await page.goto(`/servers/${node.id}`);
  await openSettingsTab(page);

  await expect(page.getByText("agent is not reachable")).toBeVisible();
  await expect(page.getByText("Install host helper")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Apply network changes" })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Save system settings" })).toBeDisabled();
});

test("Node settings puts Network and System side by side at 1280px and stacks them at 390px", async ({
  page,
  request,
}) => {
  const node = await seedServer(request, `ui-e2e-host-layout-${uniqueSuffix()}`);
  await page.route(`**/api/v1/servers/${node.id}/host`, (route) =>
    route.fulfill({ json: { host: hostState("layout-node") } }),
  );

  const cards = page.locator(".settings-grid > .n-card");

  for (const width of [769, 900, 1024, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(`/servers/${node.id}`);
    await openSettingsTab(page);
    await expect(cards).toHaveCount(2);
    const boxes = await cards.evaluateAll((items) =>
      items.map((item) => {
        const box = item.getBoundingClientRect();
        return {
          x: box.x,
          y: box.y,
          width: box.width,
          height: box.height,
          scrollWidth: item.scrollWidth,
          clientWidth: item.clientWidth,
        };
      }),
    );
    expect(Math.abs(boxes[0].y - boxes[1].y), `cards share a row at ${width}px`).toBeLessThanOrEqual(1);
    expect(Math.abs(boxes[0].width - boxes[1].width), `cards are equal width at ${width}px`).toBeLessThanOrEqual(1);
    expect(Math.abs(boxes[0].height - boxes[1].height), `cards share a row height at ${width}px`).toBeLessThanOrEqual(1);
    expect(boxes[1].x, `System sits right of Network at ${width}px`).toBeGreaterThan(boxes[0].x);
    for (const box of boxes) {
      expect(box.scrollWidth, `no overflow inside a card at ${width}px`).toBeLessThanOrEqual(box.clientWidth + 1);
    }
  }

  const systemCard = await cards.nth(1).boundingBox();
  const systemSave = await cards.nth(1).locator(".n-button").first().boundingBox();
  if (systemCard === null || systemSave === null) {
    throw new Error("expected a System card and its save button");
  }
  expect(systemSave.y - systemCard.y, "System keeps its save button at the card bottom").toBeGreaterThan(
    systemCard.height / 2,
  );
  expect(systemSave.y + systemSave.height).toBeLessThanOrEqual(systemCard.y + systemCard.height);

  await page.setViewportSize({ width: 390, height: 900 });
  await page.goto(`/servers/${node.id}`);
  await openSettingsTab(page);
  await expect(cards).toHaveCount(2);
  const stacked = await cards.evaluateAll((items) =>
    items.map((item) => {
      const box = item.getBoundingClientRect();
      return { x: box.x, y: box.y, height: box.height };
    }),
  );
  expect(stacked[1].y, "System stacks under Network at 390px").toBeGreaterThanOrEqual(
    stacked[0].y + stacked[0].height - 1,
  );
  expect(Math.abs(stacked[0].x - stacked[1].x)).toBeLessThanOrEqual(1);
});

test("Node settings reads and saves only that server", async ({ page, request }) => {
  const suffix = uniqueSuffix();
  const first = await seedServer(request, `ui-e2e-host-a-${suffix}`);
  const second = await seedServer(request, `ui-e2e-host-b-${suffix}`);
  await page.route(`**/api/v1/servers/${first.id}/host`, (route) =>
    route.fulfill({ json: { host: hostState("node-a") } }),
  );
  await page.route(`**/api/v1/servers/${second.id}/host`, (route) =>
    route.fulfill({ json: { host: hostState("node-b") } }),
  );

  await page.goto(`/servers/${first.id}`);
  await openSettingsTab(page);
  await expect(page.getByPlaceholder("gotham-1")).toHaveValue("node-a");

  await page.goto(`/servers/${second.id}`);
  await openSettingsTab(page);
  await expect(page.getByPlaceholder("gotham-1")).toHaveValue("node-b");

  let savedBody: Record<string, unknown> | null = null;
  await page.route(`**/api/v1/servers/${second.id}/host/network`, (route) => {
    savedBody = route.request().postDataJSON() as Record<string, unknown>;
    return route.fulfill({
      json: {
        host: {
          ...hostState("node-b"),
          network: savedBody,
          pending: {
            previous: hostState("node-b").network,
            proposed: savedBody,
            deadline: new Date(Date.now() + 120_000).toISOString(),
          },
        },
      },
    });
  });

  await page.getByPlaceholder("1.1.1.1").fill("9.9.9.9");
  await page.getByRole("button", { name: "Apply network changes" }).click();

  expect(savedBody?.dns_servers).toEqual(["9.9.9.9"]);
  await expect(page.getByText("Keep these network changes?")).toBeVisible();
  await expect(page.getByText("A network change is awaiting confirmation.")).toBeVisible();
});
