import { expect, test } from "./fixtures";
import { nestedURL, storageStatePath } from "./support";

// Every scenario starts from the session global setup created.
test.use({ storageState: storageStatePath });

const dbId = "11111111-1111-1111-1111-111111111111";
const scheduleId = "22222222-2222-2222-2222-222222222222";
const targetId = "33333333-3333-3333-3333-333333333333";
// The mocked database rides a fixed scope so the nested URL resolves; the
// breadcrumb reads the grouping fields, never the typed URL.
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

// The schedule points at a target that no longer exists: this is the D3-2
// case (deleted target must not block pausing the schedule).
const schedule = {
  id: scheduleId,
  database_id: dbId,
  cron,
  target_id: "deleted-target-id",
  enabled: true,
  next_run_at: inTwoHours,
  created_at: now,
  updated_at: now,
};

const target = {
  id: targetId,
  name: "ui-e2e-target",
  kind: "s3",
  endpoint: "https://s3.example.test",
  bucket: "backups",
  prefix: "logs/",
  has_credentials: true,
  created_at: now,
  updated_at: now,
};

const restores = [
  {
    id: "44444444-4444-4444-4444-444444444444",
    database_id: dbId,
    backup_id: "66666666-6666-6666-6666-666666666666",
    status: "completed",
    created_at: now,
    finished_at: now,
  },
  {
    id: "55555555-5555-5555-5555-555555555555",
    database_id: dbId,
    backup_id: "66666666-6666-6666-6666-666666666666",
    status: "failed",
    error: "restore container exited with code 1",
    created_at: now,
    finished_at: now,
  },
];

/**
 * D3-16 — browser coverage for the database backup flows.
 *
 * The UI smoke job has no node agent, so provisioning a real database is
 * impossible here; like the previews-tab smoke, the API boundary is mocked
 * and the assertions are about the rendered contract and the exact mutation
 * bodies the page sends (schedule toggle, target clear, restore list).
 */
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
    await page.route(`**/api/v1/databases/${dbId}/backups`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ backups: [] }),
      }),
    );
    await page.route(`**/api/v1/databases/${dbId}/restores`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ restores }),
      }),
    );
    await page.route(`**/api/v1/databases/${dbId}/schedules`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ schedules: [schedule] }),
      }),
    );
    await page.route("**/api/v1/databases/backup-targets", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ targets: [target] }),
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

  test("schedule toggle sends only cron and enabled, even with a deleted target", async ({
    page,
    guardrails,
  }) => {
    const row = page.locator(".backup-row").filter({ hasText: cron });
    await expect(row).toBeVisible();
    // The destination no longer exists; the row says so instead of failing.
    await expect(row).toContainText("deleted target");

    const patch = page.waitForRequest(
      (request) =>
        request.url().includes(`/api/v1/databases/${dbId}/schedules/`) &&
        request.method() === "PATCH",
    );
    await page.route(`**/api/v1/databases/${dbId}/schedules/*`, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          schedule: { ...schedule, enabled: false },
        }),
      }),
    );
    await row.locator(".n-switch").click();

    // D3-2: pausing must not resend the dead target id, or the PATCH 404s.
    expect((await patch).postDataJSON()).toEqual({ cron, enabled: false });

    expect(guardrails.apiFailures).toEqual([]);
  });

  test("clearing a target prefix sends an explicit empty string", async ({
    page,
    guardrails,
  }) => {
    const row = page.locator(".backup-row").filter({ hasText: target.name });
    await expect(row).toBeVisible();
    await row.getByRole("button", { name: "Edit" }).click();

    // Naive puts aria-label on the input wrapper; drill into the native input.
    const prefix = page.getByLabel("Key prefix").locator("input");
    await expect(prefix).toHaveValue("logs/");

    const patch = page.waitForRequest(
      (request) =>
        request.url().includes("/api/v1/databases/backup-targets/") &&
        request.method() === "PATCH",
    );
    await page.route("**/api/v1/databases/backup-targets/*", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ target: { ...target, prefix: "" } }),
      }),
    );
    await prefix.fill("");
    await page.getByRole("button", { name: "Save target" }).click();

    // D3-5: the clear marker must reach the server; blank credentials stay
    // omitted so the stored keys survive.
    const body = (await patch).postDataJSON() as Record<string, unknown>;
    expect(body["prefix"]).toBe("");
    expect(body).not.toHaveProperty("access_key");
    expect(body).not.toHaveProperty("secret_key");

    expect(guardrails.apiFailures).toEqual([]);
  });

  test("restore history lists every durable restore run", async ({
    page,
    guardrails,
  }) => {
    // The future next-run renders forward-looking (D3-3), not "just now".
    await expect(
      page.locator(".backup-row").filter({ hasText: cron }),
    ).toContainText("in 2h");

    // D3-7: the 202 queue acknowledgement is not the end of the story; the
    // durable rows render with their terminal states.
    const history = page.locator(".n-card").filter({ hasText: "Restore history" });
    await expect(history).toBeVisible();
    await expect(history).toContainText("completed");
    await expect(history).toContainText("failed");
    await expect(history).toContainText("restore container exited with code 1");
    await expect(history).toContainText("backup 66666666");

    expect(guardrails.apiFailures).toEqual([]);
  });
});
