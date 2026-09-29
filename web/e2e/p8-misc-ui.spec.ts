import type { APIRequestContext } from "@playwright/test";

import { expect, test } from "./fixtures";
import { loadAccount, storageStatePath, uniqueSuffix } from "./support";

// Every scenario starts from the session global setup created.
test.use({ storageState: storageStatePath });

/** authHeaders authenticates a direct API call with the smoke account. */
function authHeaders(): Record<string, string> {
  return { Authorization: `Bearer ${loadAccount().accessToken}` };
}

/** seedServer registers a node row; nothing is dialed without an agent. */
async function seedServer(
  request: APIRequestContext,
  name: string,
): Promise<{ id: string; name: string }> {
  const response = await request.post("/api/v1/servers", {
    headers: authHeaders(),
    data: { name, ip: "127.0.0.1", ssh_user: "root" },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { server } = (await response.json()) as { server: { id: string; name: string } };
  return server;
}

/** seedApplication stores a configuration-only application on one node. */
async function seedApplication(
  request: APIRequestContext,
  serverId: string,
  name: string,
): Promise<{ id: string; name: string }> {
  const response = await request.post("/api/v1/applications", {
    headers: authHeaders(),
    data: {
      name,
      provider: "github",
      repo: "docker/welcome-to-docker",
      clone_url: "https://github.com/docker/welcome-to-docker.git",
      branch: "main",
      build_pack: "auto",
      port: 80,
      host_port: 0,
      server_id: serverId,
    },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { application } = (await response.json()) as {
    application: { id: string; name: string };
  };
  return application;
}

/**
 * FE-8.1 (1) — the previews tab of the application detail.
 *
 * The control plane has no Git host and no agent in CI, so a real preview
 * delivery cannot be produced here; the binding listing is mocked at the API
 * boundary and the assertions are about the rendered contract (PR, branch,
 * clickable host, state) and the feature-flag behaviour (FEATURE_PREVIEWS=false
 * hides the tab instead of erroring).
 *
 * The API-level preview lifecycle is covered by the gated internal/e2e tests.
 */
test.describe("previews tab", () => {
  // The feature-off half provokes a 404 on purpose (that is what
  // FEATURE_PREVIEWS=false answers); the browser logs it, the app must not.
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

    await page.goto(`/applications/${application.id}`);

    const previewsTab = page.locator(".n-tabs-tab").filter({ hasText: "Previews" });
    await expect(previewsTab).toContainText("Previews (1)");
    await previewsTab.click();

    const row = page.locator("tr").filter({ hasText: "#482" });
    await expect(row).toBeVisible();
    await expect(row).toContainText(branch);
    await expect(row).toContainText("active");
    // The preview is HTTP-only; the link is the real address, not HTTPS.
    await expect(row.locator(`a[href="http://${host}"]`)).toBeVisible();

    // ── FEATURE_PREVIEWS=false: the tab disappears, no error is rendered ──
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
      page.getByText("Preview deployments are not enabled"),
    ).toHaveCount(0);

    expect(
      guardrails.apiFailures.filter((line) => !line.includes("/previews")),
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});

/**
 * FE-8.1 (2) — teams, members and invites, against the real API.
 *
 * Covers the owner happy path end to end: create, invite (and read the one-time
 * token from the UI), accept as the invited account, change the role, remove
 * the member, delete the team. The personal team's delete is asserted to be
 * locked, since the backend refuses it.
 */
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

    // A second real account plays the invitee; the invite is bound to its
    // email, so it cannot be accepted by the smoke account itself.
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

    // ── create ────────────────────────────────────────────────────────────
    await page.getByRole("button", { name: "New team" }).click();
    const createModal = page.locator(".n-modal").filter({ hasText: "New team" });
    await createModal.getByLabel("Team name").locator("input").fill(teamName);
    await createModal.getByRole("button", { name: "Create team" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);

    const teamRow = page.locator(`[data-team="${teamName}"]`);
    await expect(teamRow).toBeVisible();

    // ── the personal team cannot be deleted ───────────────────────────────
    const personalRow = page.locator("[data-team]").filter({ hasText: "personal" });
    await expect(personalRow).toHaveCount(1);
    await expect(personalRow.getByRole("button", { name: "Delete" })).toBeDisabled();

    // ── select the new team; members show the caller as owner ─────────────
    await teamRow.getByRole("button", { name: teamName }).click();
    const account = loadAccount();
    await expect(page.locator("tr:visible").filter({ hasText: account.email })).toBeVisible();

    // ── invite: the one-time link/token is shown and never persisted ──────
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

    // ── the invited account accepts the token ─────────────────────────────
    const accepted = await request.post("/api/v1/invites/accept", {
      headers: { Authorization: `Bearer ${invitee.access_token}` },
      data: { token },
    });
    expect(accepted.status(), await accepted.text()).toBe(200);

    // ── members: change the role, then remove ─────────────────────────────
    await page.locator(".n-tabs-tab").filter({ hasText: "Members" }).click();
    await page.getByRole("button", { name: "Refresh" }).click();
    const memberRow = page.locator("tr:visible").filter({ hasText: inviteeEmail });
    await expect(memberRow).toBeVisible();

    await memberRow.locator(".n-select").click();
    await page.locator(".n-base-select-option").filter({ hasText: "admin" }).click();
    await expect(memberRow.locator(".n-select")).toContainText("admin");
    await expect(page.locator('[data-testid="member-action-error"]')).toHaveCount(0);

    await memberRow.getByRole("button", { name: "Remove" }).click();
    await page.locator(".n-popconfirm").getByRole("button", { name: "Confirm" }).click();
    await expect(page.locator("tr:visible").filter({ hasText: inviteeEmail })).toHaveCount(0);

    // ── delete the team ───────────────────────────────────────────────────
    await teamRow.getByRole("button", { name: "Delete" }).click();
    await page.locator(".n-popconfirm").getByRole("button", { name: "Confirm" }).click();
    await expect(page.locator(`[data-team="${teamName}"]`)).toHaveCount(0);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});

/**
 * FE-8.1 (3) — notification channels against the real API.
 *
 * Secrets are proven write-only: the created webhook never appears in the DOM,
 * in browser storage or back on the edit form (which shows the read mask), and
 * a save that does not touch it keeps the stored value. The send-test uses a
 * refused loopback address, so its failure is deterministic and offline-safe.
 */
test.describe("notification channels", () => {
  test("creates a channel, keeps the secret masked, tests, toggles and deletes", async ({
    page,
    guardrails,
  }) => {
    const suffix = uniqueSuffix();
    const name = `ui-e2e-hook-${suffix}`;
    const webhook = `http://127.0.0.1:9/${name}`;
    const masked = `…${name.slice(-4)}`;

    await page.goto("/settings/notifications");
    await expect(
      page.getByRole("heading", { name: "Notification channels", level: 1 }),
    ).toBeVisible();

    // ── create ────────────────────────────────────────────────────────────
    await page.getByRole("button", { name: "New channel" }).click();
    const modal = page.locator(".n-modal").filter({ hasText: "New notification channel" });
    await modal.getByLabel("Channel name").locator("input").fill(name);
    await modal.getByLabel("Webhook URL").locator("input").fill(webhook);
    await modal.getByRole("button", { name: "Create channel" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);

    const card = page.locator(".n-card").filter({ hasText: name });
    await expect(card).toBeVisible();
    // The read view carries the mask and the "configured" indicator only.
    await expect(card).toContainText("secret configured");
    await expect(card).toContainText(masked);
    expect(await page.content()).not.toContain(webhook);
    const storage = await page.evaluate(() =>
      [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join("\n"),
    );
    expect(storage).not.toContain(webhook);

    // ── send test: the refusal is surfaced, not swallowed ─────────────────
    await card.getByRole("button", { name: "Send test" }).click();
    await expect(card.locator("[data-test-channel-result]")).toContainText("Test failed");

    // ── enable toggle ─────────────────────────────────────────────────────
    await page.getByRole("switch", { name: `Enable ${name}` }).click();
    await expect(card).toContainText("disabled");

    // ── edit: the masked secret stays masked and is not resubmitted ───────
    await card.getByRole("button", { name: "Edit" }).click();
    const editModal = page.locator(".n-modal").filter({ hasText: "Edit notification channel" });
    await expect(editModal.getByLabel("Webhook URL").locator("input")).toHaveValue(masked);
    await editModal.getByRole("button", { name: "Save" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);
    expect(await page.content()).not.toContain(webhook);
    await expect(page.locator(".n-card").filter({ hasText: name })).toContainText(masked);

    // ── delete ────────────────────────────────────────────────────────────
    await card.getByRole("button", { name: "Delete" }).click();
    await page.locator(".n-popconfirm").getByRole("button", { name: "Confirm" }).click();
    await expect(page.locator(".n-card").filter({ hasText: name })).toHaveCount(0);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});

/**
 * FE-8.1 (4) — the metrics tab of the server detail.
 *
 * First half runs against the real API: a freshly registered node has no
 * samples, so the charts must claim an empty window (never a fabricated
 * series). Second half mocks a payload with a real gap (the backend omits
 * empty buckets) and proves the chart breaks the line instead of bridging it,
 * and that switching the step issues a new range request.
 */
test.describe("server metrics", () => {
  test("renders the real empty window, then samples with gaps and a step switch", async ({
    page,
    request,
    guardrails,
  }) => {
    test.setTimeout(60_000);

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

    // ── a mocked payload with a gap in the middle of the window ───────────
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
          step: "1h",
          points: [
            point(0, 0.1),
            point(60_000, 0.2),
            point(120_000, 0.3),
            // A 8-minute hole: the next sample starts a new segment.
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

    // ── step switch issues the new range request ──────────────────────────
    await page.locator(".n-radio-button").filter({ hasText: "1h" }).click();
    await expect.poll(() => requestedSteps.includes("1h")).toBe(true);
    await expect(cpu.locator("polyline")).toHaveCount(1);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});
