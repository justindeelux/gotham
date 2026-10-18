import { expect, test } from "./fixtures";
import { test as plainTest } from "@playwright/test";
import type { APIRequestContext, Locator, Page, Route } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

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
  symlink_target?: string;
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
  symlinkTarget?: string,
): WireEntry {
  return {
    name,
    kind,
    size,
    mode,
    modified_at: "2026-01-02T03:04:05Z",
    etag,
    ...(symlinkTarget === undefined ? {} : { symlink_target: symlinkTarget }),
  };
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
      page.getByRole("region", { name: "File browser" }),
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
        .or(page.getByRole("region", { name: "File browser" })),
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
    await page.locator(".fm-label").filter({ hasText: "app.yaml" }).dblclick();

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

  test("passes an axe scan in the dark and light editor themes", async ({
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
          listing([entry("app.yaml", "file", 24, 0o100644, "etag-1")]),
        ),
      // A comment line exercises the syntax comment colour in both themes.
      content: (route) =>
        json(route, 200, {
          ...textContent,
          content: "# note: contrast\nkey: value\n",
          size: 24,
        }),
    });

    await page.goto(`/servers/${server.id}/files`);
    // Axe reads computed colors; a mid-flight modal/editor transition would
    // measure a faded surface. Freeze transitions for this scan.
    await page.addStyleTag({
      content: "*, *::before, *::after { transition: none !important; animation: none !important; }",
    });
    await page.locator(".fm-label").filter({ hasText: "app.yaml" }).dblclick();
    const editor = page.locator(".files-editor");
    await expect(editor.locator(".cm-content")).toBeVisible();

    /** scan returns the serious/critical findings for one surface. */
    const scan = async (include: string): Promise<string[]> => {
      const results = await new AxeBuilder({ page }).include(include).analyze();
      return results.violations
        .filter(
          (violation) => violation.impact === "serious" || violation.impact === "critical",
        )
        .map(
          (violation) =>
            `${violation.id}: ${violation.help} @ ${violation.nodes
              .map(
                (node) =>
                  `${node.target.join(" ")} [${node.failureSummary ?? ""}]`,
              )
              .join(" | ")}`,
        );
    };

    // T-A11Y-01: the open editor (chrome, tabs, toolbar, explorer and pane)
    // must have zero serious/critical axe findings in the default dark theme.
    expect(await scan(".files-editor")).toEqual([]);

    // JUS-231: the custom find bar is a new editor surface. Search with the
    // replace row open; Escape closes only the bar and keeps the window.
    await editor.getByRole("button", { name: "Find / replace" }).click();
    const findBar = page.locator(".ed-find");
    await expect(findBar).toBeVisible();
    await findBar.locator("input").first().fill("key");
    await expect(findBar.locator(".ed-find-count")).toHaveText("1 of 1");
    expect(await scan(".ed-find")).toEqual([]);
    await page.keyboard.press("Escape");
    await expect(findBar).toBeHidden();
    await expect(editor).toBeVisible();

    // The shortcut sheet is an editor surface too: same bar, including the
    // dialog focus/name semantics.
    await editor.locator(".ed-window-shortcuts").click();
    const sheet = page.locator(".n-modal").filter({ hasText: "Keyboard shortcuts" });
    await expect(sheet).toBeVisible();
    expect(await scan(".n-modal")).toEqual([]);

    // T-A11Y-03 / the topmost-surface rule: Escape closes the sheet, keeps the
    // window open, and returns focus to the header trigger.
    await page.keyboard.press("Escape");
    await expect(sheet).toBeHidden();
    await expect(editor).toBeVisible();
    await expect(editor.locator(".ed-window-shortcuts")).toBeFocused();

    // The settings popover can switch the editor to Light; the same scan must
    // hold there (syntax comment/gutter colours are per-theme).
    const settings = editor.getByRole("button", { name: "Settings" });
    await settings.click();
    const panel = page.locator(".ed-settings");
    await expect(panel).toBeVisible();
    await panel.locator(".n-base-selection").first().click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: "Light" })
      .click();
    await settings.click();
    await expect(panel).toBeHidden();

    // The same bar must hold in the light theme (its surfaces are tokens).
    await editor.getByRole("button", { name: "Find / replace" }).click();
    await expect(findBar).toBeVisible();
    await findBar.locator("input").first().fill("key");
    expect(await scan(".ed-find")).toEqual([]);
    await page.keyboard.press("Escape");
    await expect(findBar).toBeHidden();

    expect(await scan(".files-editor")).toEqual([]);
  });

  test("drags by the title bar and closes every tab from the close control", async ({
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
          listing([
            entry("app.yaml", "file", 11, 0o100644, "etag-1"),
            entry("notes.md", "file", 12, 0o100644, "etag-2"),
          ]),
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
    await page.locator(".fm-label").filter({ hasText: "app.yaml" }).dblclick();

    const editor = page.locator(".files-editor");
    await expect(editor.locator(".cm-content")).toBeVisible();

    // Drag the title bar up and to the left: the window follows and stays on
    // screen (never a negative offset).
    const window_ = page.locator(".ed-window");
    const before = (await window_.boundingBox())!;
    const bar = page.locator(".ed-window-bar");
    const barBox = (await bar.boundingBox())!;
    await page.mouse.move(barBox.x + 40, barBox.y + barBox.height / 2);
    await page.mouse.down();
    await page.mouse.move(barBox.x - 220, barBox.y - 200, { steps: 8 });
    await page.mouse.up();
    const after = (await window_.boundingBox())!;
    expect(after.x).toBeLessThan(before.x);
    expect(after.y).toBeLessThan(before.y);
    expect(after.x).toBeGreaterThanOrEqual(0);
    expect(after.y).toBeGreaterThanOrEqual(0);

    // A second tab, dirtied: close-all must ask before discarding.
    await page.locator(".ed-window-min").click();
    await page.locator(".fm-label").filter({ hasText: "notes.md" }).dblclick();
    const visibleContent = editor.locator(".ed-host:visible .cm-content");
    await expect(visibleContent).toBeVisible();
    await visibleContent.click();
    await page.keyboard.type("edited");
    await expect(editor.locator(".ed-dot")).toHaveCount(1);

    await editor.locator(".ed-window-close").click();
    const dialog = page.locator(".n-modal").filter({ hasText: "Close all files?" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("notes.md");
    // Only the dirty buffer is listed; the clean app.yaml tab is not.
    await expect(dialog.locator(".ed-unsaved")).not.toContainText("app.yaml");

    // Cancel keeps the editor; discard closes every tab.
    await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(page.locator(".ed-window")).toBeVisible();
    await editor.locator(".ed-window-close").click();
    await dialog
      .getByRole("button", { name: "Discard and close", exact: true })
      .click();
    await expect(page.locator(".ed-window")).toHaveCount(0);
    await expect(page.locator(".ed-dock")).toHaveCount(0);
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
    await page.locator(".fm-label").filter({ hasText: "app.yaml" }).dblclick();

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
    // JUS-155: the bin no longer shows the entry's original path.
    await expect(dialog.getByText("srv/old.txt")).toHaveCount(0);

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
    // The listing refreshed: the row's mode column now shows the new octal bits
    // (JUS-134) with the rwx form in its tooltip.
    const modeCell = page.locator(".fm-table .fm-mode").first();
    await expect(modeCell).toHaveText("600");
    await expect(modeCell).toHaveAttribute("title", "-rw-------");
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

    // JUS-128/146: the bulk actions and their count live in the header bar
    // that replaces the column-header row; the former .fm-bulk bar is gone.
    await expect(page.locator(".fm-bulk")).toHaveCount(0);
    const bar = page.locator(".fm-head-bulk");
    await expect(bar).toContainText("1 selected");
    await expect(
      bar.getByRole("button", { name: "Download", exact: true }),
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
      page.getByRole("region", { name: "File browser" }),
    ).toBeVisible();
    await expect(page.getByText("Files are disabled")).toBeVisible();
    await expect(
      page.getByText(
        "The file manager is turned off on this control plane. The platform administrator must enable it.",
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
      page.getByRole("region", { name: "File browser" }),
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

/**
 * JUS-130: a double-click dispatches by entry kind — a folder navigates, a
 * text/code file opens the editor, an extractable archive opens the Extract
 * dialog, and anything else (a binary/oversized file, a symlink, a special
 * file) does nothing. A single click only selects.
 */
test.describe("files double-click", () => {
  /** dblclickRow double-clicks the name of one listing row. */
  async function dblclickRow(page: Page, name: string): Promise<void> {
    await page.locator(".fm-label").filter({ hasText: name }).dblclick();
  }

  test("a single click only selects and Ctrl+click multi-selects", async ({
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
          listing([
            entry("conf", "directory", 4096, 0o040755, "etag-conf"),
            entry("app.yaml", "file", 11, 0o100644, "etag-yaml"),
          ]),
        ),
    });

    await page.goto(`/servers/${server.id}/files`);
    const table = page.locator(".fm-table");
    await expect(table.getByText("app.yaml")).toBeVisible();

    // A plain click selects the row; it must not navigate or open anything.
    await table.getByText("app.yaml").click();
    const selected = table.locator(".fm-row.is-selected");
    await expect(selected).toHaveCount(1);
    await expect(selected).toContainText("app.yaml");
    await expect(page.locator(".files-editor")).toHaveCount(0);
    await expect(page.locator(".n-modal")).toHaveCount(0);
    await expect(table.getByText("conf", { exact: true })).toBeVisible();

    // Ctrl/⌘+click adds to the selection instead of replacing it.
    await table.getByText("conf", { exact: true }).click({
      modifiers: ["ControlOrMeta"],
    });
    await expect(selected).toHaveCount(2);
  });

  test("double-clicking a folder navigates into it", async ({ page, request }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => {
        const path = new URL(route.request().url()).searchParams.get("path") ?? "";
        return json(
          route,
          200,
          listing(
            path === "conf"
              ? [entry("inside.txt", "file", 5, 0o100644, "etag-in")]
              : [entry("conf", "directory", 4096, 0o040755, "etag-conf")],
          ),
        );
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    await dblclickRow(page, "conf");
    await expect(page.locator(".fm-table").getByText("inside.txt")).toBeVisible();
  });

  test("double-clicking a text file opens it in the editor", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) =>
        json(route, 200, listing([entry("app.yaml", "file", 11, 0o100644, "etag-1")])),
      content: (route) => json(route, 200, textContent),
    });

    await page.goto(`/servers/${server.id}/files`);
    await dblclickRow(page, "app.yaml");
    await expect(page.locator(".files-editor")).toBeVisible({ timeout: 15_000 });
  });

  test("double-clicking an extractable archive opens the Extract dialog", async ({
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
          listing([entry("backup.tar.gz", "file", 128, 0o100644, "etag-tgz")]),
        ),
    });

    await page.goto(`/servers/${server.id}/files`);
    await dblclickRow(page, "backup.tar.gz");
    const dialog = page.locator(".n-modal").filter({ hasText: "Extract here" });
    await expect(dialog).toBeVisible();
    await expect(
      dialog.getByRole("button", { name: "Extract here…", exact: true }),
    ).toBeVisible();
  });

  test("double-clicking a binary file does nothing", async ({ page, request }) => {
    const server = await seedServer(request, uniqueSuffix());
    // A non-image binary: images now open the viewer (JUS-204), so the
    // no-op case uses a video. (app.bin would open the editor: "bin" is not
    // a known binary extension, so the dispatch treats it as text.)
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) =>
        json(
          route,
          200,
          listing([entry("movie.mp4", "file", 25190, 0o100644, "etag-mp4")]),
        ),
    });

    await page.goto(`/servers/${server.id}/files`);
    await dblclickRow(page, "movie.mp4");
    // No editor tab, no modal, no navigation: the listing is unchanged.
    await expect(page.locator(".files-editor")).toHaveCount(0);
    await expect(page.locator(".n-modal")).toHaveCount(0);
    await expect(page.locator(".fm-table").getByText("movie.mp4")).toBeVisible();
  });

  test("double-clicking an image opens the viewer (JUS-204)", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    // A 1x1 PNG body for the ticket-authorised download the viewer fetches.
    const png = Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
      "base64",
    );
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) =>
        json(
          route,
          200,
          listing([entry("logo.png", "file", 25190, 0o100644, "etag-png")]),
        ),
      "download-ticket": (route) => json(route, 201, { ticket: "ticket-viewer" }),
      download: (route) =>
        route.fulfill({ status: 200, contentType: "image/png", body: png }),
    });

    await page.goto(`/servers/${server.id}/files`);
    await dblclickRow(page, "logo.png");
    const dialog = page.locator(".n-modal").filter({ hasText: "logo.png" });
    await expect(dialog).toBeVisible();
    await expect(dialog.locator(".viewer-stage img")).toBeVisible();

    // Esc closes the viewer; the listing is unchanged.
    await page.keyboard.press("Escape");
    await expect(dialog).toHaveCount(0);
    await expect(page.locator(".fm-table").getByText("logo.png")).toBeVisible();
  });
});

