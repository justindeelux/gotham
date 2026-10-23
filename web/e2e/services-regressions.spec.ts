import { expect, test } from "@playwright/test";
import type { APIRequestContext } from "@playwright/test";

import {
  environmentURL,
  loadAccount,
  nestedURL,
  seedNodeAddress,
  seedProjectEnvironment,
  storageStatePath,
  uniqueSuffix,
} from "./support";

test.use({ storageState: storageStatePath });

const sampleCompose = [
  "services:",
  "  web:",
  "    image: nginx:1.27-alpine",
  "    labels:",
  '      gotham.domain: "fix.example.test"',
  '      gotham.domain.port: "80"',
  "    ports:",
  '      - "80"',
  "volumes:",
  "  web-data:",
  "",
].join("\n");

interface SeededService {
  serviceId: string;
  serviceName: string;
  serverName: string;
  projectId: string;
  environmentId: string;
}

async function seedServer(
  api: APIRequestContext,
  headers: Record<string, string>,
  suffix: string,
  prefix: string,
): Promise<{ id: string; name: string }> {
  const name = `${prefix}-node-${suffix}`;
  const response = await api.post("/api/v1/servers", {
    headers,
    data: { name, ip: seedNodeAddress, ssh_user: "root" },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { server } = (await response.json()) as {
    server: { id: string; name: string };
  };
  return { id: server.id, name: server.name };
}

async function createService(
  api: APIRequestContext,
  headers: Record<string, string>,
  serverId: string,
  environmentId: string,
  name: string,
  env: Record<string, string>,
  domain: string,
): Promise<{ id: string; name: string }> {
  const response = await api.post("/api/v1/services", {
    headers,
    data: {
      name,
      environment_id: environmentId,
      server_id: serverId,
      compose_yaml: sampleCompose.replace("fix.example.test", domain),
      env,
    },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { service } = (await response.json()) as {
    service: { id: string; name: string };
  };
  return service;
}

async function seedService(
  api: APIRequestContext,
  headers: Record<string, string>,
  suffix: string,
  prefix: string,
): Promise<SeededService> {
  const server = await seedServer(api, headers, suffix, prefix);
  const { projectId, environmentId } = await seedProjectEnvironment(api, headers);
  const serviceName = `${prefix}-svc-${suffix}`;
  const service = await createService(
    api,
    headers,
    server.id,
    environmentId,
    serviceName,
    {},
    "fix.example.test",
  );
  return {
    serviceId: service.id,
    serviceName: service.name,
    serverName: server.name,
    projectId,
    environmentId,
  };
}

async function serviceDetail(
  api: APIRequestContext,
  headers: Record<string, string>,
  id: string,
): Promise<{ compose_yaml?: string; env: Record<string, string> }> {
  const response = await api.get(`/api/v1/services/${id}`, { headers });
  expect(response.status(), await response.text()).toBe(200);
  const { service } = (await response.json()) as {
    service: { compose_yaml?: string; env?: Record<string, string> };
  };
  return { compose_yaml: service.compose_yaml, env: service.env ?? {} };
}

test.describe("services fix regressions", () => {
  test.beforeEach(async ({ request }) => {
    const account = loadAccount();
    const catalog = await request.get("/api/v1/templates", {
      headers: { Authorization: `Bearer ${account.accessToken}` },
    });
    test.skip(
      catalog.status() === 404 || catalog.status() === 503,
      `template surface unavailable (HTTP ${catalog.status()}): FEATURE_SERVICES is off`,
    );
  });

  test("R1: a lifecycle call that outlives the 15s read timeout is not aborted", async ({
    page,
    request,
  }) => {
    test.setTimeout(60_000);

    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const seeded = await seedService(request, headers, uniqueSuffix(), "fix1");
    const { serviceId } = seeded;

    let responseHeld = false;
    await page.route("**/api/v1/services/*/deploy", async (route) => {
      const response = await route.fetch();
      responseHeld = true;
      await new Promise((resolve) => setTimeout(resolve, 16_000));
      await route.fulfill({ response });
    });

    await page.goto(
      nestedURL(seeded.projectId, seeded.environmentId, "services", serviceId),
    );
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.getByRole("button", { name: "Deploy" }).click();

    await expect(page.getByText(/Node agent error/)).toBeVisible({
      timeout: 30_000,
    });
    expect(responseHeld).toBe(true);
    expect(await page.content()).not.toContain("timeout of 15000ms");
  });

  test("R2: a pending re-render blocks create from the obsolete preview", async ({
    page,
    request,
  }) => {
    test.setTimeout(60_000);

    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const suffix = uniqueSuffix();
    const server = await seedServer(request, headers, suffix, "fix2");
    const { projectId, projectName, environmentId } = await seedProjectEnvironment(request, headers);
    const oldDomain = `fix-old-${suffix}.example.test`;
    const newDomain = `fix-new-${suffix}.example.test`;
    const serviceName = `fix2-tpl-${suffix}`;

    await page.goto(environmentURL(projectId, environmentId));
    await page.getByRole("button", { name: "Add resource" }).first().click();
    const picker = page.locator(".n-modal.app-modal");
    await picker.getByRole("tab", { name: "Service", exact: true }).click();
    await picker.locator('[data-template="wordpress"]').click();
    const wizard = page
      .locator(".n-modal")
      .filter({ hasText: "Deploy template WordPress" })
      .first();
    const step1 = wizard.locator('[data-testid="wizard-step-1"]');
    await expect(step1).toBeVisible();
    await step1.locator(".field-domain input").fill(oldDomain);
    await step1.locator(".field-db_password input").fill(`fix2-db-${suffix}`);
    await step1.locator(".field-db_root_password input").fill(`fix2-root-${suffix}`);
    await wizard.getByRole("button", { name: "Next" }).click();

    const step2 = wizard.locator('[data-testid="wizard-step-2"]');
    const preview = step2.locator('[data-testid="compose-preview"]');
    await expect(preview).toContainText(oldDomain);

    await wizard.getByRole("button", { name: "Back" }).click();
    await step1.locator(".field-domain input").fill(newDomain);
    const hold: { release: (() => void) | null } = { release: null };
    await page.route("**/api/v1/templates/*/render", async (route) => {
      const response = await route.fetch();
      await new Promise<void>((resolve) => {
        hold.release = resolve;
      });
      await route.fulfill({ response });
    });
    await wizard.getByRole("button", { name: "Next" }).click();
    await expect.poll(() => hold.release !== null).toBe(true);

    await expect(wizard.getByRole("button", { name: "Next" })).toBeDisabled();

    const release = hold.release;
    if (release === null) {
      throw new Error("the re-render response was never held");
    }
    release();

    await expect(preview).toContainText(newDomain);
    await expect(preview).not.toContainText(oldDomain);

    await wizard.getByRole("button", { name: "Next" }).click();
    const step3 = wizard.locator('[data-testid="wizard-step-3"]');
    await step3.locator(".field-service-name input").fill(serviceName);
    await expect(step3).toContainText(`${projectName} / production`);
    await step3.locator(".wizard__metaform .n-select").click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: server.name })
      .click();
    const createRequest = page.waitForRequest(
      (request) =>
        request.url().endsWith("/api/v1/services") &&
        request.method() === "POST",
    );
    await wizard.getByRole("button", { name: "Create service" }).click();
    const createBody = (await createRequest).postDataJSON() as Record<string, unknown>;
    expect(createBody["environment_id"]).toBe(environmentId);
    await expect(wizard.locator('[data-testid="wizard-created"]')).toBeVisible();

    const listResponse = await request.get("/api/v1/services", { headers });
    expect(listResponse.status(), await listResponse.text()).toBe(200);
    const { services } = (await listResponse.json()) as {
      services: Array<{ id: string; name: string }>;
    };
    const created = services.find((service) => service.name === serviceName);
    if (!created) {
      throw new Error(`service ${serviceName} is not listed by the API`);
    }
    const detail = await serviceDetail(request, headers, created.id);
    expect(detail.compose_yaml).toContain(newDomain);
    expect(detail.compose_yaml).not.toContain(oldDomain);
  });

  test("R3: an unreadable deploy history is not presented as empty", async ({
    page,
    request,
  }) => {
    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const seeded = await seedService(
      request,
      headers,
      uniqueSuffix(),
      "fix3",
    );
    const { serviceId } = seeded;

    const historyPattern = "**/api/v1/services/*/deploys";
    await page.route(historyPattern, (route) => route.abort("failed"));

    await page.goto(
      nestedURL(seeded.projectId, seeded.environmentId, "services", serviceId),
    );
    const unavailable = page.locator('[data-testid="history-unavailable"]');
    await expect(unavailable).toBeVisible();
    await expect(unavailable).toContainText("Deploy history unavailable");
    await expect(page.locator('[data-testid="history-empty"]')).toHaveCount(0);
    await expect(page.getByText("No deploys yet.")).toHaveCount(0);
    await expect(page.getByText("Nothing has been deployed yet.")).toHaveCount(0);
    await expect(page.getByText(/\d+ attempts/)).toHaveCount(0);

    await page.unroute(historyPattern);
    await unavailable.getByRole("button", { name: "Retry" }).click();
    await expect(page.locator('[data-testid="history-empty"]')).toBeVisible();
    await expect(page.getByText("Nothing has been deployed yet.")).toBeVisible();
  });

  test("R5: a late detail read cannot overwrite another service's config", async ({
    page,
    request,
  }) => {
    test.setTimeout(60_000);

    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const suffix = uniqueSuffix();
    const server = await seedServer(request, headers, suffix, "fix5");
    const { projectId, environmentId } = await seedProjectEnvironment(request, headers);
    const domainA = `fix5-a-${suffix}.example.test`;
    const domainB = `fix5-b-${suffix}.example.test`;
    const serviceA = await createService(
      request,
      headers,
      server.id,
      environmentId,
      `fix5-a-${suffix}`,
      { MARK: "route-a" },
      domainA,
    );
    const serviceB = await createService(
      request,
      headers,
      server.id,
      environmentId,
      `fix5-b-${suffix}`,
      { MARK: "route-b" },
      domainB,
    );

    const hold: { release: (() => void) | null } = { release: null };
    let held = false;
    await page.route(
      (url) => url.pathname === `/api/v1/services/${serviceA.id}`,
      async (route) => {
        const response = await route.fetch();
        const pending = new Promise<void>((resolve) => {
          hold.release = resolve;
        });
        held = true;
        await pending;
        await route.fulfill({ response });
      },
    );

    await page.goto(
      nestedURL(projectId, environmentId, "services", serviceA.id),
    );
    await expect.poll(() => held).toBe(true);

    const nestedB = nestedURL(projectId, environmentId, "services", serviceB.id);
    await page.evaluate((url: string) => {
      window.history.pushState({}, "", url);
      window.dispatchEvent(new PopStateEvent("popstate", { state: {} }));
    }, nestedB);

    await expect(page).toHaveURL(new RegExp(`${nestedB}$`));
    await expect(page.getByRole("heading", { level: 1 })).toContainText(
      serviceB.name,
    );
    const composeView = page.locator(".compose-editor__view");
    const envValue = page.locator(".env-row input[type='password']");
    await expect(composeView).toContainText(domainB);
    await expect(envValue).toHaveValue("route-b");

    const release = hold.release;
    if (release === null) {
      throw new Error("the detail response was never held");
    }
    release();
    await page.waitForTimeout(300);
    await expect(composeView).toContainText(domainB);
    await expect(composeView).not.toContainText(domainA);
    await expect(envValue).toHaveValue("route-b");

    await page.getByRole("button", { name: "Save environment" }).click();
    await expect(
      page.getByText("Environment saved. It applies to the next deploy."),
    ).toBeVisible();
    const storedB = await serviceDetail(request, headers, serviceB.id);
    expect(storedB.env.MARK).toBe("route-b");
    expect(storedB.compose_yaml).toContain(domainB);
    expect(storedB.compose_yaml).not.toContain(domainA);

    const storedA = await serviceDetail(request, headers, serviceA.id);
    expect(storedA.env.MARK).toBe("route-a");
    expect(storedA.compose_yaml).toContain(domainA);

    await page.getByRole("button", { name: "Edit" }).click();
    const editor = page.locator(".compose-editor__input .cm-content");
    await expect(editor).toContainText(domainB);
    await editor.fill(
      (await editor.innerText()).replace("nginx:1.27-alpine", "nginx:1.28-alpine"),
    );
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(
      page.getByText("Compose document saved. Deploy to apply it on the node."),
    ).toBeVisible();
    const storedB2 = await serviceDetail(request, headers, serviceB.id);
    expect(storedB2.compose_yaml).toContain("nginx:1.28-alpine");
    expect(storedB2.compose_yaml).toContain(domainB);
    expect(storedB2.compose_yaml).not.toContain(domainA);
  });

  test("Vue: the log reader refreshes the session once on 401 and reports expiry", async ({
    page,
    request,
  }) => {
    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const seeded = await seedService(
      request,
      headers,
      uniqueSuffix(),
      "fix4",
    );
    const { serviceId } = seeded;

    await page.route("**/api/v1/services/*/logs*", (route) =>
      route.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ message: "unauthorized" }),
      }),
    );
    let refreshes = 0;
    page.on("request", (tracked) => {
      if (tracked.url().includes("/api/v1/auth/refresh")) {
        refreshes += 1;
      }
    });

    await page.goto(
      nestedURL(seeded.projectId, seeded.environmentId, "services", serviceId),
    );
    await page.getByRole("button", { name: "Stream logs" }).click();
    await expect(
      page.getByText("Your session expired. Please sign in again."),
    ).toBeVisible();
    expect(refreshes).toBe(1);
  });
});
