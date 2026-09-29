import type { APIRequestContext, Page, Route } from "@playwright/test";

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
    // The mock echoes the requested step: the applied step is what defines a
    // gap, so a step-1m payload keeps its 8-minute hole.
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
            // A 8-minute hole: the next sample starts a new segment at step 1m.
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

// ─────────────────────────────────────────────────────────────────────────────
// Fix round 1 regressions: team-context races, metrics-range races and the
// invitation redirect across registration.
// ─────────────────────────────────────────────────────────────────────────────

/** seedTeam creates a team owned by the smoke account. */
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

/** seedInvite issues a pending invite and returns its one-time token. */
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

/** acceptInviteAs consumes an invite token as the invited account. */
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

/**
 * One extra account shared by the race scenarios. Registering is rate-limited
 * per IP, so the account is created once per run and reused.
 */
let sharedInvitee: { email: string; accessToken: string } | null = null;

/** ensureInvitee registers (once) the account used as a second team member. */
async function ensureInvitee(
  request: APIRequestContext,
): Promise<{ email: string; accessToken: string }> {
  if (sharedInvitee) {
    return sharedInvitee;
  }
  const email = `ui-e2e-race-${uniqueSuffix()}@example.com`;
  const response = await request.post("/api/v1/auth/register", {
    data: { email, password: "Gotham-E2E-Password1" },
  });
  expect(response.status(), await response.text()).toBe(200);
  const body = (await response.json()) as { access_token: string };
  sharedInvitee = { email, accessToken: body.access_token };
  return sharedInvitee;
}

/** seedChannel stores a Discord channel in one team. */
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

/** settle lets Vue flush the reactive writes a just-landed response queued. */
async function settle(page: Page): Promise<void> {
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
      }),
  );
}

/** selectTeamInScope picks a team in the notifications team-scope select. */
async function selectTeamInScope(page: Page, teamName: string): Promise<void> {
  const select = page.locator('[aria-label="Notification team"]');
  await select.click();
  // The select is filterable: filling narrows the (virtualized) option list to
  // the named team, which a plain click could not reach below the fold.
  // fill() replaces any filter text left from a previous selection.
  await select.locator("input").fill(teamName);
  await page.locator(".n-base-select-option").filter({ hasText: teamName }).click();
}

/**
 * F1 (fix round 1) — a delayed read for the previously selected team must not
 * overwrite the newly selected team's collections.
 *
 * The bug: member/invite reads captured a team ID and applied their response
 * unconditionally, so holding team A's reads, switching to B and releasing A
 * rendered A's rows under B. The regression holds A, switches to B, releases A
 * and asserts B's collections are untouched.
 */