/**
 * JUS-138: shift-click selects the run between the last-clicked row and the
 * clicked one, computed from the sorted+filtered entry order (so it spans rows
 * the windowed list never rendered), in both the list and the grid.
 */
test.describe("files shift-click range selection", () => {
  // Distinct sizes let a sort change reorder the rows (name order: conf,
  // app.yaml, logo.png, notes.md; size order: conf, logo.png, notes.md,
  // app.yaml).
  const rangeEntries: WireEntry[] = [
    entry("conf", "directory", 0, 0o040755, "etag-conf"),
    entry("app.yaml", "file", 300, 0o100644, "etag-yaml"),
    entry("logo.png", "file", 100, 0o100644, "etag-png"),
    entry("notes.md", "file", 200, 0o100644, "etag-md"),
  ];

  /** row locator for one entry name in the list. */
  function listRow(page: Page, name: string): Locator {
    return page.locator(`.fm-row[data-entry="${name}"]`);
  }

  test("selects the run in the list and adds it with ⌘/Ctrl+Shift", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => json(route, 200, listing(rangeEntries)),
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(listRow(page, "notes.md")).toBeVisible();

    // Plain click anchors the run; Shift+click selects every row between.
    await listRow(page, "conf").click();
    await expect(page.locator(".fm-bulk-count")).toContainText("1 selected");
    await listRow(page, "notes.md").click({ modifiers: ["Shift"] });
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(4);
    await expect(page.locator(".fm-bulk-count")).toContainText("4 selected");

    // ⌘/Ctrl+Shift adds the new run to the existing selection.
    await listRow(page, "app.yaml").click();
    await listRow(page, "logo.png").click({ modifiers: ["ControlOrMeta"] });
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(2);
    await listRow(page, "notes.md").click({
      modifiers: ["ControlOrMeta", "Shift"],
    });
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(3);
    await expect(page.locator(".fm-bulk-count")).toContainText("3 selected");
  });

  test("selects the run in the grid", async ({ page, request }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => json(route, 200, listing(rangeEntries)),
    });

    await page.goto(`/servers/${server.id}/files`);
    await page.getByRole("button", { name: "Grid view" }).click();
    await expect(page.locator('.fm-tile-wrap[data-entry="notes.md"]')).toBeVisible();

    await page.locator('.fm-tile-wrap[data-entry="conf"]').click();
    await page
      .locator('.fm-tile-wrap[data-entry="notes.md"]')
      .click({ modifiers: ["Shift"] });

    await expect(page.locator(".fm-grid .fm-tile.is-selected")).toHaveCount(4);
    await expect(page.locator(".fm-bulk-count")).toContainText("4 selected");
  });

  test("spans rows the windowed list did not render", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    const many = Array.from({ length: 5000 }, (_, index) =>
      entry(
        `file-${String(index).padStart(4, "0")}`,
        "file",
        index + 1,
        0o100644,
        `etag-${index}`,
      ),
    );
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => json(route, 200, listing(many)),
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(listRow(page, "file-0000")).toBeVisible();
    await listRow(page, "file-0000").click();

    // Scroll to the far end: the last row is only now rendered, while every row
    // between is not in the DOM at all.
    await page.locator(".table-wrap").evaluate((el) => {
      el.scrollTop = el.scrollHeight;
    });
    await expect(listRow(page, "file-4999")).toBeVisible();
    await listRow(page, "file-4999").click({ modifiers: ["Shift"] });

    await expect(page.locator(".fm-bulk-count")).toContainText("5000 selected");
  });

  test("the sort headers return on Clear while a selection hides them", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => json(route, 200, listing(rangeEntries)),
    });

    await page.goto(`/servers/${server.id}/files`);

    // JUS-146: with nothing selected the sticky header row is the column
    // headers, so a sort is reachable and its aria-sort updates. (The store's
    // "a sort change resets the range anchor" rule is covered directly by
    // web/tests/files-range.test.ts — a sort is no longer reachable while a
    // selection exists.)
    await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "visible");
    await page.getByRole("button", { name: "Size", exact: true }).click();
    await expect(page.locator(".fm-head .fm-cell--size")).toHaveAttribute(
      "aria-sort",
      "ascending",
    );

    // Selecting a row replaces the column headers with the bulk bar, so the
    // sort control (and its aria-sort) is no longer shown...
    await listRow(page, "conf").click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);
    await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "hidden");
    await expect(page.locator(".fm-head-bulk")).toContainText("1 selected");

    // ...and Clear brings the sort headers back.
    await page
      .locator(".fm-selection-actions")
      .getByRole("button", { name: "Clear", exact: true })
      .click();
    await expect(page.locator(".fm-head-cols")).toHaveCSS("visibility", "visible");
    await expect(page.locator(".fm-head .fm-cell--size")).toHaveAttribute(
      "aria-sort",
      "ascending",
    );
  });

  test("shift-click on a row's own controls runs the control and leaves the selection alone", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => json(route, 200, listing(rangeEntries)),
    });

    await page.goto(`/servers/${server.id}/files`);
    // Anchor on app.yaml: a range to logo.png would select a second row.
    await listRow(page, "app.yaml").click();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);

    // Shift+click the ⋮ menu: the menu opens, the selection is untouched.
    await listRow(page, "logo.png")
      .getByRole("button", { name: "More actions for logo.png" })
      .click({ modifiers: ["Shift"] });
    await expect(
      page.getByRole("menu", { name: "More actions for logo.png" }),
    ).toBeVisible();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);
    await page.keyboard.press("Escape");

    // Shift+click the inline Rename action: the dialog opens, selection intact.
    await listRow(page, "logo.png")
      .getByRole("button", { name: "Rename logo.png" })
      .click({ modifiers: ["Shift"] });
    await expect(
      page.locator(".n-modal").filter({ hasText: "Rename entry" }),
    ).toBeVisible();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(1);
  });

  test("a shift+double-click does not activate a row", async ({ page, request }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => {
        const path = new URL(route.request().url()).searchParams.get("path") ?? "";
        return json(
          route,
          200,
          listing(
            path === "conf"
              ? [entry("inside.txt", "file", 5, 0o100644, "etag-in")]
              : [
                  entry("conf", "directory", 4096, 0o040755, "etag-conf"),
                  entry("app.yaml", "file", 2048, 0o100644, "etag-yaml"),
                ],
          ),
        );
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    await listRow(page, "conf").dblclick({ modifiers: ["Shift"] });

    // Still in the root listing: the folder was not opened.
    await expect(listRow(page, "conf")).toBeVisible();
    await expect(page.getByText("inside.txt")).toHaveCount(0);
  });
});

