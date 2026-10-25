import type { APIRequestContext, Page, Route } from "@playwright/test";

import { expect, test } from "./fixtures";
import { loadAccount, nestedURL, seedNodeAddress, seedProjectEnvironment, storageStatePath, uniqueSuffix } from "./support";

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

async function seedApplication(
  request: APIRequestContext,
  serverId: string,
  name: string,
): Promise<{ id: string; name: string; projectId: string; environmentId: string }> {
  const { projectId, environmentId } = await seedProjectEnvironment(request, authHeaders());
  const response = await request.post("/api/v1/applications", {
    headers: authHeaders(),
    data: {
      name,
      environment_id: environmentId,
      provider: "github",
      repo: "docker/welcome-to-docker",
      clone_url: "https://github.com/docker/welcome-to-docker.git",
      branch: "main",
      build_pack: "auto",
      server_id: serverId,
    },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { application } = (await response.json()) as {
    application: { id: string; name: string };
  };
  return { ...application, projectId, environmentId };
}

test.describe("previews tab", () => {
  test.use({
    expectedConsoleErrors: [
      "Failed to load resource: the server responded with a status of 404",
    ],
  });

  test("lists preview bindings and hides the tab when the feature is off", async ({
    page,
    request,
    guardrails,
  }) => {
    const suffix = uniqueSuffix();
    const node = await seedServer(request, `ui-e2e-prev-node-${suffix}`);
    const application = await seedApplication(
      request,
      node.id,
      `ui-e2e-prev-${suffix}`,
    );

    const host = `pr-482-${suffix}.preview.example.test`;
    const branch = `feat/checkout-${suffix}`;
    const previewsPattern = "**/api/v1/applications/*/previews";
    await page.route(previewsPattern, (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          previews: [
            {
              id: "11111111-1111-1111-1111-111111111111",
              application_id: application.id,
              preview_application_id: "22222222-2222-2222-2222-222222222222",
              provider: "github",
              repo: "docker/welcome-to-docker",
              pr_number: 482,
              branch,
              head_sha: "9a12ff0deadbeef1234",
              host,
              state: "active",
              created_at: new Date().toISOString(),
              updated_at: new Date().toISOString(),
            },
          ],
        }),
      }),
    );

    await page.goto(
      nestedURL(application.projectId, application.environmentId, "applications", application.id),
    );

    const previewsTab = page.locator(".n-tabs-tab").filter({ hasText: "Previews" });
    await expect(previewsTab).toContainText("Previews (1)");
    await previewsTab.click();

    const row = page.locator("tr").filter({ hasText: "#482" });
    await expect(row).toBeVisible();
    await expect(row).toContainText(branch);
    await expect(row).toContainText("active");
    await expect(row.locator(`a[href="http://${host}"]`)).toBeVisible();

    await page.unroute(previewsPattern);
    await page.route(previewsPattern, (route) =>
      route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ message: "not found" }),
      }),
    );
    await page.reload();
    await expect(page.locator(".n-tabs-tab").filter({ hasText: "Previews" })).toHaveCount(0);
    await expect(
      page.getByText("Preview deployments are turned off on this control plane"),
    ).toHaveCount(0);

    expect(
      guardrails.apiFailures.filter((line) => !line.includes("/previews")),
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});

