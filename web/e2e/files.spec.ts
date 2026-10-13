import { expect, test } from "./fixtures";
import { test as plainTest } from "@playwright/test";
import type { APIRequestContext, Page, Route } from "@playwright/test";

import {
  loadAccount,
  seedNodeAddress,
  storageStatePath,
  uniqueSuffix,
} from "./support";

// Reuse the authenticated session from global setup; the seeds authenticate
// separately with the access token.
test.use({ storageState: storageStatePath });

/**
 * Phase 18 file manager (JUS-115). CI runs without an enrolled node agent, so
 * every files call is answered at the network boundary with `page.route` while
 * the surrounding surfaces (server lookup, teams/roles, the SPA shell) stay
 * real. The control plane is still started for the SPA and the seeded server
 * row; only the agent-backed `**\/servers/*\/files/**` routes are mocked.
 */

/** One `/servers/*\/files/**` response shape (mirrors schemas/files.ts). */
type WireKind = "file" | "directory" | "symlink" | "special" | "unspecified";

interface WireEntry {
  name: string;
  kind: WireKind;
  size: number;
  mode: number;
  modified_at: string;
  etag: string;
}

interface WireRoot {
  name: string;
  path: string;
  writable: boolean;
  free_bytes: number;
}

/** The read-friendly tree the browse scenarios render. */
const writableRoot: WireRoot = {
  name: "srv",
  path: "/srv",
  writable: true,
  free_bytes: 1073741824,
};

/** A ComposeGuard-managed root: readable, never writable from the UI. */
const readOnlyRoot: WireRoot = {
  name: "compose",
  path: "/opt/gotham/compose",
  writable: false,
  free_bytes: 5242880,
};

/** entry builds one listing row with a fixed timestamp. */
function entry(
  name: string,
  kind: WireKind,
  size: number,
  mode: number,
  etag: string,
): WireEntry {
  return { name, kind, size, mode, modified_at: "2026-01-02T03:04:05Z", etag };
}

/** The directory the browse scenario opens. */
const browseEntries: WireEntry[] = [
  entry("conf", "directory", 4096, 0o040755, "etag-conf"),
  entry("app.yaml", "file", 2048, 0o100644, "etag-yaml"),
  entry("logo.png", "file", 1048576, 0o100644, "etag-png"),
];

/** One editable text file body (GET /content). */
const textContent = {
  content: "first line\n",
  etag: "etag-1",
  sha256: "aa",
  size: 11,
  mode: 0o100644,
};

/** One committed write (PUT /content). */
const writeEnvelope = {
  entry: entry("app.yaml", "file", 14, 0o100644, "etag-2"),
  created: false,
  write_mode: "atomic",
};

/** seedServer registers a node row the file pages can be addressed on. */
async function seedServer(
  api: APIRequestContext,
  suffix: string,
): Promise<{ id: string; name: string }> {
  const account = loadAccount();
  const headers = { Authorization: `Bearer ${account.accessToken}` };
  const name = `ui-e2e-files-${suffix}`;
  const response = await api.post("/api/v1/servers", {
    headers,
    data: { name, ip: seedNodeAddress, ssh_user: "root" },
  });
  expect(response.status(), await response.text()).toBe(201);
  const { server } = (await response.json()) as {
    server: { id: string; name: string };
  };
  return { id: server.id, name: server.name };
}

/** json answers one route with a JSON body and status. */
function json(route: Route, status: number, body: unknown): Promise<void> {
  return route.fulfill({ status, json: body });
}

/** FilesHandler answers one mocked files endpoint. */
type FilesHandler = (_route: Route) => void | Promise<void>;

/**
 * routeFiles installs one dispatcher over the server-scoped files API. The
 * handler key is the endpoint's last path segment, so a query string never
 * breaks the match. An unmocked endpoint answers 500 (the guardrail then names
 * the missing mock) instead of leaking a real agent-less request.
 */
async function routeFiles(
  page: Page,
  handlers: Partial<Record<string, FilesHandler>>,
): Promise<void> {
  await page.route(/\/api\/v1\/servers\/[^/]+\/files\//, async (route) => {
    const path = new URL(route.request().url()).pathname;
    const endpoint = path.slice(path.lastIndexOf("/") + 1);
    const handler = handlers[endpoint];
    if (!handler) {
      await json(route, 500, { message: `unrouted files call: ${path}` });
      return;
    }
    await handler(route);
  });
}

/** A listing response for the current (mutable) entry set. */
function listing(entries: WireEntry[]): {
  entries: WireEntry[];
  truncated: boolean;
  cursor: string;
} {
  return { entries, truncated: false, cursor: "" };
}

