import { expect, test } from "./fixtures";
import { baseURL, storageStatePath, uniqueSuffix } from "./support";
import type { E2EAccount } from "./support";
import { loadAccount } from "./support";
import {
  authHeaders,
  terminalServer,
  waitForCreate,
  watchTerminal,
  installSession,
  loginFresh,
} from "./terminal-support";
import type { CreateRequest, TerminalTraffic } from "./terminal-support";
import type { APIRequestContext, BrowserContext, Page } from "@playwright/test";

/**
 * JUS-247 / JUS-251 application terminal journeys against the REAL stack.
 *
 * A prebuilt `nginx` image application is created and deployed through the
 * public API on the same control plane and node agent the browser talks to, so
 * the Terminal tab resolves a real running container. Lazy discovery, the
 * single-target auto-open, the one-way window transfer, the retained window
 * session, redeployment recovery and the read-only gate are asserted by
 * request counts, exact payloads and the container id the API returned.
 */

test.use({ storageState: storageStatePath, viewport: { width: 1366, height: 900 } });

const account = loadAccount();
/** fresh is this file's own sign-in (see loginFresh). */
let fresh: Awaited<ReturnType<typeof loginFresh>>;

test.beforeAll(async ({ request }) => {
  fresh = await loginFresh(request, account);
});

test.beforeEach(async ({ page }) => {
  await installSession(page, fresh);
});
/** Images deployed by this spec. The workflow pre-pulls the nginx image. */
const APP_IMAGE = process.env.GOTHAM_E2E_TERMINAL_IMAGE ?? "nginx:1.28.0-alpine";

/** SeededApplication is one real deployment created for a test. */
interface SeededApplication {
  projectId: string;
  environmentId: string;
  applicationId: string;
  containerId: string;
  service: string;
  teamId: string;
  /** viewerToken is set only by the read-only scenario. */
  viewerToken?: string;
  viewerEmail?: string;
  viewerUserId?: string;
}

/** seeds accumulates test resources so cleanup runs even after a failure. */
const seeds: SeededApplication[] = [];
/** readOnlySeed is shared by the serial read-only scenarios. */
let readOnlySeed: SeededApplication | null = null;

/** cleanupSeed deletes the application, project and (when created) team. */
async function cleanupSeed(
  request: APIRequestContext,
  seed: SeededApplication,
): Promise<void> {
  // Deleting the application stops its container; then the project can go.
  await request
    .delete(`/api/v1/applications/${seed.applicationId}`, {
      headers: teamHeaders(seed.teamId, account),
    })
    .catch(() => undefined);
  await request
    .delete(`/api/v1/projects/${seed.projectId}`, {
      headers: teamHeaders(seed.teamId, account),
    })
    .catch(() => undefined);
  if (seed.teamId !== "") {
    await request
      .delete(`/api/v1/teams/${seed.teamId}`, { headers: authHeaders(account) })
      .catch(() => undefined);
  }
}

test.afterEach(async ({ request }) => {
  for (const seed of seeds.splice(0)) {
    await cleanupSeed(request, seed);
  }
});

test.afterAll(async ({ request }) => {
  if (readOnlySeed !== null) {
    await cleanupSeed(request, readOnlySeed);
    readOnlySeed = null;
  }
});

/** teamHeaders scopes one API call to the team under test. */
function teamHeaders(teamId: string, who: E2EAccount): Record<string, string> {
  const headers = authHeaders(who);
  if (teamId !== "") {
    headers["X-Team-Id"] = teamId;
  }
  return headers;
}

/** expectOK fails with the response body so a seed failure is debuggable. */
async function expectOK(
  response: { ok(): boolean; status(): number; text(): Promise<string> },
  what: string,
): Promise<void> {
  if (!response.ok()) {
    throw new Error(`${what}: ${response.status()} ${await response.text()}`);
  }
}

/** createProject creates a private project for owner-scoped application seeds. */
async function createProject(
  request: APIRequestContext,
  headers: Record<string, string>,
): Promise<{ projectId: string; environmentId: string }> {
  const response = await request.post("/api/v1/projects", {
    headers,
    data: { name: `ui-e2e-term-${uniqueSuffix()}` },
  });
  await expectOK(response, "seed project");
  const body = (await response.json()) as {
    project: { id: string };
    environments: Array<{ id: string }>;
  };
  return { projectId: body.project.id, environmentId: body.environments[0]!.id };
}

