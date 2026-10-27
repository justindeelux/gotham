import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, window:readonly, URL */

const testsDir = dirname(fileURLToPath(import.meta.url));
const webdistRoot = resolve(testsDir, "../../internal/server/webdist");

const mimeTypes: Record<string, string> = {
  ".css": "text/css",
  ".html": "text/html",
  ".ico": "image/x-icon",
  ".js": "text/javascript",
  ".json": "application/json",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".woff2": "font/woff2",
};

const session = {
  user: {
    id: "u-1",
    email: "ada@gotham.dev",
    created_at: "2026-03-04T12:00:00Z",
    display_name: "Ada",
    has_password: true,
    role: "owner",
  },
  accessToken: "test-access",
  refreshToken: "test-refresh",
};

const server1 = "11111111-1111-4111-8111-111111111111";
const server2 = "22222222-2222-4222-8222-222222222222";

function serverRow(id: string, name: string, ip: string) {
  return {
    id,
    name,
    ip,
    status: "ready",
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

const rows = [
  serverRow(server1, "prod-01-with-a-very-long-node-name", "10.0.0.1"),
  serverRow(server2, "prod-02", "10.0.0.2"),
];

const roots = [
  {
    name: "volumes",
    path: "/var/lib/gotham/volumes",
    writable: true,
    free_bytes: 21 * 1024 ** 3,
  },
  {
    name: "data",
    path: "/var/lib/gotham/data",
    writable: true,
    free_bytes: 21 * 1024 ** 3,
  },
];

function listingFor(path: string) {
  const target = {
    name: "target",
    kind: "directory",
    size: 4096,
    mode: 0o040755,
    modified_at: "2026-10-12T09:14:00Z",
    etag: '"target"',
  };
  if (path === "target") {
    return [
      {
        name: "inside.txt",
        kind: "file",
        size: 12,
        mode: 0o100644,
        modified_at: "2026-10-12T09:02:00Z",
        etag: '"inside"',
      },
    ];
  }
  return [
    {
      name: "report.txt",
      kind: "file",
      size: 2458,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"report"',
    },
    target,
    {
      name: "a-very-long-file-name-that-keeps-going-and-going-to-test-truncation-in-a-narrow-pane.txt",
      kind: "file",
      size: 1,
      mode: 0o100644,
      modified_at: "2026-10-12T09:02:00Z",
      etag: '"long"',
    },
  ];
}

interface Harness {
  listCalls: { serverId: string; root: string; path: string }[];
  copyCalls: {
    url: string;
    root: string;
    source: string;
    destination: string;
  }[];
}

let server: Server;
let baseURL = "";
let harness: Harness = { listCalls: [], copyCalls: [] };

test.beforeAll(async () => {
  server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url ?? "/", "http://127.0.0.1");
      let path = decodeURIComponent(url.pathname);
      if (path === "/" || !extname(path)) {
        path = "/index.html";
      }
      const file = resolve(webdistRoot, `.${path}`);
      if (!file.startsWith(webdistRoot)) {
        response.writeHead(403);
        response.end();
        return;
      }
      const body = await readFile(file);
      response.writeHead(200, {
        "content-type": mimeTypes[extname(file)] ?? "application/octet-stream",
      });
      response.end(body);
    } catch {
      response.writeHead(404);
      response.end();
    }
  });
  await new Promise<void>((done) => {
    server.listen(0, "127.0.0.1", () => done());
  });
  const address = server.address() as AddressInfo;
  baseURL = `http://127.0.0.1:${address.port}`;
});

test.afterAll(async () => {
  await new Promise<void>((done) => {
    server.close(() => done());
  });
});

