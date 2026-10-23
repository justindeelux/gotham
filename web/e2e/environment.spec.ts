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

test.use({ storageState: storageStatePath });

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

    await page.goto(environmentURL(projectId, environmentId));
    await expect(page.locator(".environment-page .title")).toHaveText("production");
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText(projectName);
    const appRow = page.locator(".resource-table tbody tr").filter({ hasText: appName });
    await expect(appRow).toHaveCount(1);
    const rowAvatar = appRow.locator(".brand-icon");
    await expect(rowAvatar).toBeVisible();
    expect((await rowAvatar.boundingBox())?.width).toBe(28);
    await expect(rowAvatar).toHaveCSS("background-color", "rgb(110, 118, 129)");
    await expect(rowAvatar.locator(".brand-icon__glyph")).toHaveCount(1);
    const serviceRow = page.locator(".resource-table tbody tr").filter({ hasText: serviceName });
    await expect(serviceRow).toHaveCount(1);

    await page.locator(".tabs").getByRole("tab", { name: /Services/ }).click();
    await expect(
      page.locator(".resource-table").filter({ hasText: serviceName }),
    ).toBeVisible();
    await expect(page.locator(".resource-table")).not.toContainText(appName);
    await page.locator(".tabs").getByRole("tab", { name: /All/ }).click();

    await appRow.locator("a.resource-link").click();
    await expect(page).toHaveURL(
      nestedURL(projectId, environmentId, "applications", application.id),
    );
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText(projectName);
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText("production");
    await expect(page.locator('nav[aria-label="Breadcrumb"]')).toContainText(appName);
    await page.goBack();
    await expect(page.locator(".resource-table")).toBeVisible();

    await serviceRow.locator("a.resource-link").focus();
    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(
      nestedURL(projectId, environmentId, "services", service.id),
    );
    await page.goBack();

    await page.getByRole("button", { name: "Add resource" }).click();
    await expect(
      page.locator('.n-modal button.res-card[data-source="git_public"] .brand-icon__glyph'),
    ).toHaveCount(1);
    await page.locator(".n-modal button.res-card", { hasText: "Public git repository" }).click();
    const wizard = page.locator(".wizard-modal");
    await expect(wizard).toBeVisible();
    await expect(wizard).toContainText(`${projectName} / production`);
    await expect(wizard).toContainText("Source: Public git repository");

    const wizardAppName = `ui-e2e-wiz-${suffix}`.slice(0, 31);
    await wizard.locator("input[placeholder='https://github.com/owner/repo.git']").fill(cloneURL);
    await wizard.locator("input[placeholder='storefront']").fill(wizardAppName);
    await wizard.getByRole("button", { name: "Continue" }).click();
    await wizard.locator(".form-row .n-select").first().click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: node.name })
      .click();
    await wizard.getByRole("button", { name: "Continue" }).click();
    await wizard.getByRole("button", { name: "Continue" }).click();
    await wizard.getByRole("button", { name: "Continue" }).click();
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