test.describe("team context races", () => {
  // The failed-read scenario aborts two requests on purpose; the browser logs
  // the transport failure, the app must render its own error state.
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

    // Positive control: the held team A payload really carries a second member
    // and a pending invite, so applying it under B would be visible.
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

    // Team A is selected first (its reads are held), then team B, which
    // renders its own collections: the operator only, and no invites.
    await rowA.getByRole("button", { name: teamA.name }).click();
    await rowB.getByRole("button", { name: teamB.name }).click();
    await expect(rowB).toHaveClass(/is-selected/);
    await expect(
      page.locator(".n-tab-pane tr").filter({ hasText: loadAccount().email }),
    ).toHaveCount(1);
    await expect(
      page.locator(".n-tab-pane tr").filter({ hasText: invitee.email }),
    ).toHaveCount(0);

    // Release A: both late payloads land after B is selected.
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

    // The stale A payloads must not touch B's collections.
    await expect(
      page.locator(".n-tab-pane tr").filter({ hasText: invitee.email }),
    ).toHaveCount(0);
    await expect(
      page.locator(".n-tab-pane tr").filter({ hasText: loadAccount().email }),
    ).toHaveCount(1);

    // A's pending invite must not surface in B's (empty) invites pane either.
    await page.locator(".n-tabs-tab").filter({ hasText: "Invites" }).click();
    await expect(
      page.locator(".n-tab-pane tr").filter({ hasText: pendingEmail }),
    ).toHaveCount(0);

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
    // Team A is loaded: its second member is on screen.
    await expect(
      page.locator(".n-tab-pane tr").filter({ hasText: invitee.email }),
    ).toBeVisible();

    // Team B's reads fail; switching to it must not keep A's rows.
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
    await expect(
      page.locator(".n-tab-pane tr").filter({ hasText: invitee.email }),
    ).toHaveCount(0);

    // The invites pane is mounted only when its tab is opened; its own failed
    // read surfaces the same way, with A's invites still dropped.
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

/**
 * F1 (fix round 1) — notification channels are scoped to the selected team.
 *
 * A delayed channel list for the previously selected team must not render
 * under the new selection, and a failed read for the new team must leave the
 * previous team's cards dropped rather than retained (or shown as empty).
 */
test.describe("notification channel team races", () => {
  // The failed-read half aborts one request on purpose; the browser logs the
  // transport failure, the app must render its own error state.
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

    // Team A's read is held; team B loads and renders only its own channel.
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

    // A is selected again and renders its card; then B's read fails.
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

/**
 * F2 (fix round 1) — a delayed range response must not overwrite the selected
 * range, and a failed switch must not keep the old series.
 *
 * The bug: every metrics response was committed unconditionally and the chart
 * used the current selection's step, so a delayed 1m response overwrote a 1h
 * window and its gaps were read at the wrong width. The regression holds the
 * initial 1m read, switches to 1h, releases the minute payload, and asserts the
 * hour's data and step still hold.
 */
test.describe("metrics range races", () => {
  // The failed switch aborts the 1d request on purpose; the browser logs the
  // transport failure, the app must render its own error state.
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
    // The minute payload's two points are 2 minutes apart: a gap at step 1m,
    // one contiguous line at step 1h. The hour payload ends at 80%.
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
      // The 1d switch fails.
      await route.abort("failed");
    });

    await page.goto(`/servers/${node.id}`);
    await page.locator(".n-tabs-tab").filter({ hasText: "Metrics" }).click();
    await expect(page.locator('[data-chart="CPU"]')).toContainText(
      "Loading the metrics window…",
    );

    // Switch to 1h while the initial 1m read is still in flight.
    await page.locator(".n-radio-button").filter({ hasText: "1h" }).click();
    const cpuCard = page
      .locator(".n-card")
      .filter({ has: page.locator('[data-chart="CPU"]') });
    const cpu = page.locator('[data-chart="CPU"]');
    await expect(cpuCard).toContainText("80%");

    // Release the minute payload; the hour's data and its step must hold.
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

    // A failed switch must not keep the previous series under the new label.
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

/**
 * FEATURE_TEAMS=false unmounts the team routes. The page must render the
 * unavailable state without its management actions — a guest 404 on the list
 * is not an error.
 */
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
      page.getByText("Team management is not enabled on this control plane"),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "New team" })).toHaveCount(0);
  });
});

/**
 * F3 (fix round 1) — a first-time invite recipient registers and still accepts.
 *
 * The auth guard sends a signed-out visitor to `/login?redirect=<invite>`, but
 * the sign-in page's "Create account" tab dropped the query, so registering
 * landed on the dashboard. The regression walks the signed-out invite link
 * through registration and asserts the invitation is accepted (and the spent
 * token is scrubbed from the URL).
 */
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

    // The signed-out visitor opens the one-time link and is sent to sign in.
    await page.goto(`/invite/accept?token=${encodeURIComponent(token)}`);
    await expect(page).toHaveURL(/\/login\?redirect=/);

    // Switching to registration keeps the invitation in the redirect. The
    // auth switch renders as tabs, not links.
    await page.getByRole("tab", { name: "Create account" }).click();
    await expect(page).toHaveURL(/\/register\?redirect=/);
    expect(decodeURIComponent(new URL(page.url()).search)).toContain(
      `/invite/accept?token=${token}`,
    );

    // Registering with the invited address must land back on the invite.
    await page.getByPlaceholder("you@gotham.dev").fill(email);
    await page.getByPlaceholder("At least 10 characters").fill("Gotham-E2E-Password1");
    await page.getByPlaceholder("Repeat your password").fill("Gotham-E2E-Password1");
    await page.getByRole("checkbox").check();
    await page.getByRole("button", { name: "Create account", exact: true }).click();

    await expect(page.getByText(`You joined ${team.name}`)).toBeVisible();
    // The spent token is scrubbed from the address bar.
    await expect(page).not.toHaveURL(/token=/);

    // The account really joined the team.
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
});