test.describe("files browse", () => {
  test("renders roots, access labels and directory entries", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) =>
        json(route, 200, { roots: [writableRoot, readOnlyRoot] }),
      list: (route) => json(route, 200, listing(browseEntries)),
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(
      page.getByRole("heading", { name: "File browser", exact: true }),
    ).toBeVisible();

    // The default root is the first writable one and its access is labelled.
    await expect(page.locator(".fm-root-select")).toContainText("srv · writable");
    await page.locator(".fm-root-select").click();
    await expect(
      page.locator(".n-base-select-option").filter({ hasText: "compose · read-only" }),
    ).toBeVisible();
    await page.keyboard.press("Escape");

    const table = page.locator(".fm-table");
    await expect(table.getByText("app.yaml")).toBeVisible();
    await expect(table.getByText("logo.png")).toBeVisible();
    await expect(table.getByText("conf", { exact: true })).toBeVisible();

    // Kind icons and rendered sizes come from the entry metadata.
    await expect(table.locator('.fm-mark[data-kind="folder"]')).toHaveCount(1);
    await expect(table.locator('.fm-mark[data-kind="doc"]')).toHaveCount(1);
    await expect(table.locator('.fm-mark[data-kind="image"]')).toHaveCount(1);
    await expect(table.getByText("2.00 KiB")).toBeVisible();
    await expect(table.getByText("1.00 MiB")).toBeVisible();
    await expect(page.getByText("Free space: 1.00 GiB")).toBeVisible();

    // The chooser forwards to the only/last-used server; both surfaces are
    // valid depending on how many servers the shared run has seeded.
    await page.goto("/files");
    await expect(
      page
        .getByRole("heading", { name: "File manager" })
        .or(page.getByRole("heading", { name: "File browser", exact: true })),
    ).toBeVisible();
  });
});

test.describe("files upload", () => {
  test("queues a file and reports the per-file success", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => json(route, 200, listing([])),
      upload: (route) =>
        json(route, 201, {
          results: [
            {
              name: "hello.txt",
              path: "hello.txt",
              entry: entry("hello.txt", "file", 5, 0o100644, "etag-up"),
            },
          ],
        }),
    });

    await page.goto(`/servers/${server.id}/files`);
    await page.getByRole("button", { name: "Upload", exact: true }).click();

    const dialog = page.locator(".n-modal").filter({ hasText: "Upload files" });
    await expect(dialog).toBeVisible();
    await dialog.locator('input[type="file"]').setInputFiles({
      name: "hello.txt",
      mimeType: "text/plain",
      buffer: Buffer.from("hello"),
    });
    await expect(dialog.locator(".row")).toContainText("hello.txt");

    const uploadRequest = page.waitForRequest(
      (candidate) =>
        candidate.method() === "POST" &&
        candidate.url().includes("/files/upload"),
    );
    await dialog.getByRole("button", { name: "Upload", exact: true }).click();
    await uploadRequest;

    await expect(dialog.locator(".row")).toContainText("Done");
    await expect(dialog.getByText("1 done · 0 failed")).toBeVisible();
  });
});

test.describe("files editor", () => {
  test("loads a file, saves it with If-Match and clears the dirty dot", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) =>
        json(
          route,
          200,
          listing([entry("app.yaml", "file", 11, 0o100644, "etag-1")]),
        ),
      content: async (route) => {
        if (route.request().method() === "PUT") {
          await json(route, 200, writeEnvelope);
          return;
        }
        await json(route, 200, textContent);
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    await page.locator(".fm-label").filter({ hasText: "app.yaml" }).click();

    const editor = page.locator(".files-editor");
    const content = editor.locator(".cm-content");
    await expect(content).toBeVisible();

    await content.click();
    await page.keyboard.press("ControlOrMeta+a");
    await page.keyboard.type("edited first line");
    await expect(editor.locator(".ed-dot")).toHaveCount(1);

    const putRequest = page.waitForRequest(
      (candidate) =>
        candidate.method() === "PUT" &&
        candidate.url().includes("/files/content"),
    );
    await editor.getByRole("button", { name: "Save", exact: true }).click();
    const put = await putRequest;

    // The saved request carries the etag read from GET /content.
    expect(put.headers()["if-match"]).toBe("etag-1");
    await expect(page.getByText("Saved app.yaml")).toBeVisible();
    await expect(editor.locator(".ed-dot")).toHaveCount(0);
  });
});

