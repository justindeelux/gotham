import { expect, test } from "./fixtures";
import {
  environmentURL,
  loadAccount,
  seedNodeAddress,
  seedProjectEnvironment,
  storageStatePath,
  uniqueSuffix,
} from "./support";

test.use({ storageState: storageStatePath });

interface TemplateSummary {
  slug: string;
  name: string;
  description: string;
}

interface ServicePayload {
  id: string;
  name: string;
  status: string;
  compose_yaml?: string;
  env: Record<string, string>;
}

test.describe("services & templates", () => {
  test("picker → dynamic form → compose preview → create service", async ({
    page,
    request,
    guardrails,
  }) => {
    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };

    const catalog = await request.get("/api/v1/templates", { headers });
    if (catalog.status() === 404 || catalog.status() === 503) {
      test.skip(
        true,
        `template surface unavailable (HTTP ${catalog.status()}): FEATURE_SERVICES is off`,
      );
      return;
    }
    expect(catalog.status(), await catalog.text()).toBe(200);
    const { templates } = (await catalog.json()) as {
      templates: TemplateSummary[];
    };
    expect(templates.length).toBeGreaterThan(0);
    const wordpress = templates.find((template) => template.slug === "wordpress");
    if (!wordpress) {
      throw new Error("catalog does not ship the wordpress template");
    }

    const suffix = uniqueSuffix();
    const nodeName = `ui-e2e-svc-node-${suffix}`;
    const domain = `wp-${suffix}.example.test`;
    const serviceName = `wp-${suffix}`;

    const serverResponse = await request.post("/api/v1/servers", {
      headers,
      data: { name: nodeName, ip: seedNodeAddress, ssh_user: "root" },
    });
    expect(serverResponse.status(), await serverResponse.text()).toBe(201);
    const { projectId, projectName, environmentId } = await seedProjectEnvironment(request, headers);

    await page.goto(environmentURL(projectId, environmentId));
    await page.getByRole("button", { name: "Add resource" }).first().click();
    const picker = page.locator(".n-modal.app-modal");
    await picker.getByRole("tab", { name: "Service", exact: true }).click();
    for (const template of templates) {
      await expect(
        picker.locator(`[data-template="${template.slug}"]`),
        `${template.slug} renders as a card`,
      ).toBeVisible();
    }

    await picker.locator('[data-template="wordpress"]').click();
    const wizard = page
      .locator(".n-modal")
      .filter({ hasText: "Deploy template WordPress" })
      .first();
    await expect(wizard).toBeVisible();

    const step1 = wizard.locator('[data-testid="wizard-step-1"]');
    await expect(step1).toBeVisible();
    for (const key of ["domain", "db_name", "db_user", "db_password", "db_root_password"]) {
      await expect(
        step1.locator(`.field-${key}`),
        `field ${key} comes from the template schema`,
      ).toBeVisible();
    }
    await expect(step1.locator(".field-db_name input")).toHaveValue("wordpress");
    await expect(step1.locator(".field-db_password input")).toHaveValue("");
    await expect(step1.locator(".field-db_password input")).toHaveAttribute(
      "type",
      "password",
    );

    await wizard.getByRole("button", { name: "Next" }).click();
    await expect(step1.getByText("This field is required.").first()).toBeVisible();
    await expect(wizard.locator('[data-testid="wizard-step-2"]')).toBeHidden();

    await step1.locator(".field-domain input").fill(domain);

    const dbInput = step1.locator(".field-db_password input");
    const rootInput = step1.locator(".field-db_root_password input");
    const generateDb = step1.getByRole("button", { name: "Generate Database password" });
    const generateRoot = step1.getByRole("button", { name: "Generate MySQL root password" });
    await expect(generateDb).toBeVisible();
    await expect(generateRoot).toBeVisible();

    await generateDb.click();
    await expect(dbInput).toHaveValue(/^[A-Za-z0-9]{24}$/);
    const firstDbPassword = await dbInput.inputValue();
    await generateDb.click();
    await expect(dbInput).not.toHaveValue(firstDbPassword);
    const dbPassword = await dbInput.inputValue();
    await expect(step1.getByRole("button", { name: "Copy Database password" })).toBeVisible();

    await generateRoot.click();
    await expect(rootInput).toHaveValue(/^[A-Za-z0-9]{24}$/);
    const rootPassword = await rootInput.inputValue();

    await wizard.getByRole("button", { name: "Next" }).click();

    const step2 = wizard.locator('[data-testid="wizard-step-2"]');
    await expect(step2).toBeVisible();
    const preview = step2.locator('[data-testid="compose-preview"]');
    await expect(preview).toContainText("image: wordpress:6-apache");
    await expect(preview).not.toContainText("gotham.domain");
    await expect(preview).toContainText("${db_password}");
    await expect(preview).not.toContainText(dbPassword);
    await expect(preview).not.toContainText(rootPassword);
    await expect(step2).toContainText("2 services");

    await wizard.getByRole("button", { name: "Next" }).click();
    const step3 = wizard.locator('[data-testid="wizard-step-3"]');
    await expect(step3).toBeVisible();
    await step3.locator(".field-service-name input").fill(serviceName);
    await expect(step3).toContainText(`${projectName} / production`);
    await step3.locator(".wizard__metaform .n-select").click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: nodeName })
      .click();
    const createRequest = page.waitForRequest(
      (request) =>
        request.url().endsWith("/api/v1/services") &&
        request.method() === "POST",
    );
    await wizard.getByRole("button", { name: "Create service" }).click();
    const createBody = (await createRequest).postDataJSON() as Record<string, unknown>;
    expect(createBody["environment_id"]).toBe(environmentId);
    expect(createBody["domains"]).toEqual([
      { domain, compose_service: "wordpress", port: 80, protocol: "http" },
    ]);

    const created = wizard.locator('[data-testid="wizard-created"]');
    await expect(created).toBeVisible();
    await expect(created).toContainText(serviceName);

    const listResponse = await request.get("/api/v1/services", { headers });
    expect(listResponse.status(), await listResponse.text()).toBe(200);
    const { services } = (await listResponse.json()) as {
      services: ServicePayload[];
    };
    const stored = services.find((service) => service.name === serviceName);
    if (!stored) {
      throw new Error(`service ${serviceName} is not listed by the API`);
    }
    const detailResponse = await request.get(`/api/v1/services/${stored.id}`, {
      headers,
    });
    expect(detailResponse.status(), await detailResponse.text()).toBe(200);
    const { service: detail } = (await detailResponse.json()) as {
      service: ServicePayload;
    };
    expect(detail.env.db_password).toBe(dbPassword);
    expect(detail.env.db_root_password).toBe(rootPassword);
    expect(detail.compose_yaml).toContain("${db_password}");
    expect(detail.compose_yaml).not.toContain(dbPassword);
    expect(detail.compose_yaml).not.toContain(rootPassword);
    expect(detail.compose_yaml).not.toContain("gotham.domain");

    const domainsResponse = await request.get(
      `/api/v1/services/${stored.id}/domains`,
      { headers },
    );
    expect(domainsResponse.status(), await domainsResponse.text()).toBe(200);
    const { domains } = (await domainsResponse.json()) as {
      domains: Array<{ domain: string; source: string }>;
    };
    expect(domains).toContainEqual(
      expect.objectContaining({ domain, source: "managed" }),
    );

    await wizard.getByRole("button", { name: "Close", exact: true }).click();
    await expect(picker).toBeVisible();
    await page.goto(environmentURL(projectId, environmentId));
    const row = page.locator(".resource-table tbody tr").filter({ hasText: serviceName });
    await expect(row).toHaveCount(1);
    await expect(row).toContainText("creating");

    const browserStorage = await page.evaluate(() =>
      [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join("\n"),
    );
    expect(browserStorage).not.toContain(dbPassword);
    expect(browserStorage).not.toContain(rootPassword);
    expect(await page.content()).not.toContain(dbPassword);
    expect(await page.content()).not.toContain(rootPassword);

    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});