test.describe("files keyboard navigation (JUS-139)", () => {
  // Distinct names let the sorted order be reasoned about: conf (dir),
  // app.yaml, logo.png, notes.md.
  const keysEntries: WireEntry[] = [
    entry("conf", "directory", 0, 0o040755, "etag-conf"),
    entry("app.yaml", "file", 300, 0o100644, "etag-yaml"),
    entry("logo.png", "file", 100, 0o100644, "etag-png"),
    entry("notes.md", "file", 200, 0o100644, "etag-md"),
  ];

  /** row locator for one entry name in the list. */
  function listRow(page: Page, name: string): Locator {
    return page.locator(`.fm-row[data-entry="${name}"]`);
  }

  test("arrows, Space, Shift+Arrow, Ctrl/Cmd+A and type-to-focus drive the list", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => json(route, 200, listing(keysEntries)),
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(listRow(page, "notes.md")).toBeVisible();

    // One roving tab stop: the focused row's name control.
    await expect(page.locator('.fm-label[tabindex="0"]')).toHaveCount(1);

    // Arrow moves focus; Space toggles the focused row's selection.
    await listRow(page, "conf").locator(".fm-label").focus();
    await page.keyboard.press("ArrowDown");
    await expect(listRow(page, "app.yaml").locator(".fm-label")).toBeFocused();
    await page.keyboard.press(" ");
    await expect(page.locator(".fm-bulk-count")).toContainText("1 selected");

    // Shift+Arrow extends the range from the roving anchor.
    await page.keyboard.press("Shift+ArrowDown");
    await expect(listRow(page, "logo.png").locator(".fm-label")).toBeFocused();
    await expect(page.locator(".fm-row.is-selected")).toHaveCount(2);

    // Ctrl/Cmd+A selects every visible entry.
    await page.keyboard.press("ControlOrMeta+a");
    await expect(page.locator(".fm-bulk-count")).toContainText("4 selected");

    // Type-to-focus jumps by prefix and leaves the selection alone.
    await page.keyboard.type("not");
    await expect(listRow(page, "notes.md").locator(".fm-label")).toBeFocused();
    await expect(page.locator(".fm-bulk-count")).toContainText("4 selected");
  });
});

