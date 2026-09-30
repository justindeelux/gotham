import { expect, request, test } from "@playwright/test";
import type { APIRequestContext } from "@playwright/test";

import {
  baseURL,
  loadAccount,
  seedNodeAddress,
  storageStatePath,
  uniqueSuffix,
} from "./support";

// Reuse the authenticated session from global setup.
test.use({ storageState: storageStatePath });

/**
 * Focused regressions for the FE-7.1 fix round (R1–R3).
 *
 * This file deliberately uses the plain Playwright `test` instead of the
 * guardrail-wrapped one in `./fixtures`: each scenario forces the failure it
 * asserts on — a lifecycle response held past the shared read timeout, a held
 * re-render, an aborted deploy-history read — and the fixture's teardown would
 * flag exactly that forced failure. The guardrail-wrapped happy path lives in
 * `./services.spec.ts`.
 */

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
}

/** seedServer registers a node row and returns its id and generated name. */
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

/** createService stores one service with a distinct document and env marker. */
async function createService(
  api: APIRequestContext,
  headers: Record<string, string>,
  serverId: string,
  name: string,
  env: Record<string, string>,
  domain: string,
): Promise<{ id: string; name: string }> {
  const response = await api.post("/api/v1/services", {
    headers,
    data: {
      name,
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

/** seedService stores a compose service on a fresh server row. */
async function seedService(
  api: APIRequestContext,
  headers: Record<string, string>,
  suffix: string,
  prefix: string,
): Promise<SeededService> {
  const server = await seedServer(api, headers, suffix, prefix);
  const serviceName = `${prefix}-svc-${suffix}`;
  const service = await createService(
    api,
    headers,
    server.id,
    serviceName,
    {},
    "fix.example.test",
  );
  return { serviceId: service.id, serviceName: service.name, serverName: server.name };
}

/** serviceDetail reads one service through the API. */
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
  // The whole services surface is FEATURE_SERVICES-gated. Verify it is mounted
  // before seeding, so a feature-off control plane skips instead of failing.
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
    // The held response is released after 16s; the test needs room for the
    // Playwright timeout plus the page interactions around it.
    test.setTimeout(60_000);

    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const { serviceId } = await seedService(request, headers, uniqueSuffix(), "fix1");

    let responseHeld = false;
    await page.route("**/api/v1/services/*/deploy", async (route) => {
      // Hold the REAL backend response — a 502 on this agent-less control
      // plane — for longer than the shared 15s read timeout. Nothing is
      // synthesized and no deploy result is faked.
      const response = await route.fetch();
      responseHeld = true;
      await new Promise((resolve) => setTimeout(resolve, 16_000));
      await route.fulfill({ response });
    });

    await page.goto(`/services/${serviceId}`);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.getByRole("button", { name: "Deploy" }).click();

    // Past 15s the request must still be in flight, so the UI reports what the
    // backend answered instead of aborting with the shared read timeout.
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
    const oldDomain = `fix-old-${suffix}.example.test`;
    const newDomain = `fix-new-${suffix}.example.test`;
    const serviceName = `fix2-tpl-${suffix}`;

    await page.goto("/templates");
    await page.locator('[data-template="wordpress"]').click();
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

    // Back, change the domain, then hold the actual re-render response.
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

    // The previous preview is invalidated the moment the new render starts, so
    // forward navigation is blocked while the response is pending.
    await expect(wizard.getByRole("button", { name: "Next" })).toBeDisabled();

    const release = hold.release;
    if (release === null) {
      throw new Error("the re-render response was never held");
    }
    release();

    await expect(preview).toContainText(newDomain);
    await expect(preview).not.toContainText(oldDomain);

    // Create from the refreshed preview and prove the persisted document
    // carries the value the operator last entered.
    await wizard.getByRole("button", { name: "Next" }).click();
    const step3 = wizard.locator('[data-testid="wizard-step-3"]');
    await step3.locator(".field-service-name input").fill(serviceName);
    await step3.locator(".field-service-node .n-select").click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: server.name })
      .click();
    await wizard.getByRole("button", { name: "Create service" }).click();
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
    const { serviceId, serviceName } = await seedService(
      request,
      headers,
      uniqueSuffix(),
      "fix3",
    );

    // Abort only the history read; the service detail response still arrives.
    const historyPattern = "**/api/v1/services/*/deploys";
    await page.route(historyPattern, (route) => route.abort("failed"));

    // ── detail page: unavailable + retry, never the empty claims ─────────
    await page.goto(`/services/${serviceId}`);
    const unavailable = page.locator('[data-testid="history-unavailable"]');
    await expect(unavailable).toBeVisible();
    await expect(unavailable).toContainText("Deploy history unavailable");
    await expect(page.locator('[data-testid="history-empty"]')).toHaveCount(0);
    await expect(page.getByText("No deploys yet.")).toHaveCount(0);
    await expect(page.getByText("Nothing has been deployed yet.")).toHaveCount(0);
    await expect(page.getByText(/\d+ attempts/)).toHaveCount(0);

    // ── list: the card marks the history unavailable, not empty ──────────
    await page.goto("/services");
    const card = page.locator(`[data-service="${serviceName}"]`);
    await expect(card).toBeVisible();
    await expect(card).toContainText("deploy history unavailable");
    await expect(card).not.toContainText("no deploys yet");
    const listAlert = page.locator('[data-testid="list-history-unavailable"]');
    await expect(listAlert).toBeVisible();

    // ── retry after the API is reachable: a real empty read is claimed ───
    await page.unroute(historyPattern);
    await listAlert.getByRole("button", { name: "Retry" }).click();
    await expect(card).toContainText("no deploys yet");
    await expect(listAlert).toHaveCount(0);

    // The detail page recovers through the same successful read.
    await page.goto(`/services/${serviceId}`);
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
    const domainA = `fix5-a-${suffix}.example.test`;
    const domainB = `fix5-b-${suffix}.example.test`;
    const serviceA = await createService(
      request,
      headers,
      server.id,
      `fix5-a-${suffix}`,
      { MARK: "route-a" },
      domainA,
    );
    const serviceB = await createService(
      request,
      headers,
      server.id,
      `fix5-b-${suffix}`,
      { MARK: "route-b" },
      domainB,
    );

    // Hold A's genuine detail response so A's load is still in flight when the
    // SPA route moves to B.
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

    await page.goto(`/services/${serviceA.id}`);
    await expect.poll(() => held).toBe(true);

    // Move to B inside the SPA (no reload), so A's load keeps running in the
    // same component instance and its completion races B's.
    await page.evaluate((id: string) => {
      window.history.pushState({}, "", `/services/${id}`);
      window.dispatchEvent(new PopStateEvent("popstate", { state: {} }));
    }, serviceB.id);

    await expect(page).toHaveURL(new RegExp(`/services/${serviceB.id}$`));
    await expect(page.getByRole("heading", { level: 1 })).toContainText(
      serviceB.name,
    );
    const composeView = page.locator(".compose-editor__view");
    const envValue = page.locator(".env-row input[type='password']");
    await expect(composeView).toContainText(domainB);
    await expect(envValue).toHaveValue("route-b");

    // Release A's response: it must not seed B's editable drafts.
    const release = hold.release;
    if (release === null) {
      throw new Error("the detail response was never held");
    }
    release();
    // Let the released response land so the assertions below compare after the
    // race settled (a stale completion would have overwritten the drafts).
    await page.waitForTimeout(300);
    await expect(composeView).toContainText(domainB);
    await expect(composeView).not.toContainText(domainA);
    await expect(envValue).toHaveValue("route-b");

    // Saving B persists B's values; A's document and env never move across.
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

    // The compose editor round-trips B's own document (the guarded compose
    // save path).
    await page.getByRole("button", { name: "Edit" }).click();
    const editor = page.locator(".compose-editor__input textarea");
    await expect(editor).toHaveValue(new RegExp(domainB.replace(/\./g, "\\.")));
    await editor.fill(
      (await editor.inputValue()).replace("nginx:1.27-alpine", "nginx:1.28-alpine"),
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
    const { serviceId } = await seedService(
      request,
      headers,
      uniqueSuffix(),
      "fix4",
    );

    // The log endpoint answers 401 on every call (the first try and the retry
    // after a real refresh), so the reader's own refresh path runs against the
    // live refresh endpoint.
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

    await page.goto(`/services/${serviceId}`);
    await page.getByRole("button", { name: "Stream logs" }).click();
    await expect(
      page.getByText("Your session expired. Please sign in again."),
    ).toBeVisible();
    // Exactly one refresh attempt: the reader retries once, never in a loop.
    expect(refreshes).toBe(1);
  });
});
