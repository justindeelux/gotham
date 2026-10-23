import { realpathSync } from "node:fs";
import { join } from "node:path";

import type { APIRequestContext } from "@playwright/test";

import { expect, test } from "./fixtures";
import {
  loadAccount,
  nestedURL,
  seedProjectEnvironment,
  storageStatePath,
  uniqueSuffix,
} from "./support";

test.use({ storageState: storageStatePath });

interface ServerRow {
  id: string;
  name: string;
  status: string;
}

interface FileRootRow {
  name: string;
  path: string;
  writable: boolean;
}

interface VolumeBody {
  runtime: string;
  volumes: Array<{
    name: string;
    host_path?: string;
    file_manager?: { server_id: string; root: string; path: string };
    open_reason?: string;
  }>;
}

async function enrolledNode(
  request: APIRequestContext,
  headers: Record<string, string>,
): Promise<ServerRow | undefined> {
  const response = await request.get("/api/v1/servers", { headers });
  expect(response.status(), await response.text()).toBe(200);
  const body = (await response.json()) as { servers: ServerRow[] };
  return body.servers.find((server) => server.status === "ready");
}

test("volume link uses the root the agent exposes and opens the directory", async ({
  page,
  guardrails,
  request,
}) => {
  const account = loadAccount();
  const headers = { Authorization: `Bearer ${account.accessToken}` };
  const node = await enrolledNode(request, headers);
  test.skip(node === undefined, "no enrolled node agent in this environment");
  if (node === undefined) {
    return;
  }

  const { projectId, environmentId } = await seedProjectEnvironment(request, headers);
  const created = await request.post("/api/v1/applications", {
    headers,
    data: {
      name: `ui-e2e-volume-${uniqueSuffix()}`,
      environment_id: environmentId,
      provider: "",
      repo: "",
      clone_url: "",
      source_type: "image",
      image_ref: "nginx:1.25",
      server_id: node.id,
      storage: [{ name: "web_data", host_path: "", container_path: "/data" }],
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
  const body = (await listing.json()) as VolumeBody;
  expect(body.runtime).toBe("available");
  expect(body.volumes).toHaveLength(1);
  const volume = body.volumes[0];
  expect(volume.name).toBe("web_data");
  expect(volume.open_reason).toBeUndefined();
  const target = volume.file_manager;
  expect(target, "the volume has an openable directory").toBeTruthy();
  if (!target) {
    return;
  }

  const rootsResponse = await request.get(
    `/api/v1/servers/${node.id}/files/roots`,
    { headers },
  );
  expect(rootsResponse.status(), await rootsResponse.text()).toBe(200);
  const { roots } = (await rootsResponse.json()) as { roots: FileRootRow[] };
  const root = roots.find((entry) => entry.name === target.root);
  expect(root, `root ${target.root} is exposed by the agent`).toBeTruthy();
  if (!root) {
    return;
  }
  expect(target.server_id).toBe(node.id);

  const parent = target.path.split("/").slice(0, -1).join("/");
  for (const path of [parent, target.path]) {
    if (path === "") {
      continue;
    }
    const made = await request.post(`/api/v1/servers/${node.id}/files/mkdir`, {
      headers,
      data: { root: target.root, path },
    });
    expect([201, 409], `mkdir ${path}: ${await made.text()}`).toContain(made.status());
  }
  const marker = `${target.path}/marker.txt`;
  const file = await request.post(`/api/v1/servers/${node.id}/files/create`, {
    headers,
    data: { root: target.root, path: marker },
  });
  expect([201, 409], `create ${marker}: ${await file.text()}`).toContain(file.status());

  expect(realpathSync(join(root.path, target.path))).toBe(
    realpathSync(volume.host_path ?? ""),
  );

  await page.goto(
    `${nestedURL(projectId, environmentId, "applications", application.id)}?tab=storage`,
  );
  await expect(page.getByText(volume.host_path ?? "", { exact: false })).toBeVisible();
  const open = page.getByRole("button", {
    name: `Open volume ${volume.name} in File Manager`,
  });
  await expect(open).toBeVisible();

  const applicationURL = page.url();
  await open.click();
  await page.waitForURL(`**/servers/${node.id}/files?**`);
  const landed = new URL(page.url());
  expect(landed.pathname).toBe(`/servers/${node.id}/files`);
  expect(landed.searchParams.get("root")).toBe(target.root);
  expect(landed.searchParams.get("path")).toBe(target.path);
  expect(page.url()).not.toBe(applicationURL);
  await expect(page.getByText("marker.txt", { exact: true }).first()).toBeVisible();

  const volumeTab = page.locator(".fm-tab").filter({ hasText: volume.name });
  await expect(volumeTab).toHaveCount(1);
  await expect(volumeTab).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".fm-tab")).toHaveCount(2);

  await page.goBack();
  await expect(page.getByText(volume.host_path ?? "", { exact: false })).toBeVisible();
  await open.click();
  await page.waitForURL(`**/servers/${node.id}/files?**`);
  await expect(page.getByText("marker.txt", { exact: true }).first()).toBeVisible();
  await expect(page.locator(".fm-tab")).toHaveCount(2);
  await expect(volumeTab).toHaveCount(1);
  await expect(volumeTab).toHaveAttribute("aria-selected", "true");

  expect(guardrails.apiFailures).toEqual([]);
});

test("an invalid deep link explains the fallback instead of failing silently", async ({
  page,
  guardrails,
  request,
}) => {
  const account = loadAccount();
  const headers = { Authorization: `Bearer ${account.accessToken}` };
  const node = await enrolledNode(request, headers);
  test.skip(node === undefined, "no enrolled node agent in this environment");
  if (node === undefined) {
    return;
  }
  const rootsResponse = await request.get(
    `/api/v1/servers/${node.id}/files/roots`,
    { headers },
  );
  expect(rootsResponse.status(), await rootsResponse.text()).toBe(200);
  const { roots } = (await rootsResponse.json()) as { roots: FileRootRow[] };
  expect(roots.length).toBeGreaterThan(0);

  await page.goto(`/servers/${node.id}/files?root=nope&path=elsewhere`);
  await expect(
    page.getByText(/root “nope” is not available on this server/),
  ).toBeVisible();
  await expect(page.getByText(roots[0].name, { exact: true }).first()).toBeVisible();

  await page.goto(
    `/servers/${node.id}/files?root=${encodeURIComponent(roots[0].name)}&path=..%2Fetc`,
  );
  await expect(page.getByText(/not a valid location/)).toBeVisible();

  expect(guardrails.apiFailures).toEqual([]);
});