/**
 * JUS-132: the folder filter resets on navigation with a filtered footer
 * count, and a symlink row shows its target and can jump to it.
 */
test.describe("files filter and symlink target", () => {
  test("the folder filter resets on navigation and the footer counts the filtered rows", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => {
        const path = new URL(route.request().url()).searchParams.get("path") ?? "";
        return json(
          route,
          200,
          listing(
            path === "conf"
              ? [entry("inside.txt", "file", 5, 0o100644, "etag-in")]
              : [
                  entry("conf", "directory", 4096, 0o040755, "etag-conf"),
                  entry("app.yaml", "file", 2048, 0o100644, "etag-yaml"),
                  entry("logo.png", "file", 1048576, 0o100644, "etag-png"),
                ],
          ),
        );
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    const search = page.locator(".fm-search input");
    await search.fill("conf");
    await expect(page.locator(".fm-foot")).toContainText("1 of 3 entries");

    await page.locator(".fm-label").filter({ hasText: "conf" }).dblclick();
    await expect(
      page.locator(".fm-table").getByText("inside.txt"),
    ).toBeVisible();
    await expect(search).toHaveValue("");
    await expect(page.locator(".fm-foot")).toContainText("1 entry");
  });

  test("a symlink row shows its target and Go to target follows it", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => {
        const path = new URL(route.request().url()).searchParams.get("path") ?? "";
        return json(
          route,
          200,
          listing(
            path === "usr/bin"
              ? [entry("python3", "file", 9, 0o100755, "etag-py")]
              : [
                  entry(
                    "bin",
                    "symlink",
                    7,
                    0o120777,
                    "etag-bin",
                    "usr/bin",
                  ),
                ],
          ),
        );
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(
      page.locator(".fm-table").getByText("→ usr/bin"),
    ).toBeVisible();

    await page.getByRole("button", { name: "More actions for bin" }).click();
    await page.getByRole("menuitem", { name: "Go to target" }).click();
    await expect(
      page.locator(".fm-table").getByText("python3", { exact: true }),
    ).toBeVisible();
  });
});

