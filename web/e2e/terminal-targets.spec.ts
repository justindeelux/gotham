import { expect, test } from "./fixtures";
import { loadAccount, storageStatePath, uniqueSuffix } from "./support";
import type { E2EAccount } from "./support";
import {
  authHeaders,
  installSession,
  loginFresh,
  pickServer,
  runCommand,
  screenText,
  skipOrFail,
  terminalPageURL,
  terminalServer,
  waitForCreate,
  watchTerminal,
} from "./terminal-support";
import type { APIRequestContext, Page } from "@playwright/test";

/**
 * JUS-265 add-tab picker on the REAL stack.
 *
 * A shared team with one prebuilt `nginx` application is created and deployed
 * through the public API against the same control plane and node agent the
 * browser talks to. The (+) control in the floating window and on the Terminal
 * page is then driven for real: the target list, the exact container create
 * payload, request counts, tab labels and isolation are asserted, while the
 * permission matrix and the empty/error states use real API responses and a
 * network-boundary failure (the sessions themselves stay real).
 */

test.use({
  storageState: storageStatePath,
  viewport: { width: 1366, height: 900 },
});

const account = loadAccount();
/** fresh is this file's own sign-in (see loginFresh). */
let fresh: Awaited<ReturnType<typeof loginFresh>>;

/** APP_IMAGE is pre-pulled by the UI E2E workflow. */
const APP_IMAGE = process.env.GOTHAM_E2E_TERMINAL_IMAGE ?? "nginx:1.28.0-alpine";

interface Seed {
  teamId: string;
  projectId: string;
  environmentId: string;
  applicationId: string;
  applicationName: string;
  containerId: string;
  service: string;
  containerLabel: string;
}

let seed: Seed | null = null;

test.beforeAll(async ({ request }) => {
  fresh = await loginFresh(request, account);
  // One deployment serves every scenario in this file; the team is seeded
  // before the first navigation so the picker reads the right team.
  seed = await seedTeamApplication(request);
});

test.beforeEach(async ({ page }) => {
  await installSession(page, fresh);
  if (seed !== null) {
    // The picker is team-scoped; make the seeded team the active one.
    await page.addInitScript((teamId) => {
      window.localStorage.setItem("gotham.teams.active", teamId);
    }, seed.teamId);
  }
});

test.afterAll(async ({ request }) => {
  if (seed === null) {
    return;
  }
  const headers = teamHeaders(seed.teamId, account);
  await request
    .delete(`/api/v1/applications/${seed.applicationId}`, { headers })
    .catch(() => undefined);
  await request
    .delete(`/api/v1/projects/${seed.projectId}`, { headers })
    .catch(() => undefined);
  await request
    .delete(`/api/v1/teams/${seed.teamId}`, { headers: authHeaders(account) })
    .catch(() => undefined);
  seed = null;
});

/** teamHeaders scopes one API call to a team. */
function teamHeaders(teamId: string, who: E2EAccount): Record<string, string> {
  const headers = authHeaders(who);
  if (teamId !== "") {
    headers["X-Team-Id"] = teamId;
  }
  return headers;
}

/** expectOK fails with the body so a seed failure is debuggable. */
async function expectOK(
  response: { ok(): boolean; status(): number; text(): Promise<string> },
  what: string,
): Promise<void> {
  if (!response.ok()) {
    throw new Error(`${what}: ${response.status()} ${await response.text()}`);
  }
}

/** requireTerminalNode is the CI interlock (missing node = failure in CI). */
async function requireTerminalNode(request: APIRequestContext) {
  const server = await terminalServer(request, account);
  skipOrFail(
    server === null,
    "no ready node agent is registered (the UI E2E workflow starts one)",
    test.skip,
  );
  return server!;
}

