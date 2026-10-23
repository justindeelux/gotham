import type { APIRequestContext, Locator, Page, Route } from "@playwright/test";

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

test.describe("teams", () => {
  test("creates a team, invites, accepts, re-roles, removes and deletes", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const teamName = `ui-e2e-team-${suffix}`;
    const inviteeEmail = `ui-e2e-invitee-${suffix}@example.com`;
    const inviteePassword = "Gotham-E2E-Password1";

    let register = await request.post("/api/v1/auth/register", {
      data: { email: inviteeEmail, password: inviteePassword },
    });
    if (register.status() === 409) {
      register = await request.post("/api/v1/auth/login", {
        data: { email: inviteeEmail, password: inviteePassword },
      });
    }
    expect(register.status(), await register.text()).toBe(200);
    const invitee = (await register.json()) as { access_token: string };

    await page.goto("/teams");
    await expect(
      page.getByRole("heading", { name: "Teams", level: 1 }),
    ).toBeVisible();

    await page.getByRole("button", { name: "New team" }).click();
    const createModal = page.locator(".n-modal").filter({ hasText: "New team" });
    await createModal.getByLabel("Team name").locator("input").fill(teamName);
    await createModal.getByRole("button", { name: "Create team" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);

    const teamRow = page.locator(`[data-team="${teamName}"]`);
    await expect(teamRow).toBeVisible();

    const personalRow = page.locator("[data-team]").filter({ hasText: "personal" });
    await expect(personalRow).toHaveCount(1);
    await expect(personalRow.getByRole("button", { name: "Delete" })).toBeDisabled();

    await teamRow.getByRole("button", { name: teamName }).click();
    const account = loadAccount();
    await expect(memberRow(page, account.email)).toBeVisible();

    await page.locator(".n-tabs-tab").filter({ hasText: "Invites" }).click();
    await page.getByRole("button", { name: "Invite member" }).click();
    const inviteModal = page.locator(".n-modal").filter({ hasText: "Invite member" });
    await inviteModal.getByLabel("Invite email").locator("input").fill(inviteeEmail);
    await inviteModal.getByRole("button", { name: "Create invite" }).click();

    const tokenModal = page.locator(".n-modal").filter({ hasText: "Invite created" });
    await expect(tokenModal).toBeVisible();
    const tokenLine = await tokenModal.locator('[data-testid="invite-token"]').innerText();
    const token = tokenLine.replace(/^token:\s*/, "").trim();
    expect(token.length).toBeGreaterThan(10);
    const acceptLink = await tokenModal
      .locator('[data-testid="invite-accept-link"] input')
      .inputValue();
    expect(acceptLink).toContain(`/invite/accept?token=${encodeURIComponent(token)}`);
    await tokenModal.getByRole("button", { name: "Done" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);

    const storage = await page.evaluate(() =>
      [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join("\n"),
    );
    expect(storage).not.toContain(token);
    expect(await page.content()).not.toContain(token);

    const accepted = await request.post("/api/v1/invites/accept", {
      headers: { Authorization: `Bearer ${invitee.access_token}` },
      data: { token },
    });
    expect(accepted.status(), await accepted.text()).toBe(200);

    await page.locator(".n-tabs-tab").filter({ hasText: "Members" }).click();
    await page.getByRole("button", { name: "Refresh" }).click();
    await expect(page.locator('[data-testid="members-table"]')).toBeVisible();
    const row = memberRow(page, inviteeEmail);
    await expect(row).toBeVisible();

    await row.locator(".n-select").click();
    await page.locator(".n-base-select-option").filter({ hasText: "admin" }).click();
    await expect(row.locator(".n-select")).toContainText("admin");
    await expect(page.locator('[data-testid="member-action-error"]')).toHaveCount(0);

    await row.getByRole("button", { name: "Remove" }).click();
    await page.locator(".n-popconfirm").getByRole("button", { name: "Confirm" }).click();
    await expect(memberRow(page, inviteeEmail)).toHaveCount(0);

    await teamRow.getByRole("button", { name: "Delete" }).click();
    await page.locator(".n-popconfirm").getByRole("button", { name: "Confirm" }).click();
    await expect(page.getByText(`Deleted team ${teamName}`)).toBeVisible();
    await expect(page.locator(`[data-team="${teamName}"]`)).toHaveCount(0);

    expect(
      guardrails.apiFailures,
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
    await selectOption("Team-wide").click();
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

async function seedTeam(
  request: APIRequestContext,
  name: string,
): Promise<{ id: string; name: string }> {
  const response = await request.post("/api/v1/teams", {
    headers: authHeaders(),
    data: { name },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { team } = (await response.json()) as { team: { id: string; name: string } };
  return team;
}

async function seedInvite(
  request: APIRequestContext,
  teamId: string,
  email: string,
  role: "admin" | "read_only",
): Promise<string> {
  const response = await request.post(`/api/v1/teams/${teamId}/invites`, {
    headers: authHeaders(),
    data: { email, role },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { invite } = (await response.json()) as { invite: { token: string } };
  return invite.token;
}

async function acceptInviteAs(
  request: APIRequestContext,
  accessToken: string,
  token: string,
): Promise<void> {
  const response = await request.post("/api/v1/invites/accept", {
    headers: { Authorization: `Bearer ${accessToken}` },
    data: { token },
  });
  expect(response.status(), await response.text()).toBe(200);
}

let sharedInvitee: { email: string; accessToken: string; userId: string } | null =
  null;

async function ensureInvitee(
  request: APIRequestContext,
): Promise<{ email: string; accessToken: string; userId: string }> {
  if (sharedInvitee) {
    return sharedInvitee;
  }
  const email = `ui-e2e-race-${uniqueSuffix()}@example.com`;
  const response = await request.post("/api/v1/auth/register", {
    data: { email, password: "Gotham-E2E-Password1" },
  });
  expect(response.status(), await response.text()).toBe(200);
  const body = (await response.json()) as {
    access_token: string;
    user: { id: string };
  };
  sharedInvitee = { email, accessToken: body.access_token, userId: body.user.id };
  return sharedInvitee;
}

async function joinTeam(
  request: APIRequestContext,
  teamId: string,
  role: "admin" | "read_only",
): Promise<void> {
  const invitee = await ensureInvitee(request);
  await acceptInviteAs(
    request,
    invitee.accessToken,
    await seedInvite(request, teamId, invitee.email, role),
  );
}

function memberBody(
  member: { userId: string; email: string },
  role: string,
): Record<string, string> {
  return {
    user_id: member.userId,
    email: member.email,
    role,
    created_at: new Date().toISOString(),
  };
}

function memberRow(page: Page, email: string): Locator {
  return page.locator('[data-testid="members-table"] tr').filter({ hasText: email });
}

function invitesRow(page: Page, email: string): Locator {
  return page.locator('[data-testid="invites-table"] tr').filter({ hasText: email });
}

async function setMemberRoleViaUi(
  page: Page,
  email: string,
  role: string,
): Promise<void> {
  await memberRow(page, email).locator(".n-select").click();
  await page.locator(".n-base-select-option").filter({ hasText: role }).click();
}

async function seedChannel(
  request: APIRequestContext,
  teamId: string,
  name: string,
): Promise<void> {
  const response = await request.post("/api/v1/notification-channels", {
    headers: { ...authHeaders(), "X-Team-Id": teamId },
    data: { name, kind: "discord", config: { webhook_url: "http://127.0.0.1:9/hook" } },
  });
  expect(response.status(), await response.text()).toBe(201);
}

async function settle(page: Page): Promise<void> {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );
}

async function selectTeamInScope(page: Page, teamName: string): Promise<void> {
  const select = page.locator('[aria-label="Notification team"]');
  await select.click();
  await select.locator("input").fill(teamName);
  await page.locator(".n-base-select-option").filter({ hasText: teamName }).click();
}

test.describe("team context races", () => {
  test.use({
    expectedConsoleErrors: ["Failed to load resource: net::ERR_FAILED"],
  });

  test("a delayed read cannot overwrite the selected team's members and invites", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const teamA = await seedTeam(request, `ui-e2e-race-a-${suffix}`);
    const teamB = await seedTeam(request, `ui-e2e-race-b-${suffix}`);
    const invitee = await ensureInvitee(request);
    const pendingEmail = `ui-e2e-race-pending-${suffix}@example.com`;
    await acceptInviteAs(
      request,
      invitee.accessToken,
      await seedInvite(request, teamA.id, invitee.email, "read_only"),
    );
    await seedInvite(request, teamA.id, pendingEmail, "read_only");

    const aMembers = await request.get(`/api/v1/teams/${teamA.id}/members`, {
      headers: authHeaders(),
    });
    expect(aMembers.status(), await aMembers.text()).toBe(200);
    expect(((await aMembers.json()) as { members: unknown[] }).members.length).toBe(2);

    let releaseA: () => void = () => undefined;
    const aGate = new Promise<void>((resolve) => {
      releaseA = resolve;
    });
    const holdTeamA = async (route: Route): Promise<void> => {
      if (route.request().url().includes(`/teams/${teamA.id}/`)) {
        await aGate;
      }
      await route.continue();
    };
    await page.route("**/api/v1/teams/*/members", holdTeamA);
    await page.route("**/api/v1/teams/*/invites", holdTeamA);

    await page.goto("/teams");
    const rowA = page.locator(`[data-team="${teamA.name}"]`);
    const rowB = page.locator(`[data-team="${teamB.name}"]`);
    await expect(rowA).toBeVisible();

    await rowA.getByRole("button", { name: teamA.name }).click();
    await rowB.getByRole("button", { name: teamB.name }).click();
    await expect(rowB).toHaveClass(/is-selected/);
    await expect(memberRow(page, loadAccount().email)).toHaveCount(1);
    await expect(memberRow(page, invitee.email)).toHaveCount(0);

    const membersLanded = page.waitForResponse(
      (response) =>
        response.url().includes(`/teams/${teamA.id}/members`) &&
        response.status() === 200,
    );
    const invitesLanded = page.waitForResponse(
      (response) =>
        response.url().includes(`/teams/${teamA.id}/invites`) &&
        response.status() === 200,
    );
    releaseA();
    await membersLanded;
    await invitesLanded;
    await settle(page);

    await expect(memberRow(page, invitee.email)).toHaveCount(0);
    await expect(memberRow(page, loadAccount().email)).toHaveCount(1);

    await page.locator(".n-tabs-tab").filter({ hasText: "Invites" }).click();
    await expect(invitesRow(page, pendingEmail)).toHaveCount(0);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });

  test("a failed read for the newly selected team does not retain the previous team's rows", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const teamA = await seedTeam(request, `ui-e2e-fail-a-${suffix}`);
    const teamB = await seedTeam(request, `ui-e2e-fail-b-${suffix}`);
    const invitee = await ensureInvitee(request);
    await acceptInviteAs(
      request,
      invitee.accessToken,
      await seedInvite(request, teamA.id, invitee.email, "read_only"),
    );

    await page.goto("/teams");
    await page
      .locator(`[data-team="${teamA.name}"]`)
      .getByRole("button", { name: teamA.name })
      .click();
    await expect(memberRow(page, invitee.email)).toBeVisible();

    const failTeamB = async (route: Route): Promise<void> => {
      if (route.request().url().includes(`/teams/${teamB.id}/`)) {
        await route.abort("failed");
        return;
      }
      await route.continue();
    };
    await page.route("**/api/v1/teams/*/members", failTeamB);
    await page.route("**/api/v1/teams/*/invites", failTeamB);

    await page
      .locator(`[data-team="${teamB.name}"]`)
      .getByRole("button", { name: teamB.name })
      .click();

    await expect(page.locator('[data-testid="members-error"]')).toBeVisible();
    await expect(memberRow(page, invitee.email)).toHaveCount(0);

    await page.locator(".n-tabs-tab").filter({ hasText: "Invites" }).click();
    await expect(page.locator('[data-testid="invites-error"]')).toBeVisible();

    const unexpected = guardrails.apiFailures.filter(
      (line) => !line.includes(`/teams/${teamB.id}/`),
    );
    expect(
      unexpected,
      `unexpected failed API requests:\n${unexpected.join("\n")}`,
    ).toEqual([]);
  });
});

test.describe("team mutation races", () => {
  test.use({
    expectedConsoleErrors: ["Failed to load resource: net::ERR_FAILED"],
  });

  test("an old role response cannot overwrite a newer mutation after A → B → A", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const teamA = await seedTeam(request, `ui-e2e-mut-a-${suffix}`);
    const teamB = await seedTeam(request, `ui-e2e-mut-b-${suffix}`);
    const invitee = await ensureInvitee(request);
    await joinTeam(request, teamA.id, "admin");
    await joinTeam(request, teamB.id, "admin");

    let releaseStale: () => void = () => undefined;
    const staleGate = new Promise<void>((resolve) => {
      releaseStale = resolve;
    });
    let patchCount = 0;
    await page.route("**/api/v1/teams/*/members/*", async (route) => {
      if (route.request().method() !== "PATCH") {
        await route.continue();
        return;
      }
      patchCount += 1;
      const role = (route.request().postDataJSON() as { role: string }).role;
      if (patchCount === 1) {
        await staleGate;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ member: memberBody(invitee, "read_only") }),
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ member: memberBody(invitee, role) }),
      });
    });

    await page.goto("/teams");
    const rowA = page.locator(`[data-team="${teamA.name}"]`);
    const rowB = page.locator(`[data-team="${teamB.name}"]`);
    await rowA.getByRole("button", { name: teamA.name }).click();
    await expect(memberRow(page, invitee.email)).toBeVisible();

    await setMemberRoleViaUi(page, invitee.email, "read-only");
    await expect(
      memberRow(page, invitee.email).locator(".n-base-loading__container").first(),
    ).toBeVisible();

    await rowB.getByRole("button", { name: teamB.name }).click();
    await rowA.getByRole("button", { name: teamA.name }).click();
    await expect(memberRow(page, invitee.email)).toBeVisible();

    await setMemberRoleViaUi(page, invitee.email, "owner");
    await expect(memberRow(page, invitee.email).locator(".n-select")).toContainText(
      "owner",
    );

    releaseStale();
    await settle(page);

    await expect(memberRow(page, invitee.email).locator(".n-select")).toContainText(
      "owner",
    );
    await expect(
      page.getByText(`${invitee.email} is now read-only`),
    ).toHaveCount(0);
    expect(patchCount).toBe(2);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });

  test("a pending mutation's spinner is not inherited across teams and survives an old completion", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const teamA = await seedTeam(request, `ui-e2e-spin-a-${suffix}`);
    const teamB = await seedTeam(request, `ui-e2e-spin-b-${suffix}`);
    const invitee = await ensureInvitee(request);
    await joinTeam(request, teamA.id, "admin");
    await joinTeam(request, teamB.id, "admin");

    let releaseA: () => void = () => undefined;
    let releaseB: () => void = () => undefined;
    const gateA = new Promise<void>((resolve) => {
      releaseA = resolve;
    });
    const gateB = new Promise<void>((resolve) => {
      releaseB = resolve;
    });
    await page.route("**/api/v1/teams/*/members/*", async (route) => {
      if (route.request().method() !== "PATCH") {
        await route.continue();
        return;
      }
      const role = (route.request().postDataJSON() as { role: string }).role;
      if (route.request().url().includes(`/teams/${teamA.id}/`)) {
        await gateA;
      } else {
        await gateB;
      }
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ member: memberBody(invitee, role) }),
      });
    });

    await page.goto("/teams");
    const rowA = page.locator(`[data-team="${teamA.name}"]`);
    const rowB = page.locator(`[data-team="${teamB.name}"]`);

    await rowA.getByRole("button", { name: teamA.name }).click();
    await setMemberRoleViaUi(page, invitee.email, "read-only");
    await expect(
      memberRow(page, invitee.email).locator(".n-base-loading__container").first(),
    ).toBeVisible();

    await rowB.getByRole("button", { name: teamB.name }).click();
    await expect(memberRow(page, invitee.email)).toBeVisible();
    await expect(memberRow(page, invitee.email).locator(".n-base-loading__container")).toHaveCount(
      0,
    );

    await setMemberRoleViaUi(page, invitee.email, "read-only");
    await expect(
      memberRow(page, invitee.email).locator(".n-base-loading__container").first(),
    ).toBeVisible();
    releaseA();
    await settle(page);
    await expect(
      memberRow(page, invitee.email).locator(".n-base-loading__container").first(),
    ).toBeVisible();

    releaseB();
    await expect(memberRow(page, invitee.email).locator(".n-select")).toContainText(
      "read-only",
    );
    await expect(memberRow(page, invitee.email).locator(".n-base-loading__container")).toHaveCount(
      0,
    );

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });

  test("an invite cancelled in one team never leaves the next team's form loading", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const teamA = await seedTeam(request, `ui-e2e-inv-a-${suffix}`);
    const teamB = await seedTeam(request, `ui-e2e-inv-b-${suffix}`);
    const emailA = `ui-e2e-inv-a-${suffix}@example.com`;
    const emailB = `ui-e2e-inv-b-${suffix}@example.com`;

    let releaseA: () => void = () => undefined;
    const gateA = new Promise<void>((resolve) => {
      releaseA = resolve;
    });
    const posts: string[] = [];
    await page.route("**/api/v1/teams/*/invites", async (route) => {
      if (route.request().method() !== "POST") {
        await route.continue();
        return;
      }
      const forTeamA = route.request().url().includes(`/teams/${teamA.id}/`);
      posts.push(forTeamA ? "A" : "B");
      if (forTeamA) {
        await gateA;
      }
      await route.continue();
    });

    await page.goto("/teams");
    const rowA = page.locator(`[data-team="${teamA.name}"]`);
    const rowB = page.locator(`[data-team="${teamB.name}"]`);

    await rowA.getByRole("button", { name: teamA.name }).click();
    await page.locator(".n-tabs-tab").filter({ hasText: "Invites" }).click();
    await page.getByRole("button", { name: "Invite member" }).click();
    let modal = page.locator(".n-modal").filter({ hasText: "Invite member" });
    await modal.getByLabel("Invite email").locator("input").fill(emailA);
    await modal.getByRole("button", { name: "Create invite" }).click();
    await expect(modal.getByRole("button", { name: "Create invite" })).toHaveClass(
      /n-button--loading/,
    );
    await modal.getByRole("button", { name: "Cancel" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);

    await rowB.getByRole("button", { name: teamB.name }).click();
    await page.getByRole("button", { name: "Invite member" }).click();
    modal = page.locator(".n-modal").filter({ hasText: "Invite member" });
    const createButton = modal.getByRole("button", { name: "Create invite" });
    await expect(createButton).not.toHaveClass(/n-button--loading/);
    await modal.getByLabel("Invite email").locator("input").fill(emailB);
    await createButton.click();

    const tokenModal = page.locator(".n-modal").filter({ hasText: "Invite created" });
    await expect(tokenModal).toBeVisible();
    await expect(tokenModal).toContainText(emailB);
    expect(posts).toEqual(["A", "B"]);

    releaseA();
    await settle(page);
    await expect(tokenModal).toContainText(emailB);
    await expect(tokenModal).not.toContainText(emailA);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });

  test("a superseded delete cannot clear the newer spinner and the newer success still reports", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const teamA = await seedTeam(request, `ui-e2e-del-a-${suffix}`);
    const teamB = await seedTeam(request, `ui-e2e-del-b-${suffix}`);

    let releaseFirst: () => void = () => undefined;
    let releaseSecond: () => void = () => undefined;
    const firstGate = new Promise<void>((resolve) => {
      releaseFirst = resolve;
    });
    const secondGate = new Promise<void>((resolve) => {
      releaseSecond = resolve;
    });
    let deletes = 0;
    await page.route("**/api/v1/teams/*", async (route) => {
      if (route.request().method() !== "DELETE") {
        await route.continue();
        return;
      }
      deletes += 1;
      if (deletes === 1) {
        await firstGate;
        await route.abort("failed");
        return;
      }
      await secondGate;
      await route.fulfill({ status: 204, body: "" });
    });

    await page.goto("/teams");
    const rowA = page.locator(`[data-team="${teamA.name}"]`);
    const rowB = page.locator(`[data-team="${teamB.name}"]`);
    const deleteButtonA = (): Locator =>
      rowA.getByRole("button", { name: "Delete" });

    await rowA.getByRole("button", { name: teamA.name }).click();
    await deleteButtonA().click();
    await page.locator(".n-popconfirm").getByRole("button", { name: "Confirm" }).click();
    await expect(deleteButtonA()).toHaveClass(/n-button--loading/);

    await rowB.getByRole("button", { name: teamB.name }).click();
    await rowA.getByRole("button", { name: teamA.name }).click();
    await deleteButtonA().click();
    await page
      .locator(".n-popconfirm")
      .getByRole("button", { name: "Confirm" })
      .last()
      .click();
    await expect(deleteButtonA()).toHaveClass(/n-button--loading/);

    releaseFirst();
    await settle(page);
    await expect(deleteButtonA()).toHaveClass(/n-button--loading/);
    await expect(page.locator('[data-testid="team-action-error"]')).toHaveCount(0);

    releaseSecond();
    await expect(page.getByText(`Deleted team ${teamA.name}`)).toBeVisible();
    await expect(page.locator(`[data-team="${teamA.name}"]`)).toHaveCount(0);
    expect(deletes).toBe(2);

    const unexpected = guardrails.apiFailures.filter(
      (line) =>
        !(line.includes(`/teams/${teamA.id}`) && line.includes("requestfailed")),
    );
    expect(
      unexpected,
      `unexpected failed API requests:\n${unexpected.join("\n")}`,
    ).toEqual([]);
  });

  test("a superseded delete failure cannot surface after a newer success", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const teamA = await seedTeam(request, `ui-e2e-late-a-${suffix}`);

    let releaseFirst: () => void = () => undefined;
    const firstGate = new Promise<void>((resolve) => {
      releaseFirst = resolve;
    });
    let deletes = 0;
    await page.route("**/api/v1/teams/*", async (route) => {
      if (route.request().method() !== "DELETE") {
        await route.continue();
        return;
      }
      deletes += 1;
      if (deletes === 1) {
        await firstGate;
        await route.abort("failed");
        return;
      }
      await route.fulfill({ status: 204, body: "" });
    });

    await page.goto("/teams");
    const rowA = page.locator(`[data-team="${teamA.name}"]`);
    await rowA.getByRole("button", { name: teamA.name }).click();

    await rowA.getByRole("button", { name: "Delete" }).click();
    await page.locator(".n-popconfirm").getByRole("button", { name: "Confirm" }).click();
    await expect(rowA.getByRole("button", { name: "Delete" })).toHaveClass(
      /n-button--loading/,
    );

    await rowA.getByRole("button", { name: "Delete" }).click();
    await page
      .locator(".n-popconfirm")
      .getByRole("button", { name: "Confirm" })
      .last()
      .click();
    await expect(page.getByText(`Deleted team ${teamA.name}`)).toBeVisible();
    await expect(page.locator(`[data-team="${teamA.name}"]`)).toHaveCount(0);
    expect(deletes).toBe(2);

    releaseFirst();
    await settle(page);
    await expect(page.locator('[data-testid="team-action-error"]')).toHaveCount(0);
    await expect(page.getByText(`Deleted team ${teamA.name}`)).toBeVisible();

    const unexpected = guardrails.apiFailures.filter(
      (line) =>
        !(line.includes(`/teams/${teamA.id}`) && line.includes("requestfailed")),
    );
    expect(
      unexpected,
      `unexpected failed API requests:\n${unexpected.join("\n")}`,
    ).toEqual([]);
  });
});