/**
 * JUS-132: a symlink whose target is a file or a broken link is offered the
 * action, but a failed open keeps the current listing and warns instead of
 * replacing the view with an error state. The deliberate 400 makes the browser
 * log the failed resource, so this scenario whitelists that console line.
 */
test.describe("files symlink target failure", () => {
  test.use({ expectedConsoleErrors: ["Failed to load resource"] });

  test("a non-directory target keeps the view and warns", async ({
    page,
    request,
  }) => {
    const server = await seedServer(request, uniqueSuffix());
    await routeFiles(page, {
      roots: (route) => json(route, 200, { roots: [writableRoot] }),
      list: (route) => {
        const path = new URL(route.request().url()).searchParams.get("path") ?? "";
        if (path === "usr/bin") {
          return json(route, 400, { message: "not a directory" });
        }
        return json(
          route,
          200,
          listing([entry("bin", "symlink", 7, 0o120777, "etag-bin", "usr/bin")]),
        );
      },
    });

    await page.goto(`/servers/${server.id}/files`);
    await expect(
      page.locator(".fm-table").getByText("→ usr/bin"),
    ).toBeVisible();

    await page.getByRole("button", { name: "More actions for bin" }).click();
    await page.getByRole("menuitem", { name: "Go to target" }).click();

    await expect(page.locator(".n-message")).toContainText(
      "Could not open the target of bin",
    );
    // The listing is unchanged: no error/empty state replaced it.
    await expect(
      page.locator(".fm-table").getByText("→ usr/bin"),
    ).toBeVisible();
    await expect(page.locator(".fm-state")).toHaveCount(0);
  });
});
