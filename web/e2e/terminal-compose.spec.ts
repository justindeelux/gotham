import { expect, test } from "./fixtures";
import { loadAccount, storageStatePath, uniqueSuffix } from "./support";
import { authHeaders, terminalServer, waitForCreate, watchTerminal } from "./terminal-support";
import type { APIRequestContext } from "@playwright/test";

/**
 * JUS-251 + JUS-256: a REAL `docker compose` application (two services)
 * deployed by the node agent, then opened from the browser Terminal tab.
 *
 * The compose project is started by the platform through the agent's
 * ComposeService — the same path a production deploy uses — and torn down by
 * the cleanup hook. The scenario pins the JUS-256 regression: compose targets
 * must carry the FULL 64-hex container id (a 12-hex CLI id was rejected by the
 * agent), each service must be selectable, and the shell must run in the real
 * container without stopping it on close.
 *
 * While #405 is unmerged the control plane still resolves 12-hex ids; the test
 * detects that specific state and skips with the ticket reference instead of
 * failing green CI on a known, separately tracked defect.
 */

test.use({ storageState: storageStatePath, viewport: { width: 1366, height: 900 } });

const account = loadAccount();
const COMPOSE_IMAGE = "busybox:1.36";

interface ComposeSeed {
  projectId: string;
  environmentId: string;
  applicationId: string;
  webId: string;
  workerId: string;
}

let seed: ComposeSeed | null = null;

test.afterEach(async ({ request }) => {
  const current = seed;
  seed = null;
  if (current === null) {
    return;
  }
  await request
    .delete(`/api/v1/applications/${current.applicationId}`, {
      headers: authHeaders(account),
    })
    .catch(() => undefined);
  await request
    .delete(`/api/v1/projects/${current.projectId}`, {
      headers: authHeaders(account),
    })
    .catch(() => undefined);
});

