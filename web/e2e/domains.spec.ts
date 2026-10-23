import { expect, test } from "./fixtures";
import type { Locator, Page } from "@playwright/test";
import {
  cloneURL,
  loadAccount,
  nestedURL,
  seedNodeAddress,
  seedProjectEnvironment,
  storageStatePath,
  uniqueSuffix,
} from "./support";

test.use({ storageState: storageStatePath });

interface SeededApplication {
  id: string;
  name: string;
}

function tab(page: Page, name: string) {
  return page.locator(".n-tabs-tab").filter({ hasText: name });
}

test.describe("domains", () => {
  test.describe.configure({ timeout: 60_000 });

  test("manages DNS providers, certificates and the application domain", async ({
    page,
    request,
  }) => {
    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const suffix = uniqueSuffix();
    const zone = `z-${suffix}.example.com`;
    const domain = `app.${zone}`;
    const changedDomain = `changed.${zone}`;
    const providerName = `e2e-cf-${suffix}`;
    const rotatedName = `e2e-cf-x-${suffix}`;
    const credential = `e2e-token-${suffix}-0123456789abcdef`;
    const rotatedCredential = `e2e-rotated-${suffix}-0123456789abcdef`;
    const throwawayCredential = `discard-me-${suffix}-0123456789abcdef`;

    const credentialEchoes: string[] = [];
    const responseChecks: Promise<void>[] = [];
    page.on("response", (response) => {
      if (!response.url().includes("/api/v1/proxy/")) {
        return;
      }
      responseChecks.push(
        response
          .text()
          .then((body) => {
            if (
              body.includes(credential) ||
              body.includes(rotatedCredential) ||
              body.includes(throwawayCredential)
            ) {
              credentialEchoes.push(
                `${response.status()} ${response.request().method()} ${response.url()}`,
              );
            }
          })
          .catch(() => undefined),
      );
    });

    const serverResponse = await request.post("/api/v1/servers", {
      headers,
      data: {
        name: `ui-e2e-node-${suffix}`,
        ip: seedNodeAddress,
        ssh_user: "root",
      },
    });
    expect(serverResponse.status(), await serverResponse.text()).toBe(201);
    const { server } = (await serverResponse.json()) as {
      server: { id: string };
    };
    const { environmentId, projectId } = await seedProjectEnvironment(request, headers);

    const appResponse = await request.post("/api/v1/applications", {
      headers,
      data: {
        name: `ui-e2e-app-${suffix}`,
        environment_id: environmentId,
        provider: "github",
        repo: "docker/welcome-to-docker",
        clone_url: cloneURL,
        branch: "main",
        build_pack: "auto",
        server_id: server.id,
      },
    });
    expect(appResponse.status(), await appResponse.text()).toBe(201);
    const { application: app } = (await appResponse.json()) as {
      application: SeededApplication;
    };
    const domainResponse = await request.post(`/api/v1/applications/${app.id}/domains`, {
      headers,
      data: { domain, port: 80, protocol: "http" },
    });
    expect(domainResponse.status(), await domainResponse.text()).toBe(201);

    const blankResponse = await request.post("/api/v1/applications", {
      headers,
      data: {
        name: `ui-e2e-blank-${suffix}`,
        environment_id: environmentId,
        provider: "github",
        repo: "docker/welcome-to-docker",
        clone_url: cloneURL,
        branch: "main",
        build_pack: "auto",
        server_id: server.id,
      },
    });
    expect(blankResponse.status(), await blankResponse.text()).toBe(201);
    const { application: blankApp } = (await blankResponse.json()) as {
      application: SeededApplication;
    };
    expect(blankApp.name).not.toBe(app.name);

    await page.goto("/domains");
    await expect(
      page.getByRole("heading", { name: "Domains & SSL", level: 1 }),
    ).toBeVisible();

    await tab(page, "Routers").click();
    await expect(page.getByText(/agent unavailable/i).first()).toBeVisible();
    await expect(page.getByText("No routers generated yet.")).toBeVisible();
    await tab(page, "Certificates").click();
    await expect(
      page.getByText("No certificate configurations yet."),
    ).toBeVisible();

    await page.getByRole("button", { name: "Add certificate" }).first().click();
    const createCertModal = page
      .locator(".n-modal")
      .filter({ hasText: "Add certificate" })
      .first();
    await createCertModal.locator(".field-application .n-select").click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: blankApp.name })
      .click();
    await createCertModal.getByRole("button", { name: "Save" }).click();
    await expect(
      createCertModal.getByText(/no primary domain yet/i),
    ).toBeVisible();
    await createCertModal.getByRole("button", { name: "Cancel" }).click();
    await expect(createCertModal).toBeHidden();

    await tab(page, "DNS providers").click();
    await page.getByRole("button", { name: "Add provider" }).first().click();
    const providerModal = page
      .locator(".n-modal")
      .filter({ hasText: "Add DNS provider" })
      .first();
    await providerModal.locator(".field-name input").fill(providerName);
    await providerModal.locator(".field-zones .n-select").click();
    await page.keyboard.type(zone);
    await page.keyboard.press("Enter");
    await providerModal.locator(".field-credential input").fill(credential);
    await providerModal.getByRole("button", { name: "Save" }).click();

    let providerCard = page.locator(".channel-card").filter({ hasText: providerName });
    await expect(providerCard).toHaveCount(1);
    await expect(providerCard).toContainText("set");
    await expect(page.getByText(credential)).toHaveCount(0);

    const credentialInput = (modal: Locator) => modal.locator(".field-credential input");
    const openEditDialog = async (): Promise<Locator> => {
      await providerCard
        .getByRole("button", { name: "Edit & rotate credential" })
        .click();
      return page
        .locator(".n-modal")
        .filter({ hasText: "Edit DNS provider" })
        .first();
    };
    const checkDismissalClears = async (
      dismiss: (_modal: Locator) => Promise<void>,
    ): Promise<void> => {
      const modal = await openEditDialog();
      await credentialInput(modal).fill(throwawayCredential);
      await dismiss(modal);
      await expect(page.locator(".n-modal")).toHaveCount(0);
      const reopened = await openEditDialog();
      await expect(credentialInput(reopened)).toHaveValue("");
      await reopened.getByRole("button", { name: "Cancel" }).click();
      await expect(page.locator(".n-modal")).toHaveCount(0);
    };

    await checkDismissalClears((modal) =>
      modal.getByRole("button", { name: "Cancel" }).click(),
    );
    await checkDismissalClears((modal) =>
      modal.locator(".n-card-header__close").click(),
    );
    await checkDismissalClears(() => page.keyboard.press("Escape"));
    await checkDismissalClears(() => page.mouse.click(5, 5));

    await page.getByRole("button", { name: "Add provider" }).first().click();
    let createProviderModal = page
      .locator(".n-modal")
      .filter({ hasText: "Add DNS provider" })
      .first();
    await createProviderModal.locator(".field-name input").fill(`e2e-dup-${suffix}`);
    await createProviderModal.locator(".field-zones .n-select").click();
    await page.keyboard.type(zone);
    await page.keyboard.press("Enter");
    await credentialInput(createProviderModal).fill(throwawayCredential);
    await createProviderModal.getByRole("button", { name: "Save" }).click();
    await expect(createProviderModal.getByText(/already exists/i)).toBeVisible();
    await createProviderModal.getByRole("button", { name: "Cancel" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);
    await page.getByRole("button", { name: "Add provider" }).first().click();
    createProviderModal = page
      .locator(".n-modal")
      .filter({ hasText: "Add DNS provider" })
      .first();
    await expect(credentialInput(createProviderModal)).toHaveValue("");
    await createProviderModal.getByRole("button", { name: "Cancel" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);
    await expect(page.getByText(throwawayCredential)).toHaveCount(0);

    await providerCard
      .getByRole("button", { name: "Edit & rotate credential" })
      .click();
    const editProviderModal = page
      .locator(".n-modal")
      .filter({ hasText: "Edit DNS provider" })
      .first();
    await expect(credentialInput(editProviderModal)).toHaveValue("");
    await editProviderModal.locator(".field-name input").fill(rotatedName);
    await editProviderModal
      .locator(".field-credential input")
      .fill(rotatedCredential);
    await editProviderModal.getByRole("button", { name: "Save" }).click();

    providerCard = page.locator(".channel-card").filter({ hasText: rotatedName });
    await expect(providerCard).toHaveCount(1);
    await expect(providerCard).toContainText("set");
    await expect(page.getByText(credential)).toHaveCount(0);
    await expect(page.getByText(rotatedCredential)).toHaveCount(0);

    await expect(page.locator(".n-modal")).toHaveCount(0);
    const afterWriteModal = await openEditDialog();
    await expect(credentialInput(afterWriteModal)).toHaveValue("");
    await afterWriteModal.getByRole("button", { name: "Cancel" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);

    await tab(page, "Certificates").click();
    await page.getByRole("button", { name: "Add certificate" }).first().click();
    const createModal = page
      .locator(".n-modal")
      .filter({ hasText: "Add certificate" })
      .first();
    await createModal.locator(".field-application .n-select").click();
    await page.locator(".n-base-select-option").filter({ hasText: app.name }).click();
    await expect(createModal.getByText(domain, { exact: true })).toBeVisible();
    await createModal.getByRole("button", { name: "Save" }).click();

    let certRow = page.getByRole("row").filter({ hasText: domain });
    await expect(certRow).toHaveCount(1);
    await expect(certRow).toContainText(domain);

    await certRow.getByRole("button", { name: "Edit" }).click();
    const editCertModal = page
      .locator(".n-modal")
      .filter({ hasText: "Edit certificate configuration" })
      .first();
    await editCertModal.locator(".n-radio").filter({ hasText: "dns-01" }).click();
    await editCertModal.locator(".field-provider-select .n-select").click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: rotatedName })
      .click();
    await editCertModal
      .getByRole("switch", { name: "Wildcard certificate" })
      .click();
    await editCertModal.getByRole("button", { name: "Save" }).click();

    certRow = page.getByRole("row").filter({ hasText: domain });
    await expect(certRow).toContainText(domain);

    await expect(
      page.getByRole("columnheader", { name: "Status" }),
    ).toBeVisible();
    await expect(
      page.getByRole("columnheader", { name: "Expires" }),
    ).toBeVisible();
    certRow = page.getByRole("row").filter({ hasText: domain });
    await expect(certRow).toContainText("unknown");

    const notAfter = new Date(Date.now() + 45 * 24 * 60 * 60 * 1000);
    const expectedExpiryDate = notAfter.toLocaleDateString("en-GB", {
      day: "numeric",
      month: "short",
      year: "numeric",
    });
    await page.route("**/api/v1/proxy/certificates", async (route) => {
      if (route.request().method() !== "GET") {
        await route.continue();
        return;
      }
      const response = await route.fetch();
      const body = (await response.json()) as {
        certificates: Array<Record<string, unknown>>;
      };
      const certificates = (body.certificates ?? []).map((certificate) => ({
        ...certificate,
        status: "present",
        not_after: notAfter.toISOString(),
      }));
      certificates.push(
        {
          ...certificates[0],
          id: `fixture-absent-${suffix}`,
          application_id: blankApp.id,
          domain: `absent-${zone}`,
          status: "absent",
          not_after: undefined,
        },
        {
          ...certificates[0],
          id: `fixture-unknown-${suffix}`,
          application_id: blankApp.id,
          domain: `unknown-${zone}`,
          status: "unknown",
          not_after: undefined,
        },
      );
      await route.fulfill({ response, json: { ...body, certificates } });
    });

    await page.reload();
    await tab(page, "Certificates").click();
    certRow = page.getByRole("row").filter({ hasText: domain });
    await expect(certRow).toContainText("present");
    await expect(certRow).toContainText(expectedExpiryDate);
    await expect(certRow).toContainText("expires in 45 days");
    await expect(
      page.getByRole("row").filter({ hasText: `absent-${zone}` }),
    ).toContainText("no certificate");
    await expect(
      page.getByRole("row").filter({ hasText: `unknown-${zone}` }),
    ).toContainText("unknown");
    await page.unroute("**/api/v1/proxy/certificates");

    await tab(page, "Redirects").click();
    await expect(page.getByText("No redirect rules yet.")).toBeVisible();
    const createRedirectCard = page
      .locator(".n-card")
      .filter({ hasText: "Add redirect" })
      .first();
    const redirectSource = `go-${zone}`;
    const redirectTarget = `checkout-${zone}`;
    const redirectTargetChanged = `storefront-${zone}`;

    await createRedirectCard.locator(".field-application .n-select").click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: app.name })
      .click();
    await createRedirectCard.locator(".field-source input").fill(redirectSource);
    await createRedirectCard.locator(".field-target input").fill(redirectTarget);
    await createRedirectCard.locator(".field-code .n-select").click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: "302" })
      .click();
    await createRedirectCard
      .getByRole("button", { name: "Add redirect" })
      .click();

    let redirectRow = page
      .locator(".domain-row")
      .filter({ hasText: redirectSource });
    await expect(redirectRow).toHaveCount(1);
    await expect(redirectRow).toContainText(redirectTarget);
    await expect(redirectRow).toContainText("302");
    await expect(redirectRow).toContainText("enabled");
    await expect(redirectRow).toContainText("keeps the path");

    await redirectRow.getByRole("switch").click();
    await expect(redirectRow).toContainText("paused");
    await redirectRow.getByRole("switch").click();
    await expect(redirectRow).toContainText("enabled");

    await redirectRow.getByRole("button", { name: "Edit" }).click();
    const editRedirectModal = page
      .locator(".n-modal")
      .filter({ hasText: "Edit redirect rule" })
      .first();
    await expect(editRedirectModal.getByText(app.name)).toBeVisible();
    const editItem = (label: string) =>
      editRedirectModal.locator(".n-form-item").filter({ hasText: label });
    await editItem("Target domain").locator("input").fill(redirectTargetChanged);
    await editItem("Redirect code").locator(".n-select").click();
    await page.keyboard.press("ArrowUp");
    await page.keyboard.press("Enter");
    await editItem("Preserve path").getByRole("switch").click();
    await editRedirectModal.getByRole("button", { name: "Save" }).click();
    await expect(editRedirectModal).toBeHidden();

    redirectRow = page.locator(".domain-row").filter({ hasText: redirectSource });
    await expect(redirectRow).toContainText(redirectTargetChanged);
    await expect(redirectRow).toContainText("301");
    await expect(redirectRow).not.toContainText("keeps the path");

    await createRedirectCard.locator(".field-source input").fill(redirectSource);
    await createRedirectCard.locator(".field-target input").fill(`other-${zone}`);
    await createRedirectCard
      .getByRole("button", { name: "Add redirect" })
      .click();
    await expect(
      createRedirectCard.getByText(/already used by another redirect rule/i),
    ).toBeVisible();

    await redirectRow.getByRole("button", { name: "Delete" }).click();
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    await expect(redirectRow).toHaveCount(0);

    await page.goto(nestedURL(projectId, environmentId, "applications", app.id));
    await tab(page, "Domains").click();
    const domainCard = page.locator(".n-card").filter({ hasText: "Domains" }).first();
    const primaryRow = domainCard.locator(".domain-row").filter({ hasText: domain });
    await primaryRow
      .getByRole("button", { name: `Edit domain ${domain}` })
      .click();
    const domainModal = page.locator(".domain-form-modal");
    await expect(domainModal).toBeVisible();
    await domainModal.locator('input[aria-label="Domain hostname"]').fill(changedDomain);
    await expect(
      domainModal.getByText(/removes the certificate configuration recorded for/i),
    ).toBeVisible();
    await domainModal.getByRole("button", { name: "Save domain" }).click();
    await expect(domainModal).toBeHidden();
    const changedRow = domainCard.locator(".domain-row").filter({ hasText: changedDomain });
    await expect(changedRow).toHaveCount(1);
    await expect(domainCard.getByText(/not found/i)).toHaveCount(0);
    await expect(
      page.locator(".n-message").filter({ hasText: "Domain updated." }),
    ).toBeVisible();
    await expect(
      page.locator(".n-message").filter({ hasText: "Not found" }),
    ).toHaveCount(0);

    await changedRow
      .getByRole("button", { name: `Certificate for ${changedDomain}` })
      .click();
    const rowCertModal = page.locator(".certificate-modal");
    await expect(rowCertModal).toBeVisible();
    await expect(rowCertModal.getByText(changedDomain, { exact: true })).toBeVisible();
    await rowCertModal.getByRole("button", { name: "Save" }).click();
    await expect(rowCertModal).toBeHidden();
    await expect(changedRow).not.toContainText("no certificate");

    const aliasCard = page.locator(".n-card").filter({ hasText: "Domains" }).first();
    const aliasDomain = `www.${zone}`;
    await aliasCard.locator(".domain-add").click();
    const aliasModal = page.locator(".domain-form-modal");
    await expect(aliasModal).toBeVisible();
    await aliasModal.locator('input[aria-label="Domain hostname"]').fill(aliasDomain);
    await aliasModal.getByRole("button", { name: "Add domain" }).click();
    await expect(aliasModal).toBeHidden();
    const aliasRow = aliasCard.locator(".domain-row").filter({ hasText: aliasDomain });
    await expect(aliasRow).toHaveCount(1);
    await expect(aliasCard.getByText(/something went wrong/i)).toHaveCount(0);
    await expect(aliasCard.getByText(/cannot read properties/i)).toHaveCount(0);
    await aliasRow.getByRole("button", { name: `Remove ${aliasDomain}` }).click();
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    await expect(aliasRow).toHaveCount(0);
    await expect(aliasCard.getByText(/something went wrong/i)).toHaveCount(0);

    await page.goto("/domains");
    await tab(page, "Certificates").click();
    certRow = page
      .getByRole("row")
      .filter({ has: page.locator(".mono", { hasText: changedDomain }) });
    await expect(certRow).toHaveCount(1);
    await certRow.getByRole("button", { name: "Delete" }).click();
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    await expect(certRow).toHaveCount(0);

    await tab(page, "DNS providers").click();
    providerCard = page.locator(".channel-card").filter({ hasText: rotatedName });
    await providerCard.getByRole("button", { name: "Delete" }).click();
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    await expect(providerCard).toHaveCount(0);

    await Promise.all(responseChecks);
    expect(
      credentialEchoes,
      `responses echoed a credential:\n${credentialEchoes.join("\n")}`,
    ).toEqual([]);
    const browserStorage = await page.evaluate(() =>
      [JSON.stringify(localStorage), JSON.stringify(sessionStorage)].join("\n"),
    );
    expect(browserStorage).not.toContain(credential);
    expect(browserStorage).not.toContain(rotatedCredential);
    expect(browserStorage).not.toContain(throwawayCredential);
  });
});