test.describe("files conflict", () => {
  // The stale save answers 409 on purpose; the browser logs the failed
  // resource, the app must render its own conflict modal instead.
  test.use({ expectedConsoleErrors: ["Failed to load resource"] });

  test("a stale save opens the conflict modal and overwrite re-issues the PUT", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    let puts = 0;
    const putCalls: Array<{ url: string; ifMatch: string | undefined }> = [];
    page.on("request", (outgoing) => {
      if (
        outgoing.method() === "PUT" &&
        outgoing.url().includes("/files/content")
      ) {
        putCalls.push({
          url: outgoing.url(),
          ifMatch: outgoing.headers()["if-match"],
        });
      }
    });

    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) =>
        json(
          route,
          200,
          listing([entry("app.yaml", "file", 11, 0o100644, "etag-1")]),
        ),
      content: async (route) => {
        if (route.request().method() === "PUT") {
          puts += 1;
          if (puts === 1) {
            await json(route, 409, { message: "conflict" });
            return;
          }
          await json(route, 200, writeEnvelope);
          return;
        }
        await json(route, 200, textContent);
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    await page.locator(".fm-label").filter({ hasText: "app.yaml" }).click();

    const editor = page.locator(".files-editor");
    const content = editor.locator(".cm-content");
    await expect(content).toBeVisible();
    await content.click();
    await page.keyboard.press("ControlOrMeta+a");
    await page.keyboard.type("edited first line");
    await editor.getByRole("button", { name: "Save", exact: true }).click();

    const conflict = page.locator(".files-conflict-modal");
    await expect(conflict).toBeVisible();
    await expect(conflict.getByText("File changed on the server")).toBeVisible();
    await expect(conflict.getByRole("button", { name: "Reload" })).toBeVisible();
    await expect(
      conflict.getByRole("button", { name: "Save as copy" }),
    ).toBeVisible();

    // Overwrite is guarded by a confirm popover; resolving it force-saves.
    await conflict
      .getByRole("button", { name: "Overwrite", exact: true })
      .click();
    await page
      .locator(".n-popconfirm")
      .getByRole("button", { name: "Overwrite", exact: true })
      .click();
    await expect(conflict).toBeHidden();

    await expect.poll(() => putCalls.length).toBe(2);
    expect(putCalls[0]?.ifMatch).toBe("etag-1");
    expect(putCalls[1]?.url).toContain("overwrite=true");
    expect(putCalls[1]?.ifMatch).toBe("*");
    await expect(page.getByText("Saved app.yaml")).toBeVisible();
  });
});

test.describe("files delete", () => {
  test("deletes a row into the recycle bin and drops it from the refreshed listing", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    let deleted = false;
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) =>
        json(
          route,
          200,
          listing(
            deleted
              ? [entry("keep.txt", "file", 3, 0o100644, "etag-keep")]
              : [
                  entry("keep.txt", "file", 3, 0o100644, "etag-keep"),
                  entry("old.txt", "file", 4, 0o100644, "etag-old"),
                ],
          ),
        ),
      entry: async (route) => {
        deleted = true;
        await route.fulfill({ status: 204 });
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(page.getByText("old.txt")).toBeVisible();
    await page.getByRole("button", { name: "Delete old.txt" }).click();

    const dialog = page.locator(".n-modal").filter({ hasText: "Delete file" });
    await expect(dialog).toBeVisible();
    // Delete always moves to the bin: the dialog offers no permanent option.
    await expect(
      dialog.getByRole("button", { name: "Delete permanently" }),
    ).toHaveCount(0);

    const deleteRequest = page.waitForRequest(
      (candidate) =>
        candidate.method() === "DELETE" &&
        candidate.url().includes("/files/entry"),
    );
    await dialog.getByRole("button", { name: "Move to recycle bin" }).click();
    const deleteCall = await deleteRequest;
    // The normal delete never asks for a permanent removal.
    expect(new URL(deleteCall.url()).searchParams.get("permanent")).not.toBe("true");

    await expect(page.getByText("1 moved to the recycle bin.")).toBeVisible();
    await expect(page.getByText("old.txt")).toHaveCount(0);
    await expect(page.getByText("keep.txt")).toBeVisible();
  });
});