test.describe("notification channel team races", () => {
  test.use({
    expectedConsoleErrors: ["Failed to load resource: net::ERR_FAILED"],
  });

  test("a delayed channel read cannot overwrite the selected team and a failed read retains nothing", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

    const suffix = uniqueSuffix();
    const teamA = await seedTeam(request, `ui-e2e-chan-a-${suffix}`);
    const teamB = await seedTeam(request, `ui-e2e-chan-b-${suffix}`);
    const channelA = `ui-e2e-chan-a-${suffix}`;
    const channelB = `ui-e2e-chan-b-${suffix}`;
    await seedChannel(request, teamA.id, channelA);
    await seedChannel(request, teamB.id, channelB);

    let releaseA: () => void = () => undefined;
    const aGate = new Promise<void>((resolve) => {
      releaseA = resolve;
    });
    await page.route("**/api/v1/notification-channels", async (route) => {
      if (route.request().headers()["x-team-id"] === teamA.id) {
        await aGate;
      }
      await route.continue();
    });

    await page.goto("/settings/notifications");
    await expect(
      page.getByRole("heading", { name: "Notification channels", level: 1 }),
    ).toBeVisible();

    await selectTeamInScope(page, teamA.name);
    await selectTeamInScope(page, teamB.name);
    await expect(page.locator(`[data-channel="${channelB}"]`)).toBeVisible();

    const aLanded = page.waitForResponse(
      (response) =>
        response.url().endsWith("/api/v1/notification-channels") &&
        response.request().headers()["x-team-id"] === teamA.id &&
        response.status() === 200,
    );
    releaseA();
    await aLanded;
    await settle(page);

    await expect(page.locator(`[data-channel="${channelB}"]`)).toBeVisible();
    await expect(page.locator(`[data-channel="${channelA}"]`)).toHaveCount(0);

    await selectTeamInScope(page, teamA.name);
    await expect(page.locator(`[data-channel="${channelA}"]`)).toBeVisible();

    await page.unroute("**/api/v1/notification-channels");
    await page.route("**/api/v1/notification-channels", async (route) => {
      if (route.request().headers()["x-team-id"] === teamB.id) {
        await route.abort("failed");
        return;
      }
      await route.continue();
    });
    await selectTeamInScope(page, teamB.name);

    await expect(page.locator('[data-testid="channels-error"]')).toBeVisible();
    await expect(page.locator(`[data-channel="${channelA}"]`)).toHaveCount(0);
    await expect(page.getByText("No channels for this team yet.")).toHaveCount(0);

    const unexpected = guardrails.apiFailures.filter(
      (line) =>
        !(line.includes("/notification-channels") && line.includes("requestfailed")),
    );
    expect(
      unexpected,
      `unexpected failed API requests:\n${unexpected.join("\n")}`,
    ).toEqual([]);
  });
});

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

