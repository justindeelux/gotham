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
      response.url().includes(`${detailPrefix}/volumes`),
    ),
    page.waitForResponse((response) =>
      response.url().includes(`${detailPrefix}/previews`),
    ),
    page.waitForResponse((response) =>
      response.url().endsWith("/api/v1/servers"),
    ),
  ]);
}

test.use({ storageState: storageStatePath });

test.describe("applications", () => {
  test("lists an API-seeded application and opens its detail page", async ({
    page,
    guardrails,
    request,
  }) => {
    const account = loadAccount();
    const name = `ui-e2e-${uniqueSuffix()}`;
    const headers = { Authorization: `Bearer ${account.accessToken}` };

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
      page.waitForResponse((response) =>
        response.url().endsWith("/api/v1/servers"),
      ),
    ]);
    await row.locator("td").nth(2).click({ force: true });
    await expect(page).toHaveURL(
      nestedURL(projectId, environmentId, "applications", application.id),
    );
    await settled;
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();

    const avatar = page.locator(".page-head .brand-icon");
    await expect(avatar).toBeVisible();
    const avatarBox = await avatar.boundingBox();
    expect(avatarBox, "avatar renders a box").not.toBeNull();
    expect(avatarBox!.width).toBe(44);
    await expect(avatar).toHaveCSS("background-color", "rgb(110, 118, 129)");
    await expect(avatar.locator(".brand-icon__glyph")).toHaveCount(1);

    expect(guardrails.apiFailures).toEqual([]);
  });

  test("compose application shows the Storage list and the compose hint", async ({
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
        compose_content: [
          "services:",
          "  web:",
          "    image: nginx:1.25",
          "    volumes:",
          "      - shared:/var/www/html",
          "  worker:",
          "    image: busybox:1.36",
          "    volumes:",
          "      - shared:/work",
          "volumes:",
          "  shared: {}",
          "",
        ].join("\n"),
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
    await expect(tabs.filter({ hasText: "Storage" })).toHaveCount(1);
    await expect(page.locator(".n-tabs-tab.n-tabs-tab--active")).toHaveText("Storage");
    await expect(page.getByText("shared", { exact: true })).toHaveCount(1);
    await expect(page.getByText("/var/www/html · web", { exact: true })).toBeVisible();
    await expect(page.getByText("/work · worker", { exact: true })).toBeVisible();
    await expect(page.getByText("Compose volumes", { exact: true })).toBeVisible();
    await expect(
      page.getByText("declared in the compose source", { exact: false }).first(),
    ).toBeVisible();
    await expect(page.getByText("Storage configuration", { exact: true })).toHaveCount(0);

    expect(guardrails.apiFailures).toEqual([]);
  });

  test("keeps the volume and explains an unreachable node", async ({
    page,
    guardrails,
    request,
  }) => {
    const account = loadAccount();
    const name = `ui-e2e-volume-${uniqueSuffix()}`;
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
        storage: [{ name: "data", host_path: "", container_path: "/data" }],
      },
    });
    expect(created.status(), await created.text()).toBe(201);
    const { application } = (await created.json()) as {
      application: { id: string };
    };

    const listing = await request.get(
      `/api/v1/applications/${application.id}/volumes`,
      { headers },
    );
    expect(listing.status(), await listing.text()).toBe(200);
    const body = (await listing.json()) as {
      source_type: string;
      runtime: string;
      reason?: string;
      volumes: Array<{
        name: string;
        kind: string;
        host_path?: string;
        state: string;
        mounts: Array<{ destination: string; read_only: boolean; state: string }>;
        file_manager?: { server_id: string; root: string; path: string };
        open_reason?: string;
      }>;
    };
    expect(body.source_type).toBe("image");
    expect(body.runtime).toBe("unavailable");
    expect(body.reason).toBeTruthy();
    expect(body.volumes).toHaveLength(1);
    const volume = body.volumes[0];
    expect(volume.name).toBe("data");
    expect(volume.kind).toBe("managed");
    expect(volume.state).toBe("unverified");
    expect(volume.mounts).toEqual([
      { destination: "/data", read_only: false, state: "unverified" },
    ]);
    expect(volume.file_manager).toBeUndefined();
    expect(volume.open_reason).toBe("root_unavailable");

    const settled = waitForDetailReads(page, application.id);
    await page.goto(
      `${nestedURL(projectId, environmentId, "applications", application.id)}?tab=storage`,
    );
    await settled;
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();
    await expect(
      page.getByText("did not answer the container listing", { exact: false }),
    ).toBeVisible();
    await expect(
      page.getByText("No File Manager root on this server exposes the directory", {
        exact: false,
      }),
    ).toBeVisible();
    await expect(
      page.getByRole("button", { name: /Open volume .* in File Manager/ }),
    ).toHaveCount(0);

    expect(guardrails.apiFailures).toEqual([]);
  });
});