test.describe("files recycle bin", () => {
  test("opens the bin, restores an entry and refreshes the listing", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    let restored = false;
    const restoreBodies: Array<Record<string, unknown>> = [];
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) =>
        json(
          route,
          200,
          listing(
            restored
              ? [entry("old.txt", "file", 4, 0o100644, "etag-old")]
              : [entry("keep.txt", "file", 3, 0o100644, "etag-keep")],
          ),
        ),
      trash: (route) =>
        json(route, 200, {
          entries: restored
            ? []
            : [
                {
                  id: "trash-1",
                  name: "old.txt",
                  original_path: "srv/old.txt",
                  deleted_at: "2026-01-02T03:04:05Z",
                  size: 4,
                  kind: "file",
                },
              ],
          truncated: false,
        }),
      restore: async (route) => {
        restoreBodies.push(route.request().postDataJSON() as Record<string, unknown>);
        restored = true;
        await json(route, 200, {
          entry: entry("old.txt", "file", 4, 0o100644, "etag-old"),
        });
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    await page.getByRole("button", { name: "Recycle bin" }).click();

    const dialog = page.locator(".n-modal").filter({ hasText: "Recycle bin" });
    await expect(dialog).toBeVisible();
    await expect(dialog.getByTitle("old.txt", { exact: true })).toBeVisible();
    await expect(dialog.getByText("srv/old.txt")).toBeVisible();

    await dialog.getByRole("button", { name: "Restore old.txt" }).click();
    await expect.poll(() => restoreBodies.length).toBe(1);
    expect(restoreBodies[0]).toMatchObject({
      root: "srv",
      id: "trash-1",
      destination: "",
      overwrite: false,
    });

    await expect(page.getByText("old.txt restored.")).toBeVisible();
    // The restored entry is back in the browser listing.
    await expect(
      page.locator(".fm-table").getByText("old.txt", { exact: true }),
    ).toBeVisible();
  });
});

test.describe("files chmod", () => {
  test("changes permissions from the row menu and refreshes the listing", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    let mode = 0o100644;
    const chmodBodies: Array<{ root: string; path: string; mode: number }> = [];
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) =>
        json(
          route,
          200,
          listing([entry("app.yaml", "file", 2048, mode, "etag-yaml")]),
        ),
      chmod: async (route) => {
        chmodBodies.push(
          route.request().postDataJSON() as { root: string; path: string; mode: number },
        );
        mode = 0o100600;
        await json(route, 200, {
          entry: entry("app.yaml", "file", 2048, mode, "etag-yaml-2"),
        });
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(page.getByText("app.yaml")).toBeVisible();

    await page
      .getByRole("button", { name: "More actions for app.yaml" })
      .click();
    await page.getByRole("menuitem", { name: "Permissions…" }).click();

    const dialog = page
      .locator(".n-modal")
      .filter({ hasText: "Permissions" });
    await expect(dialog).toBeVisible();
    const octal = dialog.locator(".octal input");
    await expect(octal).toHaveValue("644");

    // JUS-131: the bits grid comes first and the octal field sits below it,
    // with the explanatory copy removed.
    const gridBox = await dialog.locator(".perms-grid").boundingBox();
    const octalBox = await octal.boundingBox();
    expect(gridBox).not.toBeNull();
    expect(octalBox).not.toBeNull();
    expect(octalBox!.y).toBeGreaterThan(gridBox!.y + gridBox!.height);
    for (const removed of [
      "Three octal digits",
      "Special bits",
      "keeps its type",
      "Current mode",
    ]) {
      await expect(dialog).not.toContainText(removed);
    }

    await octal.fill("600");
    await dialog.getByRole("button", { name: "Apply" }).click();

    await expect.poll(() => chmodBodies.length).toBe(1);
    expect(chmodBodies[0]).toEqual({
      root: "srv",
      path: "app.yaml",
      mode: 0o600,
    });
    await expect(page.getByText("Permissions of app.yaml set to 600.")).toBeVisible();
    // The listing refreshed: the row's mode column now shows the new bits.
    await expect(page.locator(".fm-table")).toContainText("-rw-------");
  });
});