test.describe("notification channels", () => {
  test.use({
    expectedConsoleErrors: [
      "Failed to load resource: the server responded with a status of 404",
    ],
  });

  test("creates an application-scoped channel with deliverable events while databases are disabled", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const name = `ui-e2e-hook-${suffix}`;
    const webhook = `http://127.0.0.1:9/${name}`;
    const masked = `…${name.slice(-4)}`;
    const node = await seedServer(request, `ui-e2e-hook-node-${suffix}`);
    const application = await seedApplication(
      request,
      node.id,
      `ui-e2e-hook-app-${suffix}`,
    );

    await page.route("**/api/v1/databases", (route) =>
      route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ message: "not found" }),
      }),
    );

    await page.goto("/settings/notifications");
    await expect(
      page.getByRole("heading", { name: "Notification channels", level: 1 }),
    ).toBeVisible();

    await page.getByRole("button", { name: "New channel" }).click();
    const modal = page.locator(".n-modal").filter({ hasText: "New notification channel" });
    await modal.getByLabel("Channel name").locator("input").fill(name);
    await modal.getByLabel("Webhook URL").locator("input").fill(webhook);

    const selectOption = (label: string) =>
      page.locator(".n-base-select-option").filter({ hasText: label });

    await modal.locator('[aria-label="Resource scope"]').click();
    await expect(selectOption("Database")).toHaveClass(/n-base-select-option--disabled/);
    await expect(selectOption("Application")).not.toHaveClass(
      /n-base-select-option--disabled/,
    );
    await selectOption("Application").click();
    await modal.locator('[aria-label="Resource"]').click();
    await modal.locator('[aria-label="Resource"] input').fill(application.name);
    await selectOption(application.name).click();

    await modal.locator('[aria-label="Events"]').click();
    await expect(selectOption("Deploy succeeded")).toBeVisible();
    await expect(selectOption("Deploy failed")).toBeVisible();
    await expect(selectOption("Backup succeeded")).toHaveCount(0);
    await expect(selectOption("Backup failed")).toHaveCount(0);
    await modal.locator(".n-card-header").first().click();

    await modal.getByRole("button", { name: "Create channel" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);

    const card = page.locator(".n-card").filter({ hasText: name });
    await expect(card).toBeVisible();
    await expect(card).toContainText("App: " + application.name);
    await expect(card).toContainText("Deploy succeeded");
    await expect(card).toContainText("Deploy failed");
    await expect(card).not.toContainText("Backup");
    await expect(card).toContainText("secret configured");
    await expect(card).toContainText(masked);
    expect(await page.content()).not.toContain(webhook);
    const storage = await page.evaluate(() =>
      [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join("\n"),
    );
    expect(storage).not.toContain(webhook);

    await card.getByRole("button", { name: "Send test" }).click();
    await expect(card.locator("[data-test-channel-result]")).toContainText("Test failed");

    await page.getByRole("switch", { name: `Enable ${name}` }).click();
    await expect(card).toContainText("disabled");

    await card.getByRole("button", { name: "Edit" }).click();
    const editModal = page.locator(".n-modal").filter({ hasText: "Edit notification channel" });
    await expect(editModal.getByLabel("Webhook URL").locator("input")).toHaveValue(masked);
    await expect(editModal.locator('[aria-label="Resource"]')).toContainText(application.name);
    await expect(editModal.locator('[aria-label="Events"]')).toContainText("Deploy succeeded");
    await expect(editModal.locator('[aria-label="Events"]')).not.toContainText("Backup");
    await editModal.getByRole("button", { name: "Save" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);
    expect(await page.content()).not.toContain(webhook);
    await expect(page.locator(".n-card").filter({ hasText: name })).toContainText(masked);

    await card.getByRole("button", { name: "Delete" }).click();
    await page.locator(".n-popconfirm").getByRole("button", { name: "Confirm" }).click();
    await expect(page.locator(".n-card").filter({ hasText: name })).toHaveCount(0);

    const unexpected = guardrails.apiFailures.filter(
      (line) => !line.includes("/api/v1/databases"),
    );
    expect(
      unexpected,
      `unexpected failed API requests:\n${unexpected.join("\n")}`,
    ).toEqual([]);
  });

  test("preserves a legacy out-of-scope subscription on an unrelated save and constrains a deliberate scope change", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const name = `ui-e2e-legacy-${suffix}`;
    const renamed = `${name}-renamed`;
    const node = await seedServer(request, `ui-e2e-legacy-node-${suffix}`);
    const application = await seedApplication(
      request,
      node.id,
      `ui-e2e-legacy-app-${suffix}`,
    );

    const seeded = await request.post("/api/v1/notification-channels", {
      headers: authHeaders(),
      data: {
        name,
        kind: "discord",
        events: ["backup_success"],
        resource_type: "application",
        resource_id: application.id,
        config: { webhook_url: "http://127.0.0.1:9/legacy" },
      },
    });
    expect(seeded.status(), await seeded.text()).toBe(201);
    const { channel } = (await seeded.json()) as { channel: { id: string } };

    const storedEvents = async (channelName: string): Promise<string[]> => {
      const response = await request.get("/api/v1/notification-channels", {
        headers: authHeaders(),
      });
      expect(response.status(), await response.text()).toBe(200);
      const { channels } = (await response.json()) as {
        channels: Array<{ name: string; events: string[] }>;
      };
      return channels.find((item) => item.name === channelName)?.events ?? [];
    };

    await page.goto("/settings/notifications");
    const card = page.locator(".n-card").filter({ hasText: name });
    await expect(card).toBeVisible();

    await card.getByRole("button", { name: "Edit" }).click();
    const modal = page.locator(".n-modal").filter({ hasText: "Edit notification channel" });
    await expect(modal.locator('[aria-label="Events"]')).toContainText("Backup succeeded");
    await expect(modal.locator('[aria-label="Events"]')).not.toContainText("Deploy succeeded");
    await expect(modal.locator('[data-testid="events-out-of-scope"]')).toContainText(
      "Backup succeeded",
    );

    await modal.getByLabel("Channel name").locator("input").fill(renamed);
    await modal.getByRole("button", { name: "Save" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);
    expect(await storedEvents(renamed)).toEqual(["backup_success"]);

    await page
      .locator(".n-card")
      .filter({ hasText: renamed })
      .getByRole("button", { name: "Edit" })
      .click();
    const scopeModal = page.locator(".n-modal").filter({ hasText: "Edit notification channel" });
    const selectOption = (label: string) =>
      page.locator(".n-base-select-option").filter({ hasText: label });
    await scopeModal.locator('[aria-label="Resource scope"]').click();
    await selectOption("All resources").click();
    await scopeModal.locator('[aria-label="Resource scope"]').click();
    await selectOption("Application").click();
    await scopeModal.locator('[aria-label="Resource"]').click();
    await scopeModal.locator('[aria-label="Resource"] input').fill(application.name);
    await selectOption(application.name).click();
    await expect(scopeModal.locator('[aria-label="Events"]')).toContainText("Deploy succeeded");
    await expect(scopeModal.locator('[aria-label="Events"]')).not.toContainText("Backup succeeded");
    await expect(scopeModal.locator('[data-testid="events-out-of-scope"]')).toHaveCount(0);
    await scopeModal.getByRole("button", { name: "Save" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);
    expect(await storedEvents(renamed)).toEqual(["deploy_success", "deploy_failure"]);

    const removed = await request.delete(`/api/v1/notification-channels/${channel.id}`, {
      headers: authHeaders(),
    });
    expect(removed.status(), await removed.text()).toBe(204);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});

test.describe("server metrics", () => {
  test.use({
    expectedConsoleErrors: [
      "Failed to load resource: the server responded with a status of 404",
    ],
  });

  test("renders the real empty window, then samples with gaps, a step switch, auto-refresh and single-flight", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(180_000);

    const node = await seedServer(request, `ui-e2e-metrics-node-${uniqueSuffix()}`);
    const requestedSteps: string[] = [];
    page.on("request", (request) => {
      const url = new URL(request.url());
      if (url.pathname === `/api/v1/servers/${node.id}/metrics`) {
        requestedSteps.push(url.searchParams.get("step") ?? "");
      }
    });

    await page.goto(`/servers/${node.id}`);
    await page.locator(".n-tabs-tab").filter({ hasText: "Metrics" }).click();

    for (const title of ["CPU", "RAM", "Disk I/O", "Network"]) {
      await expect(page.locator(`[data-chart="${title}"]`)).toBeVisible();
    }
    await expect(page.locator('[data-chart="CPU"]')).toContainText(
      "No samples in this window.",
    );
    await expect.poll(() => requestedSteps.includes("1m")).toBe(true);

    const base = Date.now() - 600_000;
    const point = (offsetMs: number, cpu: number) => ({
      bucket: new Date(base + offsetMs).toISOString(),
      cpu_usage: cpu,
      mem_usage: 0.4,
      disk_usage: 0.2,
      net_rx_bps: 1024,
      net_tx_bps: 512,
      disk_read_bps: 2048,
      disk_write_bps: 1024,
      container_count: 3,
    });
    await page.route("**/api/v1/servers/*/metrics*", (route) =>
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          step: new URL(route.request().url()).searchParams.get("step") ?? "1m",
          points: [
            point(0, 0.1),
            point(60_000, 0.2),
            point(120_000, 0.3),
            point(600_000, 0.5),
          ],
        }),
      }),
    );

    await page.getByRole("button", { name: "Refresh" }).click();
    const cpu = page.locator('[data-chart="CPU"]');
    await expect(cpu.locator("polyline")).toHaveCount(1);
    await expect(cpu.locator("circle")).toHaveCount(1);
    await expect(page.locator('[data-chart="Network"]').locator("polyline")).toHaveCount(2);

    await page.locator(".n-radio-button").filter({ hasText: "1h" }).click();
    await expect.poll(() => requestedSteps.includes("1h")).toBe(true);
    await expect(cpu.locator("polyline")).toHaveCount(1);

    const refreshGroup = page.locator('[aria-label="Auto-refresh"]');
    await expect(refreshGroup).toContainText("off");

    const beforeRefresh = requestedSteps.length;
    await page.locator(".n-radio-button").filter({ hasText: "15s" }).click();
    await expect
      .poll(() => requestedSteps.length, { timeout: 20_000 })
      .toBeGreaterThan(beforeRefresh);

    await page.evaluate(() => {
      Object.defineProperty(document, "hidden", {
        configurable: true,
        get: () => true,
      });
      document.dispatchEvent(new Event("visibilitychange"));
    });
    const onHide = requestedSteps.length;
    await page.waitForTimeout(16_000);
    expect(requestedSteps.length).toBe(onHide);

    await page.clock.install();
    await page.evaluate(() => {
      Object.defineProperty(document, "hidden", {
        configurable: true,
        get: () => false,
      });
      document.dispatchEvent(new Event("visibilitychange"));
    });
    await page.unroute("**/api/v1/servers/*/metrics*");

    type MetricsReply = "hold" | "ok" | "missing";
    let metricsReply: MetricsReply = "hold";
    let metricsRequests = 0;
    const heldRoutes: Route[] = [];
    await page.route("**/api/v1/servers/*/metrics*", (route) => {
      metricsRequests += 1;
      if (metricsReply === "hold") {
        heldRoutes.push(route);
        return;
      }
      if (metricsReply === "missing") {
        void route.fulfill({
          status: 404,
          contentType: "application/json",
          body: JSON.stringify({ message: "not found" }),
        });
        return;
      }
      void route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          step: "1m",
          points: [point(0, 0.1), point(60_000, 0.2)],
        }),
      });
    });

    await page.clock.fastForward(15_000);
    await expect.poll(() => metricsRequests, { timeout: 10_000 }).toBe(1);

    metricsReply = "ok";
    const manualDone = page.waitForResponse(
      (response) =>
        response.url().includes(`/servers/${node.id}/metrics`) &&
        response.status() === 200,
    );
    await page.getByRole("button", { name: "Refresh" }).click();
    await manualDone;
    await page.waitForTimeout(200);

    metricsReply = "hold";
    await page.clock.fastForward(15_000);
    await page.waitForTimeout(500);
    expect.soft(
      metricsRequests,
      "a newer completion must not clear the guard for a pending fetch",
    ).toBe(2);

    metricsReply = "missing";
    await page.getByRole("button", { name: "Refresh" }).click();
    await expect(
      page.getByText("Server metrics are turned off on this control plane"),
    ).toBeVisible();
    const after404 = metricsRequests;
    await page.clock.fastForward(16_000);
    await page.waitForTimeout(500);
    expect.soft(
      metricsRequests,
      "a disabled metrics feature must stop auto-refresh",
    ).toBe(after404);

    for (const route of heldRoutes) {
      await route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ message: "not found" }),
      });
    }
    await page.waitForTimeout(200);

    const unexpected = guardrails.apiFailures.filter(
      (line) => !line.includes(`/servers/${node.id}/metrics`),
    );
    expect(
      unexpected,
      `unexpected failed API requests:\n${unexpected.join("\n")}`,
    ).toEqual([]);
  });
});

