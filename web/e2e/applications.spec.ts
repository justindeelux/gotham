import type { APIRequestContext, Page } from "@playwright/test";

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

/** seedNode registers one configuration-only node for an application seed. */
async function seedNode(
  request: APIRequestContext,
  headers: Record<string, string>,
): Promise<string> {
  const server = await request.post("/api/v1/servers", {
    headers,
    data: {
      name: `ui-e2e-node-${uniqueSuffix()}`,
      ip: seedNodeAddress,
      ssh_user: "root",
    },
  });
  expect(server.status(), await server.text()).toBe(201);
  const { server: node } = (await server.json()) as { server: { id: string } };
  return node.id;
}

/**
 * waitForDetailReads registers the detail page's dependent reads before a
 * navigation, so a late failure can never slip past the guardrail below.
 */
function waitForDetailReads(page: Page, applicationId: string): Promise<unknown[]> {
  const detailPrefix = `/api/v1/applications/${applicationId}`;
  return Promise.all([
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
    // can resolve on a poll rather than the detail's own refetch.
    page.waitForResponse((response) =>
      response.url().endsWith("/api/v1/servers"),
    ),
  ]);
}

// Reuse the authenticated session so the page starts signed in; the seed call
// authenticates separately with the access token from global setup.
test.use({ storageState: storageStatePath });

/**
 * QA-4.1b (c): seed one server row and one application row through the API
 * (configuration only — no deployment, so nothing is cloned) and prove the
 * application appears on the environment page and opens on its nested
 * detail page (PE-5 routes).
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

    const { projectId, environmentId } = await seedProjectEnvironment(request, headers);
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
        server_id: node.id,
      },
    });
    expect(created.status(), await created.text()).toBe(201);
    const { application } = (await created.json()) as {
      application: { id: string; name: string };
    };
    expect(application.name).toBe(name);

    await page.goto(environmentURL(projectId, environmentId));
    await expect(
      page.locator(".environment-page .title"),
    ).toBeVisible();

    const row = page.locator(".resource-table tbody tr").filter({ hasText: name });
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
    // The whole row opens the resource: the name link stretches over it,
    // so a pointer click on a plain cell (here the server cell) lands on
    // the overlay and navigates. Playwright's hit-target check flags that
    // interception by design, hence force: the event still goes through
    // the overlay to the link.
    await row.locator("td").nth(2).click({ force: true });
    await expect(page).toHaveURL(
      nestedURL(projectId, environmentId, "applications", application.id),
    );
    await settled;
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();

    // JUS-274: the header avatar comes from the application's source metadata
    // (the seed uses provider github), not from its name.
    const avatar = page.locator(".page-head .brand-icon");
    await expect(avatar).toBeVisible();
    const avatarBox = await avatar.boundingBox();
    expect(avatarBox, "avatar renders a box").not.toBeNull();
    expect(avatarBox!.width).toBe(44);
    await expect(avatar).toHaveCSS("background-color", "rgb(110, 118, 129)");
    await expect(avatar.locator(".brand-icon__glyph")).toHaveCount(1);

    expect(guardrails.apiFailures).toEqual([]);
  });

  // JUS-238: the single-container Storage tab does not apply to a compose
  // source (volumes live in the compose document), so the tab is absent and a
  // stale ?tab=storage deep link falls back to Overview.
  test("compose application hides Storage and falls back to Overview", async ({
    page,
    guardrails,
    request,
  }) => {
    const account = loadAccount();
    const name = `ui-e2e-compose-${uniqueSuffix()}`;
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const nodeId = await seedNode(request, headers);
    const { projectId, environmentId } = await seedProjectEnvironment(request, headers);

    const created = await request.post("/api/v1/applications", {
      headers,
      data: {
        name,
        environment_id: environmentId,
        provider: "",
        repo: "",
        clone_url: "",
        source_type: "compose",
        compose_content: "services:\n  web:\n    image: nginx:1.25\n",
        compose_service: "web",
        server_id: nodeId,
      },
    });
    expect(created.status(), await created.text()).toBe(201);
    const { application } = (await created.json()) as {
      application: { id: string; name: string };
    };

    const settled = waitForDetailReads(page, application.id);
    await page.goto(
      `${nestedURL(projectId, environmentId, "applications", application.id)}?tab=storage`,
    );
    await settled;
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();

    const tabs = page.locator(".n-tabs-tab");
    await expect(tabs.filter({ hasText: "Storage" })).toHaveCount(0);
    await expect(page.locator(".n-tabs-tab.n-tabs-tab--active")).toHaveText("Overview");
    await expect(
      page.getByText("declared in the compose source", { exact: false }),
    ).toBeVisible();

    expect(guardrails.apiFailures).toEqual([]);
  });

  // Image sources keep the single-container volume editor unchanged.
  test("image application keeps the Storage tab", async ({
    page,
    guardrails,
    request,
  }) => {
    const account = loadAccount();
    const name = `ui-e2e-image-${uniqueSuffix()}`;
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const nodeId = await seedNode(request, headers);
    const { projectId, environmentId } = await seedProjectEnvironment(request, headers);

    const created = await request.post("/api/v1/applications", {
      headers,
      data: {
        name,
        environment_id: environmentId,
        provider: "",
        repo: "",
        clone_url: "",
        source_type: "image",
        image_ref: "nginx:1.25",
        server_id: nodeId,
      },
    });
    expect(created.status(), await created.text()).toBe(201);
    const { application } = (await created.json()) as {
      application: { id: string; name: string };
    };

    const settled = waitForDetailReads(page, application.id);
    await page.goto(
      `${nestedURL(projectId, environmentId, "applications", application.id)}?tab=storage`,
    );
    await settled;
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();

    // The deep link is applied once the row loads, and the tab stays visible
    // for a non-compose source.
    const storageTab = page.locator(".n-tabs-tab").filter({ hasText: "Storage" });
    await expect(storageTab).toHaveCount(1);
    await expect(page.locator(".n-tabs-tab.n-tabs-tab--active")).toHaveText("Storage");
    await expect(page.getByText("Volumes", { exact: true })).toBeVisible();

    expect(guardrails.apiFailures).toEqual([]);
  });
});
