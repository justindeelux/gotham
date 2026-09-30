import { expect, test } from "./fixtures";
import { loadAccount, seedNodeAddress, storageStatePath, uniqueSuffix } from "./support";

// Reuse the authenticated session so the page starts signed in; the seed
// calls authenticate separately with the access token from global setup.
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

/**
 * FE-7.1: drives the real template gallery and wizard against the control
 * plane.
 *
 * Covers the catalog rendering, the dynamic form generated from the template
 * schema (required + default), the rendered compose preview, service creation
 * from the render with the secret `env` map, and the service appearing in the
 * compose-services list. The render contract is asserted at the API boundary:
 * `compose_yaml` keeps the `${field}` reference while the value travels only
 * in `env`.
 *
 * The control plane has no node agent, so a live deploy cannot be asserted
 * here (it is covered by the gated internal/e2e P7 tests); this smoke stops
 * after creation and never fakes a deploy result. When the services feature is
 * disabled, every route answers 404/503 and the smoke skips.
 */
test.describe("services & templates", () => {
  test("gallery → dynamic form → compose preview → create service", async ({
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
    const dbPassword = `e2e-db-${suffix}-9f2a-secret`;
    const rootPassword = `e2e-root-${suffix}-4c1b-secret`;

    // A server row is required before a service can be stored. No agent is
    // connected, so nothing deploys and no container exists.
    const serverResponse = await request.post("/api/v1/servers", {
      headers,
      data: { name: nodeName, ip: seedNodeAddress, ssh_user: "root" },
    });
    expect(serverResponse.status(), await serverResponse.text()).toBe(201);

    // ── the gallery loads the live catalog ───────────────────────────────
    await page.goto("/templates");
    await expect(
      page.getByRole("heading", { name: "Template library", level: 1 }),
    ).toBeVisible();
    for (const template of templates) {
      await expect(
        page.locator(`[data-template="${template.slug}"]`),
        `${template.slug} renders as a card`,
      ).toBeVisible();
    }

    // ── opening a template renders the dynamic form ──────────────────────
    await page.locator('[data-template="wordpress"]').click();
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
    // The default comes from the schema; secrets never carry one.
    await expect(step1.locator(".field-db_name input")).toHaveValue("wordpress");
    await expect(step1.locator(".field-db_password input")).toHaveValue("");
    await expect(step1.locator(".field-db_password input")).toHaveAttribute(
      "type",
      "password",
    );

    // A missing required field blocks the step and shows the schema message.
    await wizard.getByRole("button", { name: "Next" }).click();
    await expect(step1.getByText("This field is required.").first()).toBeVisible();
    await expect(wizard.locator('[data-testid="wizard-step-2"]')).toBeHidden();

    await step1.locator(".field-domain input").fill(domain);
    await step1.locator(".field-db_password input").fill(dbPassword);
    await step1.locator(".field-db_root_password input").fill(rootPassword);
    await wizard.getByRole("button", { name: "Next" }).click();

    // ── rendering shows the compose preview without any secret ───────────
    const step2 = wizard.locator('[data-testid="wizard-step-2"]');
    await expect(step2).toBeVisible();
    const preview = step2.locator('[data-testid="compose-preview"]');
    await expect(preview).toContainText("image: wordpress:6-apache");
    await expect(preview).toContainText(domain);
    await expect(preview).toContainText("${db_password}");
    await expect(preview).not.toContainText(dbPassword);
    await expect(preview).not.toContainText(rootPassword);
    await expect(step2).toContainText("2 services");

    // ── name + node, then create the service from the render ─────────────
    await wizard.getByRole("button", { name: "Next" }).click();
    const step3 = wizard.locator('[data-testid="wizard-step-3"]');
    await expect(step3).toBeVisible();
    await step3.locator(".field-service-name input").fill(serviceName);
    await step3.locator(".field-service-node .n-select").click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: nodeName })
      .click();
    // The wizard footer (not the step body) carries the submit button.
    await wizard.getByRole("button", { name: "Create service" }).click();

    const created = wizard.locator('[data-testid="wizard-created"]');
    await expect(created).toBeVisible();
    await expect(created).toContainText(serviceName);

    // The create request carries the render's env map: the API stores the
    // secret values there and keeps only `${field}` in the document.
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

    // ── the created service appears in the compose-services list ─────────
    await wizard.getByRole("button", { name: "Close", exact: true }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);
    await page.goto("/services");
    await expect(
      page.getByRole("heading", { name: "Services", level: 1 }),
    ).toBeVisible();
    const card = page.locator(`[data-service="${serviceName}"]`);
    await expect(card).toHaveCount(1);
    await expect(card).toContainText("creating");
    await expect(card).toContainText("no deploys yet");

    // ── secret hygiene: values stay out of the DOM and browser storage ───
    const browserStorage = await page.evaluate(() =>
      [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join("\n"),
    );
    expect(browserStorage).not.toContain(dbPassword);
    expect(browserStorage).not.toContain(rootPassword);
    expect(await page.content()).not.toContain(dbPassword);
    expect(await page.content()).not.toContain(rootPassword);

    // The guardrail fixture asserts console errors and 5xx responses at
    // teardown; the happy path must also leave no failed API request behind.
    expect(
      guardrails.apiFailures,
      `unexpected failed API requests:\n${guardrails.apiFailures.join("\n")}`,
    ).toEqual([]);
  });
});
