import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";
import { cloneURL, loadAccount, storageStatePath, uniqueSuffix } from "./support";

// Reuse the authenticated session so the page starts signed in; the seed
// calls authenticate separately with the access token from global setup.
test.use({ storageState: storageStatePath });

interface SeededApplication {
  id: string;
  name: string;
}

/** tab locates a Naive UI tab header by its label (tabs carry no ARIA role). */
function tab(page: Page, name: string) {
  return page.locator(".n-tabs-tab").filter({ hasText: name });
}

/**
 * FE-6.1: drives the real Domains & SSL surface against the control plane.
 *
 * Covers the DNS-provider lifecycle (create, list, edit with credential
 * rotation, delete), certificate configuration create/edit/delete, the
 * application base-domain edit with certificate re-record, the API validation
 * error path, and the explicit backend-pending stubs (no fabricated status,
 * expiry or redirect data). No ACME issuance is triggered: certificate rows
 * are intent records and no node consumes them in this run.
 */
test.describe("domains", () => {
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

    // A server row is required before an application can be stored. No agent
    // is connected, so nothing deploys and no certificate is issued.
    const serverResponse = await request.post("/api/v1/servers", {
      headers,
      data: {
        name: `ui-e2e-node-${suffix}`,
        ip: "127.0.0.1",
        ssh_user: "root",
      },
    });
    expect(serverResponse.status(), await serverResponse.text()).toBe(201);
    const { server } = (await serverResponse.json()) as {
      server: { id: string };
    };

    const appResponse = await request.post("/api/v1/applications", {
      headers,
      data: {
        name: `ui-e2e-app-${suffix}`,
        provider: "github",
        repo: "docker/welcome-to-docker",
        clone_url: cloneURL,
        branch: "main",
        build_pack: "auto",
        base_domain: domain,
        port: 80,
        host_port: 0,
        server_id: server.id,
      },
    });
    expect(appResponse.status(), await appResponse.text()).toBe(201);
    const { application: app } = (await appResponse.json()) as {
      application: SeededApplication;
    };

    // A second application without a base domain backs the API error path:
    // certificate writes for it must answer 400 and the UI must show it.
    const blankResponse = await request.post("/api/v1/applications", {
      headers,
      data: {
        name: `ui-e2e-blank-${suffix}`,
        provider: "github",
        repo: "docker/welcome-to-docker",
        clone_url: cloneURL,
        branch: "main",
        build_pack: "auto",
        port: 80,
        host_port: 0,
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

    // Honest stubs: the certificates pane labels the missing status/expiry
    // instead of rendering values, and no redirect surface exists.
    await expect(
      page.getByText("Issuance status & expiry — backend pending"),
    ).toBeVisible();
    await expect(page.getByRole("columnheader", { name: "Expires" })).toHaveCount(0);
    await expect(page.getByRole("columnheader", { name: "Status" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Add redirect" })).toHaveCount(0);

    // ── error path: the API rejects a certificate for a domainless app ────
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
      createCertModal.getByText(/no valid base domain/i),
    ).toBeVisible();
    await createCertModal.getByRole("button", { name: "Cancel" }).click();
    await expect(createCertModal).toBeHidden();

    // ── DNS provider: create ─────────────────────────────────────────────
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
    // The write-only credential must never be rendered back.
    await expect(page.getByText(credential)).toHaveCount(0);

    // ── DNS provider: edit with credential rotation ──────────────────────
    await providerCard
      .getByRole("button", { name: "Edit & rotate credential" })
      .click();
    const editProviderModal = page
      .locator(".n-modal")
      .filter({ hasText: "Edit DNS provider" })
      .first();
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

    // ── certificate: create with http-01 ─────────────────────────────────
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
    await expect(certRow).toContainText("http-01");

    // ── certificate: edit to dns-01 + provider + wildcard ────────────────
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
    await expect(certRow).toContainText("dns-01");
    await expect(certRow).toContainText(rotatedName);
    await expect(certRow).toContainText("wildcard");

    // ── stub tabs: no fabricated router or redirect data ─────────────────
    await tab(page, "Routers").click();
    await expect(
      page.getByText(/not exposed by the API yet/i),
    ).toBeVisible();
    await tab(page, "Redirects").click();
    await expect(
      page.getByText(/not implemented in the backend yet/i),
    ).toBeVisible();

    // ── application detail: change the domain and re-record the cert ─────
    await page.goto(`/applications/${app.id}`);
    await tab(page, "Domains").click();
    const domainCard = page.locator(".n-card").filter({ hasText: "Application domain" });
    await expect(domainCard.locator("input").first()).toHaveValue(domain);
    await domainCard.locator("input").first().fill(changedDomain);
    await page.getByRole("button", { name: "Save domain" }).click();
    await expect(
      page.getByText(/records the current base domain/i),
    ).toBeVisible();
    await page.getByRole("button", { name: "Re-record domain" }).click();
    await expect(
      page.getByText(/records the current base domain/i),
    ).toHaveCount(0);
    await expect(page.getByText(rotatedName)).toBeVisible();

    // ── delete certificate, then the now-unreferenced provider ───────────
    await page.goto("/domains");
    await tab(page, "Certificates").click();
    certRow = page.getByRole("row").filter({ hasText: changedDomain });
    await expect(certRow).toHaveCount(1);
    await certRow.getByRole("button", { name: "Delete" }).click();
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    await expect(certRow).toHaveCount(0);

    await tab(page, "DNS providers").click();
    providerCard = page.locator(".channel-card").filter({ hasText: rotatedName });
    await providerCard.getByRole("button", { name: "Delete" }).click();
    await page.getByRole("button", { name: "Confirm", exact: true }).click();
    await expect(providerCard).toHaveCount(0);
  });
});