async function mockApi(page: Page): Promise<Harness> {
  harness = { listCalls: [], copyCalls: [] };
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: { user: session.user } });
  });
  await page.route("**/api/v1/version", async (route) => {
    await route.fulfill({ json: { version: "v0.2.1-dev" } });
  });
  await page.route("**/api/v1/servers", async (route) => {
    await route.fulfill({ json: { servers: rows } });
  });
  for (const row of rows) {
    await page.route(`**/api/v1/servers/${row.id}`, async (route) => {
      await route.fulfill({ json: row });
    });
    await page.route(`**/api/v1/servers/${row.id}/files/roots`, async (route) => {
      await route.fulfill({ json: { roots } });
    });
    await page.route(`**/api/v1/servers/${row.id}/files/list*`, async (route) => {
      const url = new URL(route.request().url());
      const root = url.searchParams.get("root") ?? "";
      const path = url.searchParams.get("path") ?? "";
      harness.listCalls.push({ serverId: row.id, root, path });
      await route.fulfill({
        json: { entries: listingFor(path), truncated: false, cursor: "" },
      });
    });
    await page.route(`**/api/v1/servers/${row.id}/files/copy`, async (route) => {
      const body = route.request().postDataJSON() as {
        root: string;
        source: string;
        destination: string;
      };
      harness.copyCalls.push({ url: route.request().url(), ...body });
      await route.fulfill({
        json: {
          name: body.destination.split("/").pop() ?? "",
          kind: "file",
          size: 2458,
          mode: 0o100644,
          modified_at: "2026-10-12T09:02:00Z",
          etag: '"copy"',
        },
      });
    });
  }
  await page.route("**/api/v1/servers/*/files/content*", async (route) => {
    await route.fulfill({
      json: {
        content: "hello\n",
        etag: '"v1"',
        sha256: "abc",
        size: 6,
        mode: 0o100644,
        encoding: "utf-8",
      },
    });
  });
  return harness;
}

function pane(page: Page, side: "left" | "right") {
  return page.locator(`.files-browser[data-pane="${side}"]`);
}

/** Focuses a row so the pane's focusin handler activates it without opening a row. */
async function focusPane(page: Page, side: "left" | "right"): Promise<void> {
  await pane(page, side).locator(".fm-label").first().focus();
}

