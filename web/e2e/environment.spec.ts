import { expect, test } from "./fixtures";
import {
  cloneURL,
  environmentURL,
  loadAccount,
  nestedURL,
  seedNodeAddress,
  seedProjectEnvironment,
  storageStatePath,
  uniqueSuffix,
} from "./support";

// Reuse the authenticated session so the page starts signed in; the seed
// calls authenticate separately with the access token from global setup.
test.use({ storageState: storageStatePath });

/**
 * PE-5 (JUS-34): the environment page against the real control plane.
 *
 * Seeds one server, one application and one service through the API
 * (configuration only — no deployment, so nothing is cloned or started) and
 * proves the environment page lists them with tabs and counts, opens a
 * resource through its nested URL with the Project/Environment/Resource
 * breadcrumb, and creates an application through the scoped wizard (the
 * route's project/environment in the summary, the seeded node selected).
 */
test.describe("environment page", () => {
  test("lists seeded resources, opens nested details, creates scoped", async ({
    page,
    guardrails,
    request,
  }) => {
    const account = loadAccount();
    const suffix = uniqueSuffix();
    const headers = { Authorization: `Bearer ${account.accessToken}` };

    const server = await request.post("/api/v1/servers", {
      headers,
      data: {
        name: `ui-e2e-env-node-${suffix}`,
        ip: seedNodeAddress,
        ssh_user: "root",
      },
    });
    expect(server.status(), await server.text()).toBe(201);
    const { server: node } = (await server.json()) as {
      server: { id: string; name: string };
    };

    const { projectId, projectName, environmentId } = await seedProjectEnvironment(
      request,
      headers,
    );
    const appName = `ui-e2e-env-app-${suffix}`;
    const createdApp = await request.post("/api/v1/applications", {
      headers,
      data: {
        name: appName,
        environment_id: environmentId,
        provider: "github",
        repo: "docker/welcome-to-docker",
        clone_url: cloneURL,
        branch: "main",
        build_pack: "auto",
        port: 80,
        host_port: 0,
        server_id: node.id,
      },
    });
    expect(createdApp.status(), await createdApp.text()).toBe(201);
    const { application } = (await createdApp.json()) as {
      application: { id: string };
    };

    const serviceName = `ui-e2e-env-svc-${suffix}`;
    const createdService = await request.post("/api/v1/services", {
      headers,
      data: {
        name: serviceName,
        environment_id: environmentId,
        server_id: node.id,
        compose_yaml: "services:\n  web:\n    image: nginx:1.27-alpine\n",
      },
    });
    expect(createdService.status(), await createdService.text()).toBe(201);
    const { service } = (await createdService.json()) as {
      service: { id: string };
    };

    // ── the environment page lists both resources with tabs and counts ──
    await page.goto(environmentURL(projectId, environmentId));
    await expect(page.locator(".environment-page .title")).toHaveText("production");
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText(projectName);
    const appRow = page.locator(".resource-table tbody tr").filter({ hasText: appName });
    await expect(appRow).toHaveCount(1);
    const serviceRow = page.locator(".resource-table tbody tr").filter({ hasText: serviceName });
    await expect(serviceRow).toHaveCount(1);

    await page.locator(".tabs").getByRole("tab", { name: /Services/ }).click();
    await expect(
      page.locator(".resource-table").filter({ hasText: serviceName }),
    ).toBeVisible();
    await expect(page.locator(".resource-table")).not.toContainText(appName);
    await page.locator(".tabs").getByRole("tab", { name: /All/ }).click();

    // ── Open leads to the nested detail with the full breadcrumb ─────────
    await appRow.getByRole("button", { name: "Open" }).click();
    await expect(page).toHaveURL(
      nestedURL(projectId, environmentId, "applications", application.id),
    );
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText(projectName);
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText("production");
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText(appName);
    await page.goBack();
    await expect(page.locator(".resource-table")).toBeVisible();

    // ── the service opens nested too ─────────────────────────────────────
    await serviceRow.getByRole("button", { name: "Open" }).click();
    await expect(page).toHaveURL(
      nestedURL(projectId, environmentId, "services", service.id),
    );
    await page.goBack();

    // ── Add resource: the app wizard takes the route scope ───────────────
    await page.getByRole("button", { name: "Add resource" }).click();
    await page.locator(".kind-card", { hasText: "Application" }).click();
    const wizard = page.locator(".wizard-modal");
    await expect(wizard).toBeVisible();
    await expect(wizard).toContainText(`${projectName} / production`);

    // Public git source: no provider round-trip, nothing is cloned here.
    const wizardAppName = `ui-e2e-wiz-${suffix}`.slice(0, 31);
    await wizard.locator(".n-select").first().click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: "Public git repository" })
      .click();
    await wizard.locator("input[placeholder='https://github.com/owner/repo.git']").fill(cloneURL);
    await wizard.locator("input[placeholder='storefront']").fill(wizardAppName);
    await wizard.getByRole("button", { name: "Continue" }).click();
    // Build pack step: auto-detect.
    await wizard.getByRole("button", { name: "Continue" }).click();
    // Runtime step: the seeded node is pending (no agent validated it);
    // pending nodes stay selectable, only offline ones are disabled.
    await wizard.locator(".form-row .n-select").first().click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: node.name })
      .click();
    await wizard.getByRole("button", { name: "Continue" }).click();
    // Env step: keep the NODE_ENV default.
    await wizard.getByRole("button", { name: "Continue" }).click();
    // Review step: the create carries the route scope.
    const createRequest = page.waitForRequest(
      (request) =>
        request.url().endsWith("/api/v1/applications") &&
        request.method() === "POST",
    );
    await wizard.getByRole("button", { name: "Create & deploy" }).click();
    const createBody = (await createRequest).postDataJSON() as Record<string, unknown>;
    expect(createBody["environment_id"]).toBe(environmentId);
    expect(createBody["server_id"]).toBe(node.id);
    expect(createBody["source_type"]).toBe("git_public");
    // The wizard queues the first deploy and lands on the nested detail.
    await expect(page).toHaveURL(
      new RegExp(`/projects/${projectId}/environments/${environmentId}/applications/[0-9a-f-]+$`),
    );
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText(wizardAppName);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});
