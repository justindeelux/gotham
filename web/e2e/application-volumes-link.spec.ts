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

/**
 * JUS-273 real-stack proof: the volumes API must name the File Manager root the
 * enrolled agent ACTUALLY exposes (full access => "system"/"/", confined =>
 * "volumes"), and Open in File Manager must open that directory in a new tab
 * while the application tab stays. The spec
 * runs against the node agent started by the UI e2e workflow; without an
 * enrolled agent there is nothing to resolve.
 */

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

/** enrolledNode returns the ready node agent the workflow registered. */
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

  // The target root must be one the node itself advertises, and the joined
  // root path must be the volume's host path. A hard-coded root fails here in
  // full-access mode ("system", not "volumes").
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

  // Seed the real directory and a marker through the File Manager (test setup;
  // the feature itself only ever reads). A pre-existing directory/file is fine.
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

  // The agent may report a resolved root path (/tmp -> /private/tmp on macOS),
  // so compare the real directories rather than the literal strings.
  expect(realpathSync(join(root.path, target.path))).toBe(
    realpathSync(volume.host_path ?? ""),
  );

  await page.goto(
    `${nestedURL(projectId, environmentId, "applications", application.id)}?tab=storage`,
  );
  await expect(page.getByText(volume.host_path ?? "", { exact: false })).toBeVisible();
  const open = page.getByRole("link", {
    name: `Open volume ${volume.name} in File Manager`,
  });
  await expect(open).toBeVisible();

  // The action is a real link: middle-click and copy-link reach the same
  // directory, and a plain click must not navigate the application tab.
  const openHref = await open.getAttribute("href");
  expect(openHref, "the open action carries an href").toBeTruthy();
  const href = new URL(openHref as string, page.url());
  expect(href.pathname).toBe(`/servers/${node.id}/files`);
  expect(href.searchParams.get("root")).toBe(target.root);
  expect(href.searchParams.get("path")).toBe(target.path);
  expect(await open.getAttribute("target")).toBe("_blank");
  expect(await open.getAttribute("rel")).toBe("noopener");

  const applicationURL = page.url();
  const [opened] = await Promise.all([page.waitForEvent("popup"), open.click()]);
  await opened.waitForURL(`**/servers/${node.id}/files?**`);

  const landed = new URL(opened.url());
  expect(landed.pathname).toBe(`/servers/${node.id}/files`);
  expect(landed.searchParams.get("root")).toBe(target.root);
  expect(landed.searchParams.get("path")).toBe(target.path);
  await expect(opened.getByText("marker.txt", { exact: true }).first()).toBeVisible();

  // The original tab stays on the application page, still showing the volume.
  expect(page.url()).toBe(applicationURL);
  await expect(page.getByText(volume.host_path ?? "", { exact: false })).toBeVisible();

  // Re-opening the same link in the new tab shows the directory once instead
  // of stacking duplicate File Manager tabs.
  await opened.goto(opened.url());
  await expect(opened.getByText("marker.txt", { exact: true }).first()).toBeVisible();
  await expect(opened.locator(".fm-tab").filter({ hasText: "web_data" })).toHaveCount(1);
  await opened.close();

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

  // An unknown root is rejected with a localized notice naming the link and
  // the root the browser stayed on; it never silently shows another directory.
  await page.goto(`/servers/${node.id}/files?root=nope&path=elsewhere`);
  await expect(
    page.getByText(/root “nope” is not available on this server/),
  ).toBeVisible();
  await expect(page.getByText(roots[0].name, { exact: true }).first()).toBeVisible();

  // An escaping path is rejected the same way.
  await page.goto(
    `/servers/${node.id}/files?root=${encodeURIComponent(roots[0].name)}&path=..%2Fetc`,
  );
  await expect(page.getByText(/not a valid location/)).toBeVisible();

  expect(guardrails.apiFailures).toEqual([]);
});
