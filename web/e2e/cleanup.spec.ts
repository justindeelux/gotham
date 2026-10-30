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
    disk_usage: 0.3,
    container_count: 0,
    last_seen: new Date().toISOString(),
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
}

function scanNodeA() {
  return {
    server_id: nodeA,
    status: "ok",
    reason: "",
    items: [
      {
        id: "build-cache",
        category: "build_cache",
        tier: "safe",
        name: "Build cache",
        size_bytes: 1024,
        created_at: new Date(Date.now() - 3 * 86_400_000).toISOString(),
        reason: "unused build cache",
      },
      {
        id: "volume:gotham-orphan-data",
        category: "orphaned_volumes",
        tier: "dangerous",
        name: "gotham-orphan-data",
        size_bytes: 512,
        created_at: new Date(Date.now() - 30 * 86_400_000).toISOString(),
        reason: "not owned by any recorded resource",
      },
    ],
    notices: [],
  };
}

function scanNodeB() {
  return {
    server_id: nodeB,
    status: "skipped",
    reason: "busy",
    items: [],
    notices: [{ category: "journal", reason: "unsupported" }],
  };
}

async function installRoutes(
  page: Page,
  onClean: (_ids: string[]) => void,
): Promise<void> {
  await page.route("**/api/v1/servers", (route: Route) =>
    route.fulfill({
      json: { servers: [server(nodeA, "node-a"), server(nodeB, "node-b")] },
    }),
  );
  await page.route("**/api/v1/cleanup/scan", async (route: Route) => {
    const body = route.request().postDataJSON() as { server_ids: string[] };
    const node = body.server_ids[0] === nodeB ? scanNodeB() : scanNodeA();
    await route.fulfill({ json: { nodes: [node] } });
  });
  await page.route("**/api/v1/cleanup/clean", async (route: Route) => {
    const body = route.request().postDataJSON() as {
      server_id: string;
      ids: string[];
    };
    onClean(body.ids);
    await route.fulfill({
      json: {
        server_id: body.server_id,
        status: "ok",
        reason: "",
        run_id: "run-1",
        reclaimed_bytes: 512,
        items: [
          {
            id: "volume:gotham-orphan-data",
            category: "orphaned_volumes",
            name: "gotham-orphan-data",
            status: "removed",
            message: "",
            reclaimed_bytes: 512,
          },
        ],
      },
    });
  });
}

test("cleanup scans two nodes, requires confirmation for a volume and shows results", async ({
  page,
}) => {
  const cleaned: string[][] = [];
  await installRoutes(page, (ids) => cleaned.push(ids));

  await page.goto("/cleanup");
  await expect(page.getByText("node-a")).toBeVisible();
  await expect(page.getByText("node-b")).toBeVisible();

  await page.getByTestId("cleanup-scan").click();

  await expect(page.getByText("Skipped", { exact: true })).toBeVisible();
  await expect(page.getByText("A deployment is running")).toBeVisible();
  await expect(page.getByText(/Journal logs:/)).toBeVisible();
  await expect(page.getByText("Orphaned volumes")).toBeVisible();

  const volumeRow = page.locator(".cleanup-item").filter({ hasText: "gotham-orphan-data" });
  const volumeCheckbox = volumeRow.getByRole("checkbox");
  await expect(volumeCheckbox).not.toBeChecked();

  const buildRow = page.locator(".cleanup-item").filter({ hasText: "Build cache" });
  await expect(buildRow.getByRole("checkbox")).toBeChecked();

  await volumeCheckbox.check();
  await page.getByTestId("cleanup-clean").click();

  const understand = page.getByRole("checkbox", { name: /I understand/ });
  const deleteButton = page.getByRole("button", { name: "Delete", exact: true });
  await expect(understand).not.toBeChecked();
  await expect(deleteButton).toBeDisabled();
  await understand.check();
  await expect(deleteButton).toBeEnabled();
  await deleteButton.click();

  await expect(page.getByText("Total freed")).toBeVisible();
  await expect(page.getByText("gotham-orphan-data")).toBeVisible();
  expect(cleaned).toHaveLength(1);
  expect(cleaned[0]).toContain("volume:gotham-orphan-data");
});

test("cleanup has no horizontal overflow at 390 and 1280", async ({ page }) => {
  await installRoutes(page, () => undefined);
  await page.goto("/cleanup");
  await expect(page.getByText("node-a")).toBeVisible();
  await page.getByTestId("cleanup-scan").click();
  await expect(page.getByText("Orphaned volumes")).toBeVisible();

  for (const width of [390, 1280]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.locator(".cleanup-page")).toBeVisible();
    const overflow = await page.locator(".view").evaluate(
      (element) => element.scrollWidth - element.clientWidth,
    );
    expect(overflow, `no horizontal overflow at ${width}px`).toBeLessThanOrEqual(1);
  }
});