/** seedTeamApplication creates a team, project, image application and deploy. */
async function seedTeamApplication(request: APIRequestContext): Promise<Seed> {
  const server = await requireTerminalNode(request);
  const owner = authHeaders(account);

  const teamResponse = await request.post("/api/v1/teams", {
    headers: owner,
    data: { name: `ui-e2e-tab-${uniqueSuffix()}` },
  });
  await expectOK(teamResponse, "seed team");
  const teamId = ((await teamResponse.json()) as { team: { id: string } }).team.id;
  const headers = teamHeaders(teamId, account);

  const projectResponse = await request.post("/api/v1/projects", {
    headers,
    data: { name: `ui-e2e-tab-${uniqueSuffix()}` },
  });
  await expectOK(projectResponse, "seed project");
  const project = (await projectResponse.json()) as {
    project: { id: string };
    environments: Array<{ id: string }>;
  };
  const environmentId = project.environments[0]!.id;
  const applicationName = `ui-e2e-tab-${uniqueSuffix()}`.slice(0, 31);

  const appResponse = await request.post("/api/v1/applications", {
    headers,
    data: {
      environment_id: environmentId,
      server_id: server.id,
      name: applicationName,
      source_type: "image",
      image_ref: APP_IMAGE,
    },
  });
  await expectOK(appResponse, "seed application");
  const applicationId = (
    (await appResponse.json()) as { application: { id: string } }
  ).application.id;

  const deploy = await request.post(
    `/api/v1/applications/${applicationId}/deploy`,
    { headers },
  );
  await expectOK(deploy, "deploy application");
  let target = { service: "", container_id: "", label: "" };
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    const list = await request.get(
      `/api/v1/applications/${applicationId}/deployments`,
      { headers },
    );
    await expectOK(list, "list deployments");
    const body = (await list.json()) as {
      deployments: Array<{ state: string; error?: string }>;
    };
    const latest = body.deployments[0];
    if (latest?.state === "failed") {
      throw new Error(`deploy failed: ${latest.error ?? "unknown error"}`);
    }
    if (latest?.state === "running") {
      const targets = await request.get(
        `/api/v1/applications/${applicationId}/terminal/targets`,
        { headers },
      );
      await expectOK(targets, "resolve terminal targets");
      const payload = (await targets.json()) as {
        containers: Array<{ service: string; container_id: string; label: string }>;
      };
      const first = payload.containers[0];
      if (first === undefined) {
        throw new Error("the running application resolved no container target");
      }
      target = {
        service: first.service,
        container_id: first.container_id,
        label: first.label,
      };
      break;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  if (target.container_id === "") {
    throw new Error("deployment never reached running");
  }
  return {
    teamId,
    projectId: project.project.id,
    environmentId,
    applicationId,
    applicationName,
    containerId: target.container_id,
    service: target.service,
    containerLabel: target.label,
  };
}

/** openPicker clicks the (+) control and waits for the list. */
async function openPicker(page: Page, surface: "window" | "page"): Promise<void> {
  const scope = surface === "window" ? ".term-window" : ".term-page";
  await page.locator(`${scope} .term-add-tab`).click();
  await expect(page.locator(".term-targets")).toBeVisible();
}

/** pickTarget clicks one option by its visible label. */
async function pickTarget(page: Page, label: string): Promise<void> {
  await page
    .locator(".term-target-option")
    .filter({ hasText: label })
    .first()
    .click();
  await expect(page.locator(".term-targets")).toHaveCount(0);
}

/** activeWindowScreen reads only the selected window tab's buffer. */
async function activeWindowScreen(page: Page): Promise<string> {
  const panel = await page
    .locator(".term-tab-name[aria-selected='true']")
    .getAttribute("aria-controls");
  if (panel === null) {
    throw new Error("the selected window tab has no panel");
  }
  return screenText(page, `#${panel}`);
}

/** watchTargetReads counts GET /terminal/targets and records the team header. */
function watchTargetReads(page: Page): string[] {
  const reads: string[] = [];
  page.on("request", (request) => {
    if (
      request.method() === "GET" &&
      new URL(request.url()).pathname === "/api/v1/terminal/targets"
    ) {
      reads.push(request.headers()["x-team-id"] ?? "");
    }
  });
  return reads;
}

test.describe.serial("add-tab picker on the real stack", () => {
  test("windows and pages open host and container sessions in isolated tabs", async ({
    page,
    request,
    context,
  }) => {
    test.setTimeout(240_000);
    const server = (await requireTerminalNode(request))!;
    const traffic = watchTerminal(page);
    const reads = watchTargetReads(page);

    // One host page session from the empty-state server cards.
    await page.goto(terminalPageURL);
    await expect(
      page.getByRole("heading", { name: "Terminal", level: 1 }),
    ).toBeVisible();
    await pickServer(page, server.name);
    await waitForCreate(traffic, 1);
    await expect(page.locator(".term-page-tab")).toHaveCount(1);

    const marker = "TAB-HOST-" + seed.containerId.slice(0, 6);
    await runCommand(context, page, `echo ${marker}`, ".term-page");
    await expect
      .poll(() => screenText(page, ".term-page").then((text) => text.includes(marker)), {
        timeout: 30_000,
      })
      .toBe(true);

    // The same session moves into the window (no new create).
    await page.getByRole("button", { name: "Open in window" }).first().click();
    await expect(page.locator(".term-window")).toBeVisible();
    expect(traffic.createCount()).toBe(1);

    // Window (+): the list carries the ready host and the seeded container,
    // scoped to the active team.
    await openPicker(page, "window");
    expect(reads).toEqual([seed.teamId]);
    await expect(page.locator(".term-targets-group-title")).toContainText([
      "Servers",
      seed.applicationName,
    ]);
    await expect(
      page.locator(".term-target-option").filter({ hasText: server.name }),
    ).toBeVisible();
    await pickTarget(page, seed.containerLabel);

    const containerCreate = await waitForCreate(traffic, 2);
    expect(containerCreate.status).toBe(201);
    expect(containerCreate.url).toContain(
      `/api/v1/applications/${seed.applicationId}/terminal/sessions`,
    );
    expect(containerCreate.body).toMatchObject({
      service: seed.service,
      container_id: seed.containerId,
    });
    expect(String(containerCreate.body?.container_id)).toMatch(/^[0-9a-f]{64}$/);
    await expect(page.locator(".term-tab")).toHaveCount(2);
    await expect(page.locator(".term-tab").nth(1)).toContainText(seed.containerLabel);

    // Tab isolation: the host tab keeps its buffer and socket; the container
    // tab has a real prompt and no host banner.
    await page.locator(".term-tab-name").nth(0).click();
    await expect
      .poll(() => activeWindowScreen(page).then((text) => text.includes(marker)), {
        timeout: 30_000,
      })
      .toBe(true);
    await page.locator(".term-tab-name").nth(1).click();
    await expect
      .poll(() => activeWindowScreen(page).then((text) => /[#$]/.test(text)), {
        timeout: 30_000,
      })
      .toBe(true);
    expect(await activeWindowScreen(page)).not.toContain("server: ");

    // The Terminal page gets its own tab strip: open a host and a container
    // page session through the same (+) control. The window keeps its two.
    await page.locator("#app-nav").getByRole("link", { name: "Dashboard" }).click();
    await expect(page).toHaveURL(/\/dashboard$/);
    await page.locator("#app-nav").getByRole("link", { name: "Terminal" }).click();
    await expect(page).toHaveURL(new RegExp(`${terminalPageURL}$`));
    await expect(page.locator(".term-page-tab")).toHaveCount(0);

    await openPicker(page, "page");
    await pickTarget(page, server.name);
    await waitForCreate(traffic, 3);
    await expect(page.locator(".term-page-tab")).toHaveCount(1);

    await openPicker(page, "page");
    await pickTarget(page, seed.containerLabel);
    const pageContainerCreate = await waitForCreate(traffic, 4);
    expect(pageContainerCreate.url).toContain(
      `/api/v1/applications/${seed.applicationId}/terminal/sessions`,
    );
    expect(pageContainerCreate.body).toMatchObject({
      service: seed.service,
      container_id: seed.containerId,
    });
    await expect(page.locator(".term-page-tab")).toHaveCount(2);
    await expect(page.locator(".term-page-tab").nth(1)).toContainText(
      seed.containerLabel,
    );

    // Page tab isolation: the host tab paints its own banner/prompt and the
    // container tab its busybox prompt; neither reconnects.
    await page.locator(".term-page-tab-name").nth(0).click();
    await expect
      .poll(
        () => screenText(page, ".term-page").then((text) => text.includes("server: ")),
        { timeout: 30_000 },
      )
      .toBe(true);
    await page.locator(".term-page-tab-name").nth(1).click();
    await expect
      .poll(() => screenText(page, ".term-page").then((text) => /[#$]/.test(text)), {
        timeout: 30_000,
      })
      .toBe(true);
    expect(reads).toHaveLength(3);
    expect(reads).toEqual([seed.teamId, seed.teamId, seed.teamId]);
    expect(traffic.createCount()).toBe(4);
    expect(traffic.deletes).toHaveLength(0);
  });

  test("permission matrix: a member gets no hosts and a read-only viewer no containers", async ({
    request,
  }) => {
    test.setTimeout(120_000);
    skipOrFail(
      process.env.GOTHAM_AUTH_ALLOW_REGISTRATION !== "true",
      "registration is closed, so the permission accounts cannot be created",
      test.skip,
    );

    // One extra account serves both denial cases: it is not a platform
    // operator and it is invited to the seeded team as read_only.
    const viewerEmail = `ui-e2e-tab-viewer-${uniqueSuffix()}@example.com`;
    const viewerResponse = await request.post("/api/v1/auth/register", {
      data: { email: viewerEmail, password: "Gotham-E2E-Password1" },
    });
    await expectOK(viewerResponse, "register viewer");
    const viewer = (await viewerResponse.json()) as { access_token: string };

    // Non-operator, personal scope: no host servers, and no 403 oracle.
    const memberTargets = await request.get("/api/v1/terminal/targets", {
      headers: { Authorization: `Bearer ${viewer.access_token}` },
    });
    expect(memberTargets.status()).toBe(200);
    const memberBody = (await memberTargets.json()) as {
      servers: unknown[];
      applications: unknown[];
      errors: unknown[];
    };
    expect(memberBody.servers).toEqual([]);
    expect(memberBody.applications).toEqual([]);
    expect(memberBody.errors).toEqual([]);

    // The operator in the seeded team sees the ready host and the container.
    const ownerTargets = await request.get("/api/v1/terminal/targets", {
      headers: teamHeaders(seed.teamId, account),
    });
    expect(ownerTargets.status()).toBe(200);
    const ownerBody = (await ownerTargets.json()) as {
      servers: Array<{ id: string }>;
      applications: Array<{ application_id: string }>;
      errors: unknown[];
    };
    expect(ownerBody.servers.length).toBeGreaterThan(0);
    expect(ownerBody.applications.map((group) => group.application_id)).toContain(
      seed.applicationId,
    );

    // The same account as a read-only team member: it reads the team but may
    // not open a shell — no server (not an operator) and no container (no
    // write), and no error row that would leak the application's existence.
    const invite = await request.post(`/api/v1/teams/${seed.teamId}/invites`, {
      headers: authHeaders(account),
      data: { email: viewerEmail, role: "read_only" },
    });
    await expectOK(invite, "invite viewer");
    const token = ((await invite.json()) as { invite: { token: string } }).invite
      .token;
    const accept = await request.post("/api/v1/invites/accept", {
      headers: { Authorization: `Bearer ${viewer.access_token}` },
      data: { token },
    });
    await expectOK(accept, "accept invite");

    const viewerTargets = await request.get("/api/v1/terminal/targets", {
      headers: {
        Authorization: `Bearer ${viewer.access_token}`,
        "X-Team-Id": seed.teamId,
      },
    });
    expect(viewerTargets.status()).toBe(200);
    const viewerBody = (await viewerTargets.json()) as {
      servers: unknown[];
      applications: unknown[];
      errors: unknown[];
    };
    expect(viewerBody.servers).toEqual([]);
    expect(viewerBody.applications).toEqual([]);
    expect(viewerBody.errors).toEqual([]);
  });

  test("the picker renders the real empty list and retries a failed read", async ({
    page,
    request,
  }) => {
    test.setTimeout(180_000);
    const server = (await requireTerminalNode(request))!;
    const traffic = watchTerminal(page);

    await page.goto(terminalPageURL);
    await pickServer(page, server.name);
    await waitForCreate(traffic, 1);
    await page.getByRole("button", { name: "Open in window" }).first().click();
    await expect(page.locator(".term-window")).toBeVisible();

    // Empty: the control plane's real envelope with no targets. Only the read
    // is stubbed; the window/session stay real.
    await page.route("**/api/v1/terminal/targets", async (route) => {
      await route.fulfill({
        json: { servers: [], applications: [], errors: [] },
      });
    });
    await openPicker(page, "window");
    await expect(page.locator(".term-targets-empty")).toHaveText(
      "No terminal targets available",
    );
    await page.keyboard.press("Escape");
    await expect(page.locator(".term-targets")).toHaveCount(0);
    await page.unroute("**/api/v1/terminal/targets");

    // Error: a 503 at the network boundary renders the localized retry state,
    // never the backend body. Retry then reads the real list.
    await page.route("**/api/v1/terminal/targets", async (route) => {
      await route.fulfill({
        status: 503,
        json: { message: "node agent is unavailable" },
      });
    });
    await openPicker(page, "window");
    await expect(page.locator(".term-targets-error")).toContainText(
      "The target list is unavailable right now.",
    );
    await expect(page.locator(".term-targets-error")).not.toContainText(
      "node agent is unavailable",
    );
    await page.unroute("**/api/v1/terminal/targets");
    await page
      .locator(".term-targets")
      .getByRole("button", { name: "Retry" })
      .click();
    await expect(page.locator(".term-targets-group-title")).toContainText([
      "Servers",
      seed.applicationName,
    ]);
    expect(traffic.createCount()).toBe(1);
    expect(traffic.deletes).toHaveLength(0);
  });
});