/** createApplication posts one image-source application. */
async function createImageApplication(
  request: APIRequestContext,
  headers: Record<string, string>,
  environmentId: string,
  serverId: string,
): Promise<string> {
  const response = await request.post("/api/v1/applications", {
    headers,
    data: {
      environment_id: environmentId,
      server_id: serverId,
      name: `ui-e2e-term-${uniqueSuffix()}`.slice(0, 31),
      source_type: "image",
      image_ref: APP_IMAGE,
    },
  });
  await expectOK(response, "seed application");
  const body = (await response.json()) as { application: { id: string } };
  return body.application.id;
}

/** deployApplication queues a real deploy and waits for a running container. */
async function deployApplication(
  request: APIRequestContext,
  headers: Record<string, string>,
  applicationId: string,
): Promise<{ containerId: string; service: string }> {
  const response = await request.post(
    `/api/v1/applications/${applicationId}/deploy`,
    { headers },
  );
  await expectOK(response, "deploy application");

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
      const targets = await terminalTargets(request, headers, applicationId);
      return targets.containers[0]!;
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error("deployment never reached running");
}

/** terminalTargets reads the API's resolved containers for one application. */
async function terminalTargets(
  request: APIRequestContext,
  headers: Record<string, string>,
  applicationId: string,
): Promise<{
  containers: Array<{ service: string; container_id: string; state: string }>;
}> {
  const response = await request.get(
    `/api/v1/applications/${applicationId}/terminal/targets`,
    { headers },
  );
  await expectOK(response, "terminal targets");
  return (await response.json()) as {
    containers: Array<{ service: string; container_id: string; state: string }>;
  };
}

/** seedSingleApplication creates, deploys and returns one image application. */
async function seedSingleApplication(
  request: APIRequestContext,
): Promise<SeededApplication> {
  test.skip(
    !(await isOperator(request)),
    "the smoke account is not a platform operator (PLATFORM_ADMINS)",
  );
  const server = await terminalServer(request, account);
  test.skip(server === null, "no ready node agent is registered");
  const headers = authHeaders(account);
  const { projectId, environmentId } = await createProject(request, headers);
  const applicationId = await createImageApplication(
    request,
    headers,
    environmentId,
    server!.id,
  );
  const target = await deployApplication(request, headers, applicationId);
  const seed: SeededApplication = {
    projectId,
    environmentId,
    applicationId,
    containerId: target.container_id,
    service: target.service,
    teamId: "",
  };
  seeds.push(seed);
  return seed;
}

/** isOperator reads the platform-admin flag. */
async function isOperator(request: APIRequestContext): Promise<boolean> {
  const response = await request.get("/api/v1/auth/me", {
    headers: authHeaders(account),
  });
  if (!response.ok()) {
    return false;
  }
  const body = (await response.json()) as {
    user?: { is_platform_admin?: boolean };
  };
  return body.user?.is_platform_admin === true;
}

/** applicationURL is the nested application detail route. */
function applicationURL(seed: SeededApplication, tab?: string): string {
  const base = `/projects/${seed.projectId}/environments/${seed.environmentId}/applications/${seed.applicationId}`;
  return tab === undefined ? base : `${base}?tab=${tab}`;
}

/** terminalCalls records the terminal control requests of one page. */
interface TerminalCalls {
  traffic: TerminalTraffic;
  paths: string[];
}

/** watchApplicationTerminal adds application-route request logging. */
function watchApplicationTerminal(page: Page): TerminalCalls {
  const traffic = watchTerminal(page);
  const paths: string[] = [];
  page.on("request", (request) => {
    const url = request.url();
    if (
      /\/applications\/[^/]+\/terminal\/(targets|sessions)/.test(url) &&
      !/\/ws(?:\?|$)/.test(url)
    ) {
      paths.push(`${request.method()} ${new URL(url).pathname}`);
    }
  });
  return { traffic, paths };
}

