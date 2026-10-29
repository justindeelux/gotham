import { expect, test } from "./fixtures";
import { nestedURL, storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

const dbId = "11111111-1111-1111-1111-111111111111";
const scheduleId = "22222222-2222-2222-2222-222222222222";
const providerId = "33333333-3333-3333-3333-333333333333";
const projectId = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const environmentId = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const cron = "0 2 * * *";

const now = new Date().toISOString();
const inTwoHours = new Date(Date.now() + 2 * 3600 * 1000).toISOString();

const database = {
  id: dbId,
  name: "ui-e2e-backups-db",
  environment_id: environmentId,
  environment_name: "production",
  project_id: projectId,
  project_name: "shop",
  engine: "postgres",
  version: "16",
  status: "running",
  server_id: "99999999-9999-9999-9999-999999999999",
  server_name: "node",
  container_id: "container-1",
  public_port: 5432,
  volume: "volume-1",
  created_at: now,
  updated_at: now,
};

const schedule = {
  id: scheduleId,
  resource_type: "database",
  resource_id: dbId,
  cron,
  provider_id: "deleted-provider-id",
  enabled: true,
  next_run_at: inTwoHours,
  max_run_seconds: 7200,
  created_at: now,
  updated_at: now,
};

const provider = {
  id: providerId,
  name: "ui-e2e-provider",
  kind: "s3",
  endpoint: "https://s3.example.test",
  region: "auto",
  bucket: "backups",
  prefix: "logs/",
  builtin: false,
  has_credentials: true,
  created_at: now,
  updated_at: now,
};

const restores = [
  {
    id: "44444444-4444-4444-4444-444444444444",
    resource_type: "database",
    resource_id: dbId,
    backup_id: "66666666-6666-6666-6666-666666666666",
    status: "completed",
    created_at: now,
    finished_at: now,
  },
  {
    id: "55555555-5555-5555-5555-555555555555",
    resource_type: "database",
    resource_id: dbId,
    backup_id: "66666666-6666-6666-6666-666666666666",
    status: "failed",
    error: "restore container exited with code 1",
    created_at: now,
    finished_at: now,
  },
];

const resourceFilter = (url: URL): boolean =>
  url.searchParams.get("resource_type") === "database" &&
  url.searchParams.get("resource_id") === dbId;

test.describe("database backups", () => {
  test.beforeEach(async ({ page }) => {
    await page.route("**/api/v1/databases", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ databases: [database] }),
      }),
    );
    await page.route(`**/api/v1/databases/${dbId}`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ database }),
      }),
    );
    await page.route(`**/api/v1/databases/${dbId}/credentials`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          credentials: { username: "owner", password: "secret", database: "app" },
        }),
      }),
    );
    await page.route(/\/api\/v1\/backups\?/, (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.endsWith("/backups") && resourceFilter(url)) {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ backups: [] }),
        });
      }
      return route.fallback();
    });
    await page.route(/\/api\/v1\/backups\/restores\?/, (route) => {
      const url = new URL(route.request().url());
      if (resourceFilter(url)) {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ restores }),
        });
      }
      return route.fallback();
    });
    await page.route(/\/api\/v1\/backups\/schedules\?/, (route) => {
      const url = new URL(route.request().url());
      if (resourceFilter(url)) {
        return route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ schedules: [schedule] }),
        });
      }
      return route.fallback();
    });
    await page.route("**/api/v1/backups/providers", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ providers: [provider] }),
      }),
    );
    await page.route(`**/api/v1/databases/${dbId}/backup-protection`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          protection: {
            database_id: dbId,
            status: "not_configured",
            schedules: [],
          },
        }),
      }),
    );
    await page.route("**/api/v1/servers", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ servers: [] }),
      }),
    );

    await page.goto(nestedURL(projectId, environmentId, "databases", dbId));
    await page.locator(".n-tabs-tab").filter({ hasText: "Backups" }).click();
  });

  test("schedule toggle sends only cron and enabled, even with a deleted provider", async ({
    page,
    guardrails,
  }) => {
    const row = page.locator(".backup-row").filter({ hasText: cron });
    await expect(row).toBeVisible();
    await expect(row).toContainText("deleted target");

    const patch = page.waitForRequest(
      (request) =>
        request.url().includes(`/api/v1/backups/schedules/${scheduleId}`) &&
        request.method() === "PATCH",
    );
    await page.route(`**/api/v1/backups/schedules/${scheduleId}`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          schedule: { ...schedule, enabled: false },
        }),
      }),
    );
    await row.locator(".n-switch").click();

    expect((await patch).postDataJSON()).toEqual({ cron, enabled: false });

    expect(guardrails.apiFailures).toEqual([]);
  });

  test("provider management moved out of the database tab", async ({ page }) => {
    await expect(
      page.locator(".n-card-header").filter({ hasText: "Backup targets" }),
    ).toHaveCount(0);
    await expect(page.getByText("Open Backups → Storage")).toBeVisible();
  });

  test("restore history lists every durable restore run", async ({
    page,
    guardrails,
  }) => {
    await expect(
      page.locator(".backup-row").filter({ hasText: cron }),
    ).toContainText("in 2h");

    const history = page.locator(".n-card").filter({ hasText: "Restore history" });
    await expect(history).toBeVisible();
    await expect(history).toContainText("completed");
    await expect(history).toContainText("failed");
    await expect(history).toContainText("restore container exited with code 1");
    await expect(history).toContainText("backup 66666666");

    expect(guardrails.apiFailures).toEqual([]);
  });
});
