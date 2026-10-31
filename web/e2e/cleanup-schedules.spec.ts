import type { Page, Route } from "@playwright/test";

import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

const nodeA = "11111111-1111-4111-8111-111111111111";
const scheduleID = "33333333-3333-4333-8333-333333333333";
const runID = "44444444-4444-4444-8444-444444444444";

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

function categories() {
  return {
    categories: [
      { key: "build_cache", tier: "safe", manual_only: false },
      { key: "dangling_images", tier: "safe", manual_only: false },
      { key: "stopped_containers", tier: "safe", manual_only: false },
      { key: "journal", tier: "safe", manual_only: false },
      { key: "package_cache", tier: "safe", manual_only: false },
      { key: "unused_images", tier: "careful", manual_only: false },
      { key: "orphaned_volumes", tier: "dangerous", manual_only: true },
    ],
  };
}

function schedule() {
  return {
    id: scheduleID,
    name: "nightly",
    server_ids: [nodeA],
    categories: ["build_cache"],
    cron: "0 3 * * *",
    min_age_hours: 24,
    enabled: true,
    next_run_at: new Date(Date.now() + 86_400_000).toISOString(),
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
}

function scanNode() {
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
    ],
    notices: [],
  };
}

async function installRoutes(page: Page): Promise<{
  created: Record<string, unknown>[];
  exclusions: Record<string, unknown>[];
}> {
  const created: Record<string, unknown>[] = [];
  const exclusions: Record<string, unknown>[] = [];

  await page.route("**/api/v1/servers", (route: Route) =>
    route.fulfill({ json: { servers: [server(nodeA, "node-a")] } }),
  );
  await page.route("**/api/v1/cleanup/categories", (route: Route) =>
    route.fulfill({ json: categories() }),
  );
  await page.route("**/api/v1/cleanup/threshold", (route: Route) =>
    route.fulfill({
      json: {
        threshold: {
          enabled: false,
          percent: 85,
          categories: ["build_cache"],
          min_age_hours: 24,
          cooldown_hours: 6,
        },
      },
    }),
  );
  await page.route("**/api/v1/cleanup/cron/next**", (route: Route) =>
    route.fulfill({
      json: { next_run_at: new Date(Date.now() + 86_400_000).toISOString() },
    }),
  );
  await page.route("**/api/v1/cleanup/schedules", async (route: Route) => {
    if (route.request().method() === "POST") {
      const body = route.request().postDataJSON() as Record<string, unknown>;
      created.push(body);
      await route.fulfill({ status: 201, json: { schedule: { ...schedule(), ...body } } });
      return;
    }
    await route.fulfill({ json: { schedules: created.length > 0 ? [schedule()] : [] } });
  });
  await page.route("**/api/v1/cleanup/runs?**", (route: Route) =>
    route.fulfill({
      json: {
        runs: [
          {
            id: runID,
            server_id: nodeA,
            trigger: "schedule",
            schedule_name: "nightly",
            started_at: new Date().toISOString(),
            finished_at: new Date().toISOString(),
            reclaimed_bytes: 2048,
            status: "ok",
            categories: { build_cache: 2048 },
          },
        ],
      },
    }),
  );
  await page.route("**/api/v1/cleanup/runs", (route: Route) =>
    route.fulfill({
      json: {
        runs: [
          {
            id: runID,
            server_id: nodeA,
            trigger: "schedule",
            schedule_name: "nightly",
            started_at: new Date().toISOString(),
            finished_at: new Date().toISOString(),
            reclaimed_bytes: 2048,
            status: "ok",
            categories: { build_cache: 2048 },
          },
        ],
      },
    }),
  );
  await page.route(`**/api/v1/cleanup/runs/${runID}`, (route: Route) =>
    route.fulfill({
      json: {
        run: {
          id: runID,
          server_id: nodeA,
          trigger: "schedule",
          schedule_name: "nightly",
          started_at: new Date().toISOString(),
          finished_at: new Date().toISOString(),
          reclaimed_bytes: 2048,
          status: "ok",
          categories: { build_cache: 2048 },
          items: [
            {
              id: "build-cache",
              category: "build_cache",
              name: "Build cache",
              status: "removed",
              message: "",
              reclaimed_bytes: 2048,
            },
          ],
        },
      },
    }),
  );
  await page.route("**/api/v1/cleanup/exclusions", async (route: Route) => {
    if (route.request().method() === "POST") {
      const body = route.request().postDataJSON() as Record<string, unknown>;
      exclusions.push(body);
      await route.fulfill({
        status: 201,
        json: {
          exclusion: {
            id: "ex-1",
            server_id: body.server_id,
            category: body.category,
            match: body.match,
            note: "",
            created_at: new Date().toISOString(),
          },
        },
      });
      return;
    }
    await route.fulfill({ json: { exclusions } });
  });
  await page.route("**/api/v1/cleanup/scan", (route: Route) =>
    route.fulfill({ json: { nodes: [scanNode()] } }),
  );
  return { created, exclusions };
}

