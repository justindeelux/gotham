import { expect, test } from "./fixtures";
import {
  cloneURL,
  loadAccount,
  seedNodeAddress,
  seedProjectEnvironment,
  storageStatePath,
  uniqueSuffix,
} from "./support";

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
        ip: seedNodeAddress,
        ssh_user: "root",
      },
    });
    expect(server.status(), await server.text()).toBe(201);
    const { server: node } = (await server.json()) as {
      server: { id: string };
    };

    const { environmentId } = await seedProjectEnvironment(request, headers);
    const created = await request.post("/api/v1/applications", {
      headers,
      data: {
        name,
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

    // The detail view fires its history, config and node reads after the
    // application row arrives. Await each dependent response (any status —
    // a failure still resolves the wait and lands in the guardrail below),
    // otherwise a late failure can slip past the assertion and the smoke
    // still passes. Waiters register before the click so an already-answered
    // read cannot be missed.
    const detailPrefix = `/api/v1/applications/${application.id}`;
    const settled = Promise.all([
      page.waitForResponse((response) =>
        response.url().includes(`${detailPrefix}/deployments`),
      ),
      page.waitForResponse((response) =>
        response.url().includes(`${detailPrefix}/env`),
      ),
      page.waitForResponse((response) =>
        response.url().includes(`${detailPrefix}/storages`),
      ),
      page.waitForResponse((response) =>
        response.url().includes(`${detailPrefix}/previews`),
      ),
      // The rail polls the same endpoint on its own interval, so this waiter
      // can resolve on a poll rather than the detail's own refetch; the five
      // app-scoped waiters above are exact.
      page.waitForResponse((response) =>
        response.url().endsWith("/api/v1/servers"),
      ),
    ]);
    await row.getByRole("button", { name: "Open" }).click();
    await expect(page).toHaveURL(
      new RegExp(`/applications/${application.id}$`),
    );
    await settled;
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();

    expect(guardrails.apiFailures).toEqual([]);
  });
});