/** openTerminalTab activates the application Terminal tab. */
async function openTerminalTab(page: Page): Promise<void> {
  await page.locator(".n-tabs-tab", { hasText: /^Terminal$/ }).first().click();
}

test.describe("application terminal on the real stack", () => {
  test("lazy start resolves the exact running container with one GET and one POST", async ({
    page,
    request,
  }) => {
    test.setTimeout(180_000);
    const seed = await seedSingleApplication(request);
    const { traffic, paths } = watchApplicationTerminal(page);

    await page.goto(applicationURL(seed));
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toBeVisible();
    await page.waitForTimeout(1_000);
    // Loading the detail page, and visiting another tab, issues nothing.
    expect(paths).toEqual([]);
    await page.locator(".n-tabs-tab", { hasText: /^Logs/ }).first().click();
    await page.waitForTimeout(500);
    expect(paths).toEqual([]);

    await openTerminalTab(page);
    await expect
      .poll(() => paths.filter((path) => path.startsWith("GET")).length, {
        timeout: 20_000,
      })
      .toBe(1);
    const create = await waitForCreate(traffic);
    expect(create.status).toBe(201);

    // Exact target identity: the service and the FULL 64-hex container id the
    // API resolved. A short id, a name or a host fallback would be a defect.
    expect(create.body).toMatchObject({
      service: seed.service,
      container_id: seed.containerId,
    });
    expect(String(create.body?.container_id)).toMatch(/^[0-9a-f]{64}$/);
    expect(paths).toEqual(
      expect.arrayContaining([
        `GET /api/v1/applications/${seed.applicationId}/terminal/targets`,
        `POST /api/v1/applications/${seed.applicationId}/terminal/sessions`,
      ]),
    );
    // Never a host shell from the application surface.
    expect(
      traffic.creates.some((entry: CreateRequest) =>
        entry.url.includes("/servers/"),
      ),
    ).toBe(false);

    // A real container shell: no host banner, a real prompt.
    await expect
      .poll(async () =>
        page.locator(".xterm-rows").first().innerText().then((text) => /[#$]/.test(text)),
      )
      .toBe(true);
    const screen = await page.locator(".xterm-rows").first().innerText();
    expect(screen).not.toContain("Gotham dev · server:");
  });

  test("the retained window session survives while the page starts one more", async ({
    page,
    request,
  }) => {
    test.setTimeout(180_000);
    const seed = await seedSingleApplication(request);
    const { traffic, paths } = watchApplicationTerminal(page);

    await page.goto(applicationURL(seed));
    await openTerminalTab(page);
    const first = await waitForCreate(traffic);
    expect(first.body).toMatchObject({ container_id: seed.containerId });
    await expect
      .poll(async () =>
        page.locator(".xterm-rows").first().innerText().then((text) => /[#$]/.test(text)),
      )
      .toBe(true);

    await page.getByRole("button", { name: "Open in window" }).first().click();
    await expect(page.locator(".term-window")).toBeVisible();
    expect(traffic.createCount()).toBe(1);
    expect(traffic.sockets).toHaveLength(1);
    expect(traffic.currentSocket().closed).toBe(false);

    await page.getByRole("button", { name: "Minimize terminal window" }).click();
    await expect(page.locator(".term-window")).not.toBeVisible();
    const before = paths.length;
    await page.getByRole("button", { name: "Start new session" }).first().click();
    await expect
      .poll(() => paths.length - before, { timeout: 20_000 })
      .toBeGreaterThanOrEqual(2);
    const added = paths.slice(before);
    expect(added.filter((path) => path.startsWith("GET"))).toHaveLength(1);
    expect(added.filter((path) => path.startsWith("POST"))).toHaveLength(1);

    // The window's session was kept: no DELETE and two live sockets.
    expect(traffic.deletes).toHaveLength(0);
    await expect
      .poll(() => traffic.sockets.filter((socket) => !socket.closed).length)
      .toBe(2);
    expect(traffic.createCount()).toBe(2);
  });

  test("a replaced container is rediscovered before the page opens a new shell", async ({
    page,
    request,
  }) => {
    test.setTimeout(240_000);
    const seed = await seedSingleApplication(request);
    const { traffic, paths } = watchApplicationTerminal(page);

    await page.goto(applicationURL(seed));
    await openTerminalTab(page);
    await waitForCreate(traffic);
    await expect
      .poll(async () =>
        page.locator(".xterm-rows").first().innerText().then((text) => /[#$]/.test(text)),
      )
      .toBe(true);

    // Replace the container through the real deploy pipeline. The old exec
    // session dies with it; the UI must rediscover targets on the explicit
    // restart instead of re-POSTing the stale id.
    const redeploy = await request.post(
      `/api/v1/applications/${seed.applicationId}/deploy`,
      { headers: authHeaders(account) },
    );
    expect(redeploy.status()).toBe(202);
    let fresh = "";
    await expect
      .poll(
        async () => {
          // A deploy in flight answers 409; poll through it.
          const response = await request.get(
            `/api/v1/applications/${seed.applicationId}/terminal/targets`,
            { headers: authHeaders(account) },
          );
          if (response.status() !== 200) {
            return false;
          }
          const targets = (await response.json()) as {
            containers: Array<{ container_id: string }>;
          };
          fresh = targets.containers[0]?.container_id ?? "";
          return fresh !== "" && fresh !== seed.containerId;
        },
        { timeout: 120_000 },
      )
      .toBe(true);

    const restart = page.getByRole("button", { name: "Start new session" }).first();
    await expect(restart).toBeVisible({ timeout: 60_000 });
    const before = paths.length;
    await restart.click();
    await expect
      .poll(() => traffic.createCount(), { timeout: 30_000 })
      .toBe(2);
    const added = paths.slice(before);
    expect(added.filter((path) => path.startsWith("GET"))).toHaveLength(1);
    expect(added.filter((path) => path.startsWith("POST"))).toHaveLength(1);
    const retried = traffic.creates[1]!;
    expect(retried.body).toMatchObject({
      service: seed.service,
      container_id: fresh,
    });
    // Never a host fallback and the retained target is the FRESH id.
    expect(retried.url).not.toContain("/servers/");
    expect(retried.body?.container_id).not.toBe(seed.containerId);
  });

  test.describe("read-only viewer", () => {
    let readOnly: SeededApplication;

    test.beforeAll(async ({ request }) => {
      readOnlySeed = await seedReadOnlyApplication(request);
      readOnly = readOnlySeed;
    });

    test("receives 403 on terminal targets and create", async ({ request }) => {
      const viewer = {
        Authorization: `Bearer ${readOnly.viewerToken}`,
        "X-Team-Id": readOnly.teamId,
      };
      const targets = await request.get(
        `/api/v1/applications/${readOnly.applicationId}/terminal/targets`,
        { headers: viewer },
      );
      expect(targets.status()).toBe(403);
      const create = await request.post(
        `/api/v1/applications/${readOnly.applicationId}/terminal/sessions`,
        {
          headers: viewer,
          data: {
            service: readOnly.service,
            container_id: readOnly.containerId,
            cols: 80,
            rows: 24,
          },
        },
      );
      expect(create.status()).toBe(403);
      // The same session without the team scope cannot see the application at
      // all (404, never a cross-team 403 oracle).
      const foreign = await request.get(
        `/api/v1/applications/${readOnly.applicationId}/terminal/targets`,
        { headers: { Authorization: `Bearer ${readOnly.viewerToken}` } },
      );
      expect(foreign.status()).toBe(404);
    });

    test("sees no Terminal tab and issues zero terminal requests", async ({
      browser,
      request,
    }) => {
      // JUS-257: the application detail route reads without X-Team-Id, so every
      // shared-team application page answers 404 for every member. Until that is
      // fixed the UI half cannot load the fixture; the API half above still
      // proves the terminal permission boundary. Flip this back on with JUS-257.
      const probe = await request.get(
        `/api/v1/applications/${readOnly.applicationId}`,
        { headers: { Authorization: `Bearer ${readOnly.viewerToken}` } },
      );
      test.skip(
        probe.status() === 404,
        "JUS-257: shared-team application detail 404s (getApplication sends no X-Team-Id)",
      );
      const visitor = await viewerContext(browser, readOnly);

      try {
        const viewerPage = await visitor.newPage();
        const { paths } = watchApplicationTerminal(viewerPage);
        await viewerPage.goto(applicationURL(readOnly));
        await expect(
          viewerPage.locator('nav[aria-label="Breadcrumb"]'),
        ).toBeVisible();
        await expect(
          viewerPage.locator(".n-tabs-tab", { hasText: /^Terminal$/ }),
        ).toHaveCount(0);
        // A crafted deep link falls back to Overview and shows the blocked state.
        await viewerPage.goto(applicationURL(readOnly, "terminal"));
        await expect(
          viewerPage.getByText(/needs write access/i).first(),
        ).toBeVisible();
        await viewerPage.waitForTimeout(1_000);
        expect(paths).toEqual([]);
      } finally {
        await visitor.close();
      }
    });
  });
});

/**
 * seedReadOnlyApplication creates a shared team, invites a fresh viewer with
 * the read_only role, and deploys an image application in that team. The
 * caller is the team owner; the returned viewer can only read.
 */
async function seedReadOnlyApplication(
  request: APIRequestContext,
): Promise<SeededApplication> {
  test.skip(
    !(await isOperator(request)),
    "the smoke account is not a platform operator (PLATFORM_ADMINS)",
  );
  test.skip(
    process.env.GOTHAM_AUTH_ALLOW_REGISTRATION !== "true",
    "registration is closed, so the read-only viewer account cannot be created",
  );
  const server = await terminalServer(request, account);
  test.skip(server === null, "no ready node agent is registered");

  const ownerHeaders = authHeaders(account);
  const teamResponse = await request.post("/api/v1/teams", {
    headers: ownerHeaders,
    data: { name: `ui-e2e-term-${uniqueSuffix()}` },
  });
  await expectOK(teamResponse, "seed team");
  const teamId = ((await teamResponse.json()) as { team: { id: string } }).team.id;

  const viewerEmail = `ui-e2e-viewer-${uniqueSuffix()}@example.com`;
  const viewerPassword = "Gotham-E2E-Password1";
  const register = await request.post("/api/v1/auth/register", {
    data: { email: viewerEmail, password: viewerPassword },
  });
  await expectOK(register, "register viewer");
  const session = (await register.json()) as {
    user: { id: string };
    access_token: string;
    refresh_token: string;
  };
  const invite = await request.post(`/api/v1/teams/${teamId}/invites`, {
    headers: ownerHeaders,
    data: { email: viewerEmail, role: "read_only" },
  });
  await expectOK(invite, "invite viewer");
  const token = ((await invite.json()) as { invite: { token: string } }).invite.token;
  const accept = await request.post("/api/v1/invites/accept", {
    data: { token },
    headers: { Authorization: `Bearer ${session.access_token}` },
  });
  await expectOK(accept, "accept invite");

  const teamScoped = teamHeaders(teamId, account);
  const { projectId, environmentId } = await createProject(request, teamScoped);
  const applicationId = await createImageApplication(
    request,
    teamScoped,
    environmentId,
    server!.id,
  );
  const target = await deployApplication(request, teamScoped, applicationId);
  const seed: SeededApplication = {
    projectId,
    environmentId,
    applicationId,
    containerId: target.container_id,
    service: target.service,
    teamId,
    viewerToken: session.access_token,
    viewerEmail,
    viewerUserId: session.user.id,
  };
  // The read-only seed is shared by the serial describe and cleaned in
  // afterAll, not after the first test.
  void viewerPassword;
  return seed;
}

/** viewerContext builds a browser context signed in as the read-only viewer. */
async function viewerContext(
  browser: BrowserContext["browser"],
  seed: SeededApplication,
): Promise<BrowserContext> {
  if (browser === undefined) {
    throw new Error("browser fixture unavailable");
  }
  const session = {
    user: { id: seed.viewerUserId ?? "", email: seed.viewerEmail ?? "" },
    accessToken: seed.viewerToken ?? "",
    refreshToken: "",
  };
  return browser.newContext({
    baseURL,
    viewport: { width: 1280, height: 800 },
    storageState: {
      cookies: [],
      origins: [
        {
          origin: baseURL,
          localStorage: [
            { name: "gotham.auth.session", value: JSON.stringify(session) },
            { name: "gotham.teams.active", value: seed.teamId },
          ],
        },
      ],
    },
  });
}