async function seedInvite(
  request: APIRequestContext,
  email: string,
): Promise<string> {
  const response = await request.post("/api/v1/invites", {
    headers: authHeaders(),
    data: { email, project_ids: [] },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { token } = (await response.json()) as { token: string };
  return token;
}

async function settle(page: Page): Promise<void> {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );
}

test.describe("metrics range races", () => {
  test.use({
    expectedConsoleErrors: ["Failed to load resource: net::ERR_FAILED"],
  });

  test("a delayed range response cannot overwrite the selected range and a failed switch keeps no stale series", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const node = await seedServer(request, `ui-e2e-range-${uniqueSuffix()}`);
    const base = Date.now() - 3_600_000;
    const point = (bucketMs: number, cpu: number) => ({
      bucket: new Date(bucketMs).toISOString(),
      cpu_usage: cpu,
      mem_usage: 0.4,
      disk_usage: 0.2,
      net_rx_bps: 1024,
      net_tx_bps: 512,
      disk_read_bps: 2048,
      disk_write_bps: 1024,
      container_count: 3,
    });
    const minutePoints = [point(base, 0.1), point(base + 120_000, 0.11)];
    const hourPoints = [
      point(base, 0.2),
      point(base + 3_600_000, 0.4),
      point(base + 7_200_000, 0.6),
      point(base + 10_800_000, 0.8),
    ];

    let releaseMinute: () => void = () => undefined;
    const minuteGate = new Promise<void>((resolve) => {
      releaseMinute = resolve;
    });
    await page.route("**/api/v1/servers/*/metrics*", async (route) => {
      const step = new URL(route.request().url()).searchParams.get("step");
      if (step === "1m") {
        await minuteGate;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ step: "1m", points: minutePoints }),
        });
        return;
      }
      if (step === "1h") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ step: "1h", points: hourPoints }),
        });
        return;
      }
      await route.abort("failed");
    });

    await page.goto(`/servers/${node.id}`);
    await page.locator(".n-tabs-tab").filter({ hasText: "Metrics" }).click();
    await expect(page.locator('[data-chart="CPU"]')).toContainText(
      "Loading the metrics window…",
    );

    await page.locator(".n-radio-button").filter({ hasText: "1h" }).click();
    const cpuCard = page
      .locator(".n-card")
      .filter({ has: page.locator('[data-chart="CPU"]') });
    const cpu = page.locator('[data-chart="CPU"]');
    await expect(cpuCard).toContainText("80%");

    const minuteLanded = page.waitForResponse(
      (response) =>
        response.url().includes("/metrics") &&
        new URL(response.url()).searchParams.get("step") === "1m" &&
        response.status() === 200,
    );
    releaseMinute();
    await minuteLanded;
    await settle(page);

    await expect(cpuCard).toContainText("80%");
    await expect(cpu.locator("polyline")).toHaveCount(1);
    await expect(cpu.locator("circle")).toHaveCount(0);
    await expect(page.locator(".metrics-toolbar")).toContainText("last 24 hours");

    await page.locator(".n-radio-button").filter({ hasText: "1d" }).click();
    await expect(page.locator('[data-testid="metrics-error"]')).toBeVisible();
    await expect(cpuCard).toContainText("Metrics unavailable.");
    await expect(cpu.locator("polyline")).toHaveCount(0);

    const unexpected = guardrails.apiFailures.filter(
      (line) => !(line.includes("/metrics") && line.includes("step=1d")),
    );
    expect(
      unexpected,
      `unexpected failed API requests:\n${unexpected.join("\n")}`,
    ).toEqual([]);
  });
});