async function openFiles(page: Page): Promise<void> {
  await page.setViewportSize({ width: 1440, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label").first()).toBeVisible();
}

async function enableSplit(page: Page): Promise<void> {
  await page.getByRole("button", { name: "Split view" }).click();
  await expect(pane(page, "right")).toBeVisible();
}

test("split duplicates the location and keeps node, root and scroll per pane", async ({
  page,
}) => {
  await openFiles(page);
  await enableSplit(page);

  await expect(pane(page, "right").locator(".fm-label").first()).toBeVisible();
  await expect(pane(page, "left").locator(".fm-node__select")).toContainText("prod-01");
  await expect(pane(page, "right").locator(".fm-node__select")).toContainText("prod-01");
  expect(new URL(page.url()).pathname).toContain(server1);

  await pane(page, "right").locator(".fm-node__select").click();
  await page.locator(".n-base-select-option", { hasText: "prod-02" }).click();
  await expect(pane(page, "right").locator(".fm-node__select")).toContainText("prod-02");
  await expect(pane(page, "left").locator(".fm-node__select")).toContainText("prod-01");
  expect(new URL(page.url()).pathname).toContain(server1);

  await pane(page, "right").locator(".fm-root-select").click();
  await page.locator(".n-base-select-option", { hasText: "data" }).click();
  await expect(pane(page, "right").locator(".fm-root-select")).toContainText("data");

  await pane(page, "left").locator('.fm-row[data-entry="target"] .fm-label').dblclick();
  await expect(
    pane(page, "left").locator(".fm-crumb", { hasText: "target" }),
  ).toBeVisible();
  await expect(
    pane(page, "right").locator(".fm-crumb", { hasText: "data" }),
  ).toBeVisible();
});

test("copy and paste between panes reaches the real API destination", async ({
  page,
}) => {
  await openFiles(page);
  await enableSplit(page);

  const report = pane(page, "left").locator('.fm-row[data-entry="report.txt"]');
  await report.locator(".fm-label").click();
  await report.getByRole("button", { name: "More actions for report.txt" }).click();
  await page.getByRole("menuitem", { name: "Copy", exact: true }).click();

  await pane(page, "right")
    .locator('.fm-row[data-entry="target"] .fm-label')
    .dblclick();
  await expect(
    pane(page, "right").locator(".fm-crumb", { hasText: "target" }),
  ).toBeVisible();

  await pane(page, "right")
    .getByRole("button", { name: "File operations" })
    .click();
  await page.getByRole("menuitem", { name: /^Paste/ }).click();

  await expect.poll(() => harness.copyCalls.length).toBe(1);
  expect(harness.copyCalls[0]).toMatchObject({
    root: "volumes",
    source: "report.txt",
    destination: "target/report.txt",
  });
  expect(harness.copyCalls[0]!.url).toContain(`/servers/${server1}/files/copy`);
});

test("a same-node cross-root paste stays disabled with a reason", async ({
  page,
}) => {
  await openFiles(page);
  await enableSplit(page);

  const report = pane(page, "left").locator('.fm-row[data-entry="report.txt"]');
  await report.locator(".fm-label").click();
  await report.getByRole("button", { name: "More actions for report.txt" }).click();
  await page.getByRole("menuitem", { name: "Copy", exact: true }).click();

  await pane(page, "right").locator(".fm-root-select").click();
  await page.locator(".n-base-select-option", { hasText: "data" }).click();
  await expect(pane(page, "right").locator(".fm-root-select")).toContainText("data");

  await pane(page, "right")
    .getByRole("button", { name: "File operations" })
    .click();
  const paste = page.getByRole("menuitem", { name: /^Paste/ });
  await expect(paste).toHaveClass(/n-dropdown-option-body--disabled/);
  await expect(paste).toContainText("another root");
  expect(harness.copyCalls).toHaveLength(0);
});

test("narrow widths show one pane with an explicit switch and keep both contexts", async ({
  page,
}) => {
  await openFiles(page);
  await enableSplit(page);

  for (const width of [390, 480]) {
    await page.setViewportSize({ width, height: 844 });
    await expect(page.getByRole("button", { name: "Show left pane" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Show right pane" })).toBeVisible();
    await expect(pane(page, "left")).toBeVisible();
    await expect(pane(page, "right")).toBeHidden();

    await page.getByRole("button", { name: "Show right pane" }).click();
    await expect(pane(page, "right")).toBeVisible();
    await expect(pane(page, "left")).toBeHidden();
    await expect(
      pane(page, "right").locator(".fm-label").first(),
    ).toBeVisible();

    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    );
    expect(overflow, `no horizontal overflow at ${width}px`).toBeLessThanOrEqual(1);

    await page.getByRole("button", { name: "Show left pane" }).click();
    await expect(pane(page, "left")).toBeVisible();
  }
});

test("desktop split resizes from the keyboard and survives a refresh", async ({
  page,
}) => {
  await openFiles(page);
  await enableSplit(page);

  for (const width of [1280, 1366]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.getByRole("separator", { name: "Resize panes" })).toBeVisible();
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - window.innerWidth,
    );
    expect(overflow, `no horizontal overflow at ${width}px`).toBeLessThanOrEqual(1);
  }

  const before = (await pane(page, "left").boundingBox())!.width;
  const separator = page.getByRole("separator", { name: "Resize panes" });
  await separator.focus();
  await page.keyboard.press("ArrowRight");
  await expect
    .poll(async () => (await pane(page, "left").boundingBox())!.width)
    .toBeGreaterThan(before);
  await expect(separator).toHaveAttribute("aria-valuenow", /5[1-9]|6\d|7[0-5]/);

  await pane(page, "right").locator(".fm-node__select").click();
  await page.locator(".n-base-select-option", { hasText: "prod-02" }).click();
  await expect(pane(page, "right").locator(".fm-node__select")).toContainText("prod-02");

  await page.reload();
  await expect(page.locator(".files-browser")).toHaveCount(2);
  await expect(pane(page, "right").locator(".fm-node__select")).toContainText("prod-02");
  expect(new URL(page.url()).pathname).toContain(server1);
});

test("the active pane is marked and double-click opens a directory only there", async ({
  page,
}) => {
  await openFiles(page);
  await enableSplit(page);

  await pane(page, "right").locator('.fm-row[data-entry="target"] .fm-label').click();
  await expect(pane(page, "right")).toHaveClass(/is-active-pane/);
  await expect(pane(page, "left")).not.toHaveClass(/is-active-pane/);
  await expect(pane(page, "right")).toHaveAttribute("aria-current", "true");
  await expect(pane(page, "left")).not.toHaveAttribute("aria-current", "true");
  await expect(page.locator('.files-browser[aria-current="true"]')).toHaveCount(1);
  await expect(page.locator(".fm-active-tag")).toHaveCount(1);
  await expect(pane(page, "right").locator(".fm-active-tag")).toBeVisible();

  await pane(page, "left").locator(".fm-foot").click();
  await expect(pane(page, "left")).toHaveAttribute("aria-current", "true");
  await expect(pane(page, "right")).not.toHaveAttribute("aria-current", "true");
  await expect(page.locator('.files-browser[aria-current="true"]')).toHaveCount(1);
  await expect(pane(page, "left").locator(".fm-active-tag")).toBeVisible();

  const calls = harness.listCalls.length;
  await pane(page, "right")
    .locator('.fm-row[data-entry="target"] .fm-label')
    .dblclick();
  await expect(
    pane(page, "right").locator(".fm-crumb", { hasText: "target" }),
  ).toBeVisible();
  await expect(
    pane(page, "left").locator(".fm-crumb", { hasText: "target" }),
  ).toHaveCount(0);
  await expect
    .poll(
      () =>
        harness.listCalls.filter(
          (call) => call.serverId === server1 && call.path === "target",
        ).length,
    )
    .toBe(1);
  expect(harness.listCalls.length).toBeGreaterThanOrEqual(calls);
});

test.describe("rail editor button across panes", () => {
  async function openInPane(page: Page, side: "left" | "right"): Promise<void> {
    await pane(page, side)
      .locator('.fm-row[data-entry="report.txt"] .fm-label')
      .dblclick();
    await expect(page.locator(".ed-window")).toBeVisible();
    await expect(page.locator(".cm-content").first()).toBeVisible();
  }

  for (const dirty of [false, true]) {
    test(`shows the inactive pane's ${dirty ? "dirty" : "clean"} editor`, async ({
      page,
    }) => {
      await openFiles(page);
      await enableSplit(page);
      await openInPane(page, "left");

      if (dirty) {
        await page.locator(".cm-content").first().click();
        await page.keyboard.type(" edited");
        await expect(page.locator(".ed-window-dirty")).toBeVisible();
      }

      await focusPane(page, "right");
      await expect(pane(page, "right")).toHaveAttribute("aria-current", "true");
      await expect(page.locator(".ed-window")).toBeHidden();

      await page.locator(".rail-btn--editor").click();

      await expect(pane(page, "left")).toHaveAttribute("aria-current", "true");
      await expect(pane(page, "right")).not.toHaveAttribute("aria-current", "true");
      await expect(page.locator(".ed-window")).toBeVisible();
      await expect(page.locator(".ed-window .ed-tab-name").first()).toBeVisible();
      if (dirty) {
        await expect(page.locator(".ed-window-dirty")).toBeVisible();
      }
    });
  }

  test("activates the right pane when it holds the only editor", async ({
    page,
  }) => {
    await openFiles(page);
    await enableSplit(page);
    await openInPane(page, "right");

    await focusPane(page, "left");
    await expect(pane(page, "left")).toHaveAttribute("aria-current", "true");
    await expect(page.locator(".ed-window")).toBeHidden();

    await page.locator(".rail-btn--editor").click();

    await expect(pane(page, "right")).toHaveAttribute("aria-current", "true");
    await expect(pane(page, "left")).not.toHaveAttribute("aria-current", "true");
    await expect(page.locator(".ed-window")).toBeVisible();
    await expect(page.locator(".ed-window .ed-tab-name").first()).toBeVisible();
  });
});
