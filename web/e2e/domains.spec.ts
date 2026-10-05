import { expect, test } from "./fixtures";
import type { Locator, Page } from "@playwright/test";
import {
  cloneURL,
  loadAccount,
  seedNodeAddress,
  seedProjectEnvironment,
  storageStatePath,
  uniqueSuffix,
} from "./support";

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
 * FE-6.x: drives the real Domains & SSL surface against the control plane.
 *
 * Covers the DNS-provider lifecycle (create, list, edit with credential
 * rotation, delete), certificate configuration create/edit/delete, the live
 * certificate status/expiry rendering (observed node states), the redirect
 * rule lifecycle (create, toggle, edit, conflict error, delete), the
 * application base-domain edit with certificate re-record, and the API
 * validation error path. The router list has no API and stays a labeled
 * stub. No ACME issuance is triggered: certificate rows are intent records
 * and no node consumes them in this run.
 */
test.describe("domains", () => {
  // The single scenario drives the whole Domains & SSL surface against a real
  // control plane. Every reroute mutation — a certificate, a DNS provider, a
  // redirect, the application domain — makes the control plane resync *every*
  // registered node, so the run scales with the node rows the smoke seeds and
  // was recorded at 26-34 s on the shared self-hosted runner, straddling the
  // 30 s default. Raise the per-test budget rather than loosen one assertion:
  // nodes are seeded on an unreachable loopback address (seedNodeAddress) so each
  // resync fails fast, but the flow is legitimately long.
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

    // No API response may ever echo a credential back (the API returns only
    // `credentials_set`); response bodies are collected and asserted below.
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

    // A server row is required before an application can be stored. No agent
    // is connected, so nothing deploys and no certificate is issued.
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
    const { environmentId } = await seedProjectEnvironment(request, headers);

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
        environment_id: environmentId,
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

    // The router list is the only surface left without an API (BE-6.3 added
    // no router-list endpoint); certificates and redirects render live data.
    await tab(page, "Routers").click();
    await expect(page.getByText(/not exposed by the API yet/i)).toBeVisible();
    await tab(page, "Certificates").click();
    await expect(
      page.getByText("No certificate configurations yet."),
    ).toBeVisible();

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

    // ── credential lifecycle: every dismissal clears the secret ───────────
    // The credential input renders the page's form state, so a retained
    // secret would reappear in a reopened dialog. Each path types a throwaway
    // token, dismisses, reopens and requires a blank field.
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
    // Mask click: the overlay's outside-click handler closes the dialog.
    await checkDismissalClears(() => page.mouse.click(5, 5));

    // A failed write followed by Cancel: the rejected secret is dropped too.
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
    // A second enabled Cloudflare provider conflicts with the first.
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

    // ── DNS provider: edit with credential rotation ──────────────────────
    await providerCard
      .getByRole("button", { name: "Edit & rotate credential" })
      .click();
    const editProviderModal = page
      .locator(".n-modal")
      .filter({ hasText: "Edit DNS provider" })
      .first();
    // A reopened dialog must never prepopulate the previous secret.
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

    // A successful write also clears the field (checked on reopen).
    await expect(page.locator(".n-modal")).toHaveCount(0);
    const afterWriteModal = await openEditDialog();
    await expect(credentialInput(afterWriteModal)).toHaveValue("");
    await afterWriteModal.getByRole("button", { name: "Cancel" }).click();
    await expect(page.locator(".n-modal")).toHaveCount(0);

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

    // ── certificate status/expiry: live unknown from this control plane ───
    // No node agent is reachable in this run, so the control plane observes
    // the certificate as unknown and the view shows no expiry for it.
    await expect(
      page.getByRole("columnheader", { name: "Status" }),
    ).toBeVisible();
    await expect(
      page.getByRole("columnheader", { name: "Expires" }),
    ).toBeVisible();
    certRow = page.getByRole("row").filter({ hasText: domain });
    await expect(certRow).toContainText("unknown");

    // present/absent cannot be produced by a local CP without a connected
    // node, so the list response is rewritten in the browser: the SPA must
    // render exactly what the API reports and never invent a status.
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

    // ── redirects: create, toggle, edit, conflict error, delete ───────────
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
    // Keyboard selection avoids ambiguity with the add form's mounted menu.
    await page.keyboard.press("ArrowUp");
    await page.keyboard.press("Enter");
    await editItem("Preserve path").getByRole("switch").click();
    await editRedirectModal.getByRole("button", { name: "Save" }).click();
    await expect(editRedirectModal).toBeHidden();

    redirectRow = page.locator(".domain-row").filter({ hasText: redirectSource });
    await expect(redirectRow).toContainText(redirectTargetChanged);
    await expect(redirectRow).toContainText("301");
    await expect(redirectRow).not.toContainText("keeps the path");

    // A duplicate source is rejected with the API's actionable 409 message.
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

    // ── credential hygiene: no echo, no browser storage ──────────────────
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