test.describe("files archive", () => {
  test("compresses the selection and extracts an archive in place", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    const entries = [
      entry("conf", "directory", 4096, 0o040755, "etag-conf"),
      entry("backup.tar.gz", "file", 128, 0o100644, "etag-tgz"),
    ];
    let created = false;
    const compressBodies: Array<Record<string, unknown>> = [];
    page.on("request", (outgoing) => {
      if (
        outgoing.method() === "POST" &&
        outgoing.url().includes("/files/compress")
      ) {
        compressBodies.push(
          JSON.parse(outgoing.postData() ?? "{}") as Record<string, unknown>,
        );
      }
    });

    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) =>
        json(
          route,
          200,
          listing(
            created
              ? [...entries, entry("conf.zip", "file", 64, 0o100644, "etag-zip")]
              : entries,
          ),
        ),
      compress: async (route) => {
        created = true;
        await json(route, 200, {
          entry: entry("conf.zip", "file", 64, 0o100644, "etag-zip"),
        });
      },
      extract: (route) => route.fulfill({ status: 204 }),
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(page.getByText("backup.tar.gz")).toBeVisible();

    // Compress the selected folder.
    await page.getByLabel("Select conf").check();

    // JUS-128: bulk actions live on the right of the toolbar; the former
    // .fm-bulk bar is gone and the selection count shows.
    await expect(page.locator(".fm-bulk")).toHaveCount(0);
    const toolbar = page.locator(".fm-toolbar");
    await expect(toolbar).toContainText("1 selected");
    await expect(
      toolbar.getByRole("button", { name: "Download", exact: true }),
    ).toBeVisible();

    await page.getByRole("button", { name: "Compress…", exact: true }).click();
    const dialog = page.locator(".n-modal").filter({ hasText: "Compress selection" });
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Compress…", exact: true }).click();

    await expect.poll(() => compressBodies.length).toBe(1);
    expect(compressBodies[0]).toMatchObject({
      root: "srv",
      sources: ["conf"],
      destination: "conf.zip",
      format: "zip",
    });
    await expect(page.getByText("conf.zip created.")).toBeVisible();

    // Extract the archive into the current folder. The listing now holds two
    // archives (seeded backup.tar.gz + the new conf.zip), so scope the row
    // action to backup.tar.gz instead of a page-wide ambiguous locator.
    await page
      .getByRole("row")
      .filter({ hasText: "backup.tar.gz" })
      .getByLabel("Extract here…")
      .click();
    const extractDialog = page
      .locator(".n-modal")
      .filter({ hasText: "Extract here" });
    await expect(extractDialog).toBeVisible();
    const extractRequest = page.waitForRequest(
      (candidate) =>
        candidate.method() === "POST" &&
        candidate.url().includes("/files/extract"),
    );
    await extractDialog
      .getByRole("button", { name: "Extract here…", exact: true })
      .click();
    const extract = await extractRequest;
    expect(JSON.parse(extract.postData() ?? "{}")).toMatchObject({
      root: "srv",
      source: "backup.tar.gz",
      destination: "",
      overwrite: false,
    });
    await expect(page.getByText("Extracted backup.tar.gz.")).toBeVisible();
  });
});

/**
 * The disabled scenario forces a 503 on purpose (that is exactly what
 * FEATURE_FILES=false answers) and the browser logs the 5xx; the guardrail
 * fixture would flag that forced failure, so this half uses the plain
 * Playwright test (mirroring services-regressions.spec.ts). The app itself
 * must still render the disabled state, never a crash or error toast.
 */
plainTest.describe("files disabled", () => {
  plainTest.use({ storageState: storageStatePath });

  plainTest("a 503 renders the disabled state instead of an error", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await page.route(/\/api\/v1\/servers\/[^/]+\/files\//, (route) =>
      json(route, 503, { message: "files are disabled" }),
    );

    await page.goto(`/servers/${server.id}/files`);
    await expect(
      page.getByRole("heading", { name: "File browser", exact: true }),
    ).toBeVisible();
    await expect(page.getByText("Files are disabled")).toBeVisible();
    await expect(
      page.getByText(
        "The file manager is turned off on the control plane (FEATURE_FILES=false). Enable it to browse this server.",
      ),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
  });
});

test.describe("files read-only role", () => {
  test("a read-only member sees no write controls and a read-only root banner", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    // The role is read-only for the active team...
    await page.route("**/api/v1/teams", (route) =>
      json(route, 200, {
        teams: [
          {
            id: "team-read-only",
            name: "Personal",
            is_personal: true,
            role: "read_only",
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
      }),
    );
    // ...and the selected root itself is read-only.
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [readOnlyRoot] }),
      list: (route) =>
        json(
          route,
          200,
          listing([entry("app.yaml", "file", 11, 0o100644, "etag-1")]),
        ),
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(
      page.getByRole("heading", { name: "File browser", exact: true }),
    ).toBeVisible();

    // No mutation affordances and no editor for a read-only member.
    await expect(page.getByRole("button", { name: "New file" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "New folder" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Upload" })).toHaveCount(0);
    await expect(page.getByRole("button", { name: "Rename" })).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "Editor" })).toHaveCount(0);
    // Reading a body needs owner/admin, so the row download stays disabled.
    await expect(page.getByRole("button", { name: "Download app.yaml" })).toBeDisabled();

    // The read-only banner explains the frozen write surface.
    await expect(
      page.getByRole("heading", { name: "This root is read-only" }),
    ).toBeVisible();
  });
});