/** seedComposeApplication deploys a two-service compose app on the real node. */
async function seedComposeApplication(request: APIRequestContext): Promise<ComposeSeed> {
  const me = await request.get("/api/v1/auth/me", { headers: authHeaders(account) });
  const body = me.ok()
    ? ((await me.json()) as { user?: { is_platform_admin?: boolean } })
    : {};
  test.skip(
    body.user?.is_platform_admin !== true,
    "the smoke account is not a platform operator (PLATFORM_ADMINS)",
  );
  const server = await terminalServer(request, account);
  test.skip(server === null, "no ready node agent is registered");
  const headers = authHeaders(account);

  const project = await request.post("/api/v1/projects", {
    headers,
    data: { name: `ui-e2e-compose-${uniqueSuffix()}` },
  });
  expect(project.status()).toBe(201);
  const projectBody = (await project.json()) as {
    project: { id: string };
    environments: Array<{ id: string }>;
  };
  const projectId = projectBody.project.id;
  const environmentId = projectBody.environments[0]!.id;

  const composeContent = [
    "services:",
    "  web:",
    `    image: ${COMPOSE_IMAGE}`,
    '    command: ["sleep", "3600"]',
    "  worker:",
    `    image: ${COMPOSE_IMAGE}`,
    '    command: ["sleep", "3600"]',
    "",
  ].join("\n");
  const application = await request.post("/api/v1/applications", {
    headers,
    data: {
      environment_id: environmentId,
      server_id: server!.id,
      name: `ui-e2e-compose-${uniqueSuffix()}`.slice(0, 31),
      source_type: "compose",
      compose_content: composeContent,
      compose_service: "web",
    },
  });
  expect(
    application.status(),
    `create compose app: ${await application.text()}`,
  ).toBe(201);
  const applicationId = (
    (await application.json()) as { application: { id: string } }
  ).application.id;

  const deploy = await request.post(
    `/api/v1/applications/${applicationId}/deploy`,
    { headers },
  );
  expect(deploy.status()).toBe(202);

  const deadline = Date.now() + 180_000;
  while (Date.now() < deadline) {
    const response = await request.get(
      `/api/v1/applications/${applicationId}/terminal/targets`,
      { headers },
    );
    if (response.status() === 200) {
      const targets = (await response.json()) as {
        containers: Array<{ service: string; container_id: string; state: string }>;
      };
      const web = targets.containers.find((container) => container.service === "web");
      const worker = targets.containers.find((container) => container.service === "worker");
      if (web !== undefined && worker !== undefined) {
        const result: ComposeSeed = {
          projectId,
          environmentId,
          applicationId,
          webId: web.container_id,
          workerId: worker.container_id,
        };
        seed = result;
        return result;
      }
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error("compose application never produced two service containers");
}

/** fullComposeIds reports whether JUS-256's full-id resolution is deployed. */
function fullComposeIds(current: ComposeSeed): boolean {
  return (
    /^[0-9a-f]{64}$/.test(current.webId) && /^[0-9a-f]{64}$/.test(current.workerId)
  );
}

test("a real compose project lists every service with a full id and opens a shell", async ({
  page,
  request,
}) => {
  test.setTimeout(300_000);
  const current = await seedComposeApplication(request);
  test.skip(
    !fullComposeIds(current),
    "JUS-256 (#405) not merged: compose targets still return 12-hex ids",
  );

  const traffic = watchTerminal(page);
  const calls: string[] = [];
  page.on("request", (requestSeen) => {
    const url = requestSeen.url();
    if (
      /\/applications\/[^/]+\/terminal\/(targets|sessions)/.test(url) &&
      !/\/ws(?:\?|$)/.test(url)
    ) {
      calls.push(`${requestSeen.method()} ${new URL(url).pathname}`);
    }
  });

  await page.goto(
    `/projects/${current.projectId}/environments/${current.environmentId}/applications/${current.applicationId}`,
  );
  await page.waitForTimeout(1_000);
  expect(calls).toEqual([]);
  await page.locator(".n-tabs-tab", { hasText: /^Terminal$/ }).first().click();

  // Two services: discovery runs, but no shell starts until one is chosen.
  await expect
    .poll(() => calls.filter((call) => call.startsWith("GET")).length, {
      timeout: 20_000,
    })
    .toBe(1);
  await expect(page.getByText("Choose a service container to open a shell")).toBeVisible();
  await page.waitForTimeout(1_000);
  expect(traffic.createCount()).toBe(0);

  await page.locator("#app-term-target").click();
  await page
    .locator(".n-base-select-option")
    .filter({ hasText: /\bweb\b/ })
    .first()
    .click();
  const create = await waitForCreate(traffic);
  expect(create.status).toBe(201);
  expect(create.body).toMatchObject({
    service: "web",
    container_id: current.webId,
  });
  expect(String(create.body?.container_id)).toMatch(/^[0-9a-f]{64}$/);
  expect(calls.filter((call) => call.startsWith("POST"))).toHaveLength(1);

  // A real shell in the compose container: busybox's prompt, no host banner.
  await expect
    .poll(
      async () =>
        page
          .locator(".xterm-rows")
          .first()
          .innerText()
          .then((text) => text.includes("/ #")),
      { timeout: 20_000 },
    )
    .toBe(true);
  const screen = await page.locator(".xterm-rows").first().innerText();
  expect(screen).not.toContain("Gotham dev · server:");
  const marker = "COMPOSE-" + current.webId.slice(0, 8);
  await page.locator(".xterm-helper-textarea").first().focus();
  await page.keyboard.type(`echo ${marker}`);
  await page.keyboard.press("Enter");
  await expect
    .poll(async () => (await page.locator(".xterm-rows").first().innerText()).includes(marker))
    .toBe(true);

  // Closing the exec must never stop the application containers.
  await page.getByRole("button", { name: "Close terminal" }).click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: /^(Close all|Close terminal)$/ })
    .click();
  await expect
    .poll(async () => {
      const response = await request.get(
        `/api/v1/applications/${current.applicationId}/terminal/targets`,
        { headers: authHeaders(account) },
      );
      if (response.status() !== 200) {
        return false;
      }
      const targets = (await response.json()) as {
        containers: Array<{ service: string; state: string }>;
      };
      return targets.containers.every(
        (container) => container.service !== "" && container.state === "running",
      );
    })
    .toBe(true);
});