test.describe("teams feature off", () => {
  test.use({
    expectedConsoleErrors: [
      "Failed to load resource: the server responded with a status of 404",
    ],
  });

  test("hides the team-management actions when the feature is disabled", async ({
    page,
  }) => {
    await page.route("**/api/v1/teams", (route) =>
      route.fulfill({
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ message: "not found" }),
      }),
    );

    await page.goto("/teams");
    await expect(
      page.getByText("Team management is turned off on this control plane"),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "New team" })).toHaveCount(0);
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
    const team = await seedTeam(request, `ui-e2e-redirect-${suffix}`);
    const email = `ui-e2e-register-${suffix}@example.com`;
    const token = await seedInvite(request, team.id, email, "read_only");

    await page.goto(`/invite/accept?token=${encodeURIComponent(token)}`);
    await expect(page).toHaveURL(/\/login\?redirect=/);

    await page.getByRole("link", { name: "Create account" }).click();
    await expect(page).toHaveURL(/\/register\?redirect=/);
    expect(decodeURIComponent(new URL(page.url()).search)).toContain(
      `/invite/accept?token=${token}`,
    );

    await page.getByPlaceholder("you@gotham.dev").fill(email);
    await page.getByPlaceholder("At least 10 characters").fill("Gotham-E2E-Password1");
    await page.getByPlaceholder("Repeat your password").fill("Gotham-E2E-Password1");
    await page.getByRole("checkbox").check();
    await page.getByRole("button", { name: "Create account", exact: true }).click();

    await expect(page.getByText(`You joined ${team.name}`)).toBeVisible();
    await expect(page).not.toHaveURL(/token=/);
    expect(
      await page.evaluate(() =>
        [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join("\n"),
      ),
    ).not.toContain(token);

    const members = await request.get(`/api/v1/teams/${team.id}/members`, {
      headers: authHeaders(),
    });
    expect(members.status(), await members.text()).toBe(200);
    const emails = ((await members.json()) as { members: { email: string }[] }).members.map(
      (member) => member.email,
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
