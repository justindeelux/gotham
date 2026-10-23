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

test.use({
  storageState: storageStatePath,
  viewport: { width: 1366, height: 900 },
});

const account = loadAccount();
let fresh: Awaited<ReturnType<typeof loginFresh>>;

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
  seed = await seedTeamApplication(request);
});

test.beforeEach(async ({ page }) => {
  await installSession(page, fresh);
  if (seed !== null) {
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

function teamHeaders(teamId: string, who: E2EAccount): Record<string, string> {
  const headers = authHeaders(who);
  if (teamId !== "") {
    headers["X-Team-Id"] = teamId;
  }
  return headers;
}

async function expectOK(
  response: { ok(): boolean; status(): number; text(): Promise<string> },
  what: string,
): Promise<void> {
  if (!response.ok()) {
    throw new Error(`${what}: ${response.status()} ${await response.text()}`);
  }
}

async function requireTerminalNode(request: APIRequestContext) {
  const server = await terminalServer(request, account);
  skipOrFail(
    server === null,
    "no ready node agent is registered (the UI E2E workflow starts one)",
    test.skip,
  );
  return server!;
}

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

async function openPicker(page: Page, surface: "window" | "page"): Promise<void> {
  const scope = surface === "window" ? ".term-window" : ".term-page";
  await page.locator(`${scope} .term-add-tab`).click();
  await expect(page.locator(".term-targets")).toBeVisible();
}

async function pickTarget(page: Page, label: string): Promise<void> {
  await page
    .locator(".term-target-option")
    .filter({ hasText: label })
    .first()
    .click();
  await expect(page.locator(".term-targets")).toHaveCount(0);
}

async function activeWindowScreen(page: Page): Promise<string> {
  const panel = await page
    .locator(".term-tab-name[aria-selected='true']")
    .getAttribute("aria-controls");
  if (panel === null) {
    throw new Error("the selected window tab has no panel");
  }
  return screenText(page, `#${panel}`);
}

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

    await page.getByRole("button", { name: "Open in window" }).first().click();
    await expect(page.locator(".term-window")).toBeVisible();
    expect(traffic.createCount()).toBe(1);

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
    expect(await screenText(page, ".term-page")).not.toContain("server: ");
    expect(reads).toHaveLength(3);
    expect(reads).toEqual([seed.teamId, seed.teamId, seed.teamId]);
    expect(traffic.createCount()).toBe(4);
    expect(traffic.deletes).toHaveLength(0);
  });

  test("the picker is fully keyboard reachable from the (+) trigger", async ({
    page,
    request,
  }) => {
    test.setTimeout(180_000);
    const server = (await requireTerminalNode(request))!;
    const traffic = watchTerminal(page);
    const reads = watchTargetReads(page);

    await page.goto(terminalPageURL);
    await pickServer(page, server.name);
    await waitForCreate(traffic, 1);
    await page.getByRole("button", { name: "Open in window" }).first().click();
    await expect(page.locator(".term-window")).toBeVisible();

    const trigger = page.locator(".term-window .term-add-tab");
    await trigger.focus();
    await page.keyboard.press("Enter");
    await expect(page.locator(".term-targets")).toBeVisible();
    await expect(page.locator(".term-targets input").first()).toBeFocused();
    await page.keyboard.type(seed!.containerLabel);
    await expect(page.locator(".term-target-option")).toHaveCount(1);
    await page.keyboard.press("ArrowDown");
    await expect(page.locator(".term-target-option")).toBeFocused();
    await page.keyboard.press("Enter");
    const created = await waitForCreate(traffic, 2);
    expect(created.body).toMatchObject({
      service: seed!.service,
      container_id: seed!.containerId,
    });
    await expect(page.locator(".term-tab")).toHaveCount(2);

    await trigger.focus();
    await page.keyboard.press("Space");
    await expect(page.locator(".term-targets")).toBeVisible();
    await expect(page.locator(".term-targets input").first()).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(page.locator(".term-targets")).toHaveCount(0);
    await expect(trigger).toBeFocused();
    expect(traffic.createCount()).toBe(2);
    expect(reads).toEqual([seed!.teamId, seed!.teamId]);
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

    const viewerEmail = `ui-e2e-tab-viewer-${uniqueSuffix()}@example.com`;
    const viewerResponse = await request.post("/api/v1/auth/register", {
      data: { email: viewerEmail, password: "Gotham-E2E-Password1" },
    });
    await expectOK(viewerResponse, "register viewer");
    const viewer = (await viewerResponse.json()) as { access_token: string };

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

    await page.route("**/api/v1/terminal/targets", async (route) => {
      await route.fulfill({
        json: { servers: [], applications: [], errors: [], truncated: false },
      });
    });
    await openPicker(page, "window");
    await expect(page.locator(".term-targets-empty")).toHaveText(
      "No terminal targets available",
    );
    await page.keyboard.press("Escape");
    await expect(page.locator(".term-targets")).toHaveCount(0);
    await page.unroute("**/api/v1/terminal/targets");

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