test("creates a cleanup schedule and lists it", async ({ page }) => {
  const { created } = await installRoutes(page);
  await page.goto("/cleanup?tab=schedules");
  await expect(page.getByText("No schedules yet.")).toBeVisible();

  await page.getByTestId("cleanup-schedule-new").click();
  await page.getByPlaceholder("Nightly node cleanup").fill("nightly");
  await page.getByRole("checkbox", { name: "node-a" }).check();
  await page.getByTestId("cleanup-schedule-save").click();

  await expect(page.getByTestId("cleanup-schedule-nightly")).toBeVisible();
  expect(created).toHaveLength(1);
  expect(created[0]).toMatchObject({
    name: "nightly",
    server_ids: [nodeA],
    cron: "0 3 * * *",
  });
});

test("history lists a run and expands its items", async ({ page }) => {
  await installRoutes(page);
  await page.goto("/cleanup?tab=history");
  await expect(page.getByText("All servers")).toBeVisible();
  await expect(page.getByTestId(`cleanup-run-${runID}`)).toBeVisible();
  await page.getByTestId(`cleanup-run-${runID}`).getByRole("button").click();
  await expect(page.getByText("Build cache")).toBeVisible();
});

test("a scan row can be excluded and leaves the results", async ({ page }) => {
  const { exclusions } = await installRoutes(page);
  await page.goto("/cleanup?tab=scan");
  await page.getByTestId("cleanup-scan").click();
  const row = page.locator(".cleanup-item").filter({ hasText: "Build cache" });
  await expect(row).toBeVisible();

  await page.getByTestId("cleanup-exclude-build-cache").click();
  await expect(row).toHaveCount(0);
  expect(exclusions[0]).toMatchObject({
    server_id: nodeA,
    category: "build_cache",
    match: "Build cache",
  });

  await page.locator(".n-tabs-tab").filter({ hasText: "Exclusions" }).click();
  await expect(page.getByTestId("cleanup-exclusion-Build cache")).toBeVisible();
});

test("cleanup tabs keep their selection across a reload", async ({ page }) => {
  await installRoutes(page);
  await page.goto("/cleanup?tab=schedules");
  await expect(page.getByTestId("cleanup-schedule-new")).toBeVisible();
  await page.reload();
  await expect(page.getByTestId("cleanup-schedule-new")).toBeVisible();
});

test("schedule and history tabs have no horizontal overflow", async ({ page }) => {
  await installRoutes(page);
  await page.goto("/cleanup?tab=schedules");
  await expect(page.getByTestId("cleanup-schedule-new")).toBeVisible();
  await page.getByTestId("cleanup-schedule-new").click();
  await expect(page.getByPlaceholder("Nightly node cleanup")).toBeVisible();
  await page.getByRole("button", { name: "Cancel" }).click();

  for (const width of [390, 480, 1280, 1366]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.locator(".cleanup-page")).toBeVisible();
    const overflow = await page.locator(".view").evaluate(
      (element) => element.scrollWidth - element.clientWidth,
    );
    expect(overflow, `no horizontal overflow at ${width}px`).toBeLessThanOrEqual(1);
  }
});
