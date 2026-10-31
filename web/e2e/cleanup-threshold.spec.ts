import type { Page, Route } from "@playwright/test";

import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

const nodeA = "11111111-1111-4111-8111-111111111111";
const nodeB = "22222222-2222-4222-8222-222222222222";

function server(id: string, name: string) {
  return {
    id,
    name,
    ip: "10.0.0.10",
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status: "ready",
    node_id: null,
    os: "Ubuntu 24.04",
    docker_version: "27.0.0",
    arch: "amd64",
    total_mem: 2048,
    total_disk: 40960,
    cpu_usage: 0.1,
    mem_usage: 0.2,
    disk_usage: 0.9,
    container_count: 0,
    last_seen: new Date().toISOString(),
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
}

function threshold() {
  return {
    enabled: false,
    percent: 85,
    categories: ["build_cache", "journal"],
    min_age_hours: 24,
    cooldown_hours: 6,
  };
}

function categories() {
  return {
    categories: [
      { key: "build_cache", tier: "safe", manual_only: false },
      { key: "journal", tier: "safe", manual_only: false },
      { key: "unused_images", tier: "careful", manual_only: false },
      { key: "orphaned_volumes", tier: "dangerous", manual_only: true },
    ],
  };
}

async function installRoutes(page: Page): Promise<Record<string, unknown>[]> {
  const saved: Record<string, unknown>[] = [];
  await page.route("**/api/v1/servers", (route: Route) =>
    route.fulfill({ json: { servers: [server(nodeA, "node-a"), server(nodeB, "node-b")] } }),
  );
  await page.route("**/api/v1/cleanup/categories", (route: Route) =>
    route.fulfill({ json: categories() }),
  );
  await page.route("**/api/v1/cleanup/schedules", (route: Route) =>
    route.fulfill({ json: { schedules: [] } }),
  );
  await page.route("**/api/v1/cleanup/threshold", async (route: Route) => {
    if (route.request().method() === "PUT") {
      const body = route.request().postDataJSON() as Record<string, unknown>;
      saved.push(body);
      await route.fulfill({ json: { threshold: { ...threshold(), ...body } } });
      return;
    }
    await route.fulfill({ json: { threshold: saved.length > 0 ? saved[0] : threshold() } });
  });
  await page.route("**/api/v1/cleanup/scan", (route: Route) =>
    route.fulfill({
      json: {
        nodes: [{ server_id: nodeB, status: "ok", reason: "", items: [], notices: [] }],
      },
    }),
  );
  return saved;
}

test("saves the disk-pressure threshold", async ({ page }) => {
  const saved = await installRoutes(page);
  await page.goto("/cleanup?tab=schedules");

  const percent = page.getByTestId("cleanup-threshold-percent").locator("input");
  await expect(percent).toHaveValue("85");
  await page.getByTestId("cleanup-threshold-enabled").click();
  await percent.fill("90");
  await page.getByTestId("cleanup-threshold-save").click();

  await expect(page.getByText("Threshold saved.")).toBeVisible();
  expect(saved).toHaveLength(1);
  expect(saved[0]).toMatchObject({ enabled: true, percent: 90, cooldown_hours: 6 });
});

test("a server deep link preselects only that node", async ({ page }) => {
  await installRoutes(page);
  await page.goto(`/cleanup?server=${nodeB}`);

  await expect(page.getByRole("checkbox", { name: "node-b" })).toBeChecked();
  await expect(page.getByRole("checkbox", { name: "node-a" })).not.toBeChecked();
});
