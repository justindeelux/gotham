import { expect, test } from "./fixtures";
import { cloneURL, loadAccount, storageStatePath, uniqueSuffix } from "./support";

// Reuse the authenticated session so the page starts signed in; the seed call
// authenticates separately with the access token from global setup.
test.use({ storageState: storageStatePath });

/**
 * QA-4.1b (c): seed one server row and one application row through the API
 * (configuration only — no deployment, so nothing is cloned) and prove the
 * application appears in the list and opens on its detail page.
 */
test.describe("applications", () => {
  test("lists an API-seeded application and opens its detail page", async ({
    page,
    guardrails,
    request,
  }) => {
    const account = loadAccount();
    const name = `ui-e2e-${uniqueSuffix()}`;
    const headers = { Authorization: `Bearer ${account.accessToken}` };

    // A server row is required by the API before an application can be
    // stored. Registering it does not connect an agent and does not deploy
    // anything — the application stays configuration-only.
    const server = await request.post("/api/v1/servers", {
      headers,
      data: {
        name: `ui-e2e-node-${uniqueSuffix()}`,
        ip: "127.0.0.1",
        ssh_user: "root",
      },
    });
    expect(server.status(), await server.text()).toBe(201);
    const { server: node } = (await server.json()) as {
      server: { id: string };
    };

    const created = await request.post("/api/v1/applications", {
      headers,
      data: {
        name,
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
    expect(created.status(), await created.text()).toBe(201);
    const { application } = (await created.json()) as {
      application: { id: string; name: string };
    };
    expect(application.name).toBe(name);

    await page.goto("/applications");
    await expect(
      page.getByRole("heading", { name: "Applications", level: 1 }),
    ).toBeVisible();

    const row = page.locator(".provider-row").filter({ hasText: name });
    await expect(row).toHaveCount(1);

    await row.getByRole("button", { name: "Open" }).click();
    await expect(page).toHaveURL(
      new RegExp(`/applications/${application.id}$`),
    );
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();

    expect(guardrails.apiFailures).toEqual([]);
  });
});