test.describe("invite redirect across registration", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test("a signed-out invite recipient keeps the invitation through registration", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const email = `ui-e2e-register-${suffix}@example.com`;
    const token = await seedInvite(request, email);

    await page.goto(`/register?invite=${encodeURIComponent(token)}`);
    await expect(page.getByPlaceholder("you@gotham.dev")).toHaveValue(email);

    await page.getByPlaceholder("At least 10 characters").fill("Gotham-E2E-Password1");
    await page.getByPlaceholder("Repeat your password").fill("Gotham-E2E-Password1");
    await page.getByRole("checkbox").check();
    await page.getByRole("button", { name: "Create account", exact: true }).click();

    await expect(page).not.toHaveURL(/invite=/);
    expect(
      await page.evaluate(() =>
        [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join("\n"),
      ),
    ).not.toContain(token);

    const users = await request.get("/api/v1/users", { headers: authHeaders() });
    expect(users.status(), await users.text()).toBe(200);
    const emails = ((await users.json()) as { users: { email: string }[] }).users.map(
      (user) => user.email,
    );
    expect(emails).toContain(email);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });

  test("the auth switch refuses a backslash-spelled protocol-relative redirect", async ({
    page,
  }) => {
    await page.goto(`/login?redirect=${encodeURIComponent("/\\evil.example.com")}`);
    const registerLink = page.getByRole("link", { name: "Create account" });
    await expect(registerLink).toBeVisible();
    await expect(registerLink).toHaveAttribute("href", "/register");
  });
});

test.describe("servers grid", () => {
  test("renders a seeded node as a card, not a table row", async ({ page, request }) => {
    const name = `ui-e2e-node-${uniqueSuffix()}`;
    await seedServer(request, name);

    await page.goto("/servers");
    await page.getByPlaceholder("Search by name, IP, OS…").fill(name);

    const card = page.locator(`[data-server="${name}"]`);
    await expect(card).toBeVisible();
    await expect(card.locator(".node-head")).toContainText(name);
    await expect(card.locator("dl.kv dt")).toHaveCount(4);
    await expect(card.locator(".node-metrics .node-metric")).toHaveCount(3);
    await expect(card.locator(".node-metrics .n-progress")).toHaveCount(3);
    await expect(card.getByRole("button", { name: "Open node" })).toBeVisible();
    await expect(card.getByRole("button", { name: "Revalidate SSH" })).toBeVisible();

    await expect(page.locator(".n-data-table")).toHaveCount(0);
  });
});
