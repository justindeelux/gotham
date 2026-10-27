import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page, Route } from "@playwright/test";

/* global Event, document:readonly, setTimeout, window:readonly, URL */

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

const rows = [serverRow(server1, "prod-01", "10.0.0.1"), serverRow(server2, "prod-02", "10.0.0.2")];

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
    free_bytes: 5 * 1024 ** 3,
  },
];

function entry(name: string, kind: "file" | "directory" = "file") {
  return {
    name,
    kind,
    size: kind === "file" ? 2458 : 4096,
    mode: kind === "file" ? 0o100644 : 0o040755,
    modified_at: "2026-10-12T09:02:00Z",
    etag: `"${name}"`,
  };
}

function listingFor(path: string) {
  if (path === "target") {
    return [entry("inside.txt")];
  }
  return [entry("report.txt"), entry("target", "directory")];
}

const basePlan = {
  mode: "bundle",
  bounds: {
    max_bundle_bytes: 1073741824,
    max_expanded_bytes: 4294967296,
    max_entries: 100000,
    max_sources: 1024,
    max_concurrency: 4,
    bundle_available: true,
  },
  sources: [{ name: "report.txt", kind: "file", size: 2458, conflict: false }],
  conflicts: [],
  unsupported: [],
  available_bytes: 5 * 1024 ** 3,
};

function summaryBody(overrides: Record<string, unknown> = {}): string {
  const summary = {
    transfer_id: "transfer-1",
    mode: "bundle",
    status: "copied",
    copied: 1,
    conflicts: 0,
    skipped: 0,
    failed: 0,
    cancelled: 0,
    uncertain: 0,
    entries: [{ name: "report.txt", status: "copied" }],
    skipped_count: 0,
    ...overrides,
  };
  return [
    JSON.stringify({
      type: "phase",
      phase: "transferring",
      bytes: 1024,
      total_bytes: 2458,
    }),
    JSON.stringify({ type: "phase", phase: "publishing", bytes: 2458, total_bytes: 2458 }),
    JSON.stringify({ type: "summary", status: summary.status, summary }),
  ].join("\n").concat("\n");
}

interface PlanCall {
  serverId: string;
  body: Record<string, unknown>;
}

interface TransferCall {
  serverId: string;
  body: Record<string, unknown>;
}

interface Harness {
  plans: PlanCall[];
  transfers: TransferCall[];
  planResponse: () => { status: number; body: unknown };
  transferResponse: () => { status: number; body: string; delayMs: number };
}

let server: Server;
let baseURL = "";
let harness: Harness;

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

async function json(route: Route, status: number, body: unknown): Promise<void> {
  await route.fulfill({ status, json: body });
}

async function mockApi(page: Page): Promise<Harness> {
  harness = {
    plans: [],
    transfers: [],
    planResponse: () => ({ status: 200, body: basePlan }),
    transferResponse: () => ({ status: 200, body: summaryBody(), delayMs: 0 }),
  };
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  await page.route("**/api/v1/**", async (route) => {
    await json(route, 200, {});
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await json(route, 200, { user: session.user });
  });
  await page.route("**/api/v1/version", async (route) => {
    await json(route, 200, { version: "v0.2.1-dev" });
  });
  await page.route("**/api/v1/servers", async (route) => {
    await json(route, 200, { servers: rows });
  });
  for (const row of rows) {
    await page.route(`**/api/v1/servers/${row.id}`, async (route) => {
      await json(route, 200, row);
    });
    await page.route(`**/api/v1/servers/${row.id}/files/roots`, async (route) => {
      await json(route, 200, { roots });
    });
    await page.route(`**/api/v1/servers/${row.id}/files/list*`, async (route) => {
      const url = new URL(route.request().url());
      const path = url.searchParams.get("path") ?? "";
      await json(route, 200, {
        entries: listingFor(path),
        truncated: false,
        cursor: "",
      });
    });
    await page.route(
      `**/api/v1/servers/${row.id}/files/transfers/plan`,
      async (route) => {
        const body = route.request().postDataJSON() as Record<string, unknown>;
        harness.plans.push({ serverId: row.id, body });
        const response = harness.planResponse();
        await json(route, response.status, response.body);
      },
    );
    await page.route(
      `**/api/v1/servers/${row.id}/files/transfers`,
      async (route) => {
        const body = route.request().postDataJSON() as Record<string, unknown>;
        harness.transfers.push({ serverId: row.id, body });
        const response = harness.transferResponse();
        if (response.delayMs > 0) {
          try {
            await new Promise((resolve) =>
              setTimeout(resolve, response.delayMs),
            );
            await route.fulfill({
              status: response.status,
              headers: { "content-type": "application/x-ndjson" },
              body: response.body,
            });
          } catch {
            // The client cancelled and closed the request.
          }
          return;
        }
        await route.fulfill({
          status: response.status,
          headers: { "content-type": "application/x-ndjson" },
          body: response.body,
        });
      },
    );
  }
  return harness;
}

function pane(page: Page, side: "left" | "right") {
  return page.locator(`.files-browser[data-pane="${side}"]`);
}

async function openFiles(page: Page, width = 1440): Promise<void> {
  await page.setViewportSize({ width, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/servers/${server1}/files`);
  await expect(page.locator(".fm-label").first()).toBeVisible();
}

async function enableSplit(page: Page): Promise<void> {
  await page.getByRole("button", { name: "Split view" }).click();
  await expect(pane(page, "right")).toBeVisible();
}

async function openNodeCopy(page: Page): Promise<void> {
  await pane(page, "left").locator('.fm-row[data-entry="report.txt"]').click();
  await pane(page, "left")
    .getByRole("button", { name: "File operations", exact: true })
    .click();
  await page.getByRole("menuitem", { name: "Copy to node…" }).click();
  await expect(page.locator(".transfer-modal")).toBeVisible();
}

async function checkNodeCopy(page: Page): Promise<void> {
  const dialog = page.locator(".transfer-modal");
  await dialog.getByRole("button", { name: "Check destination" }).click();
  await expect(dialog.getByRole("button", { name: "Start copy" })).toBeEnabled();
}

test("single pane copies a selection to the chosen node directory", async ({ page }) => {
  await openFiles(page);
  await openNodeCopy(page);
  const dialog = page.locator(".transfer-modal");
  await expect(dialog.getByTestId("transfer-source")).toContainText("report.txt");

  await dialog.locator(".transfer-root-select").click();
  await page.locator(".n-base-select-option", { hasText: "volumes" }).click();
  await dialog.locator(".transfer-field--path input").fill("backups");
  await dialog.getByRole("button", { name: "Check destination" }).click();

  await expect(dialog.getByText("Archive transfer")).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Start copy" })).toBeEnabled();
  await dialog.getByRole("button", { name: "Start copy" }).click();

  await expect(dialog).toContainText("Copy finished");
  await expect(dialog).toContainText("1 copied");
  await expect(dialog.locator(".transfer-list__item[data-status='copied']")).toHaveCount(1);

  expect(harness.plans).toHaveLength(1);
  expect(harness.plans[0]?.serverId).toBe(server1);
  expect(harness.plans[0]?.body).toMatchObject({
    source_root: "volumes",
    source_directory: "",
    sources: ["report.txt"],
    destination: {
      server_id: server2,
      root: "volumes",
      path: "backups",
    },
  });
  expect(harness.transfers).toHaveLength(1);
  expect(harness.transfers[0]?.body).toEqual(harness.plans[0]?.body);
});

test("copy to the other pane captures the destination before it moves", async ({ page }) => {
  await openFiles(page);
  await enableSplit(page);
  await pane(page, "right").locator(".fm-node__select").click();
  await page.locator(".n-base-select-option", { hasText: "prod-02" }).click();
  await expect(pane(page, "right").locator(".fm-node__select")).toContainText("prod-02");
  await pane(page, "right").locator(".fm-root-select").click();
  await page.locator(".n-base-select-option", { hasText: "data" }).click();
  await expect(pane(page, "right").locator(".fm-root-select")).toContainText("data");

  await pane(page, "left").locator('.fm-row[data-entry="report.txt"]').click();
  await pane(page, "left")
    .locator('button[data-action="copy-to-pane"]')
    .click();

  const dialog = page.locator(".transfer-modal");
  await expect(dialog.getByTestId("transfer-destination")).toContainText("prod-02");
  await expect(dialog.getByTestId("transfer-destination")).toContainText("data");
  await expect(dialog.getByTestId("transfer-source")).toContainText("report.txt");
  await expect(dialog.getByRole("button", { name: "Start copy" })).toBeEnabled();

  harness.transferResponse = () => ({
    status: 200,
    body: summaryBody(),
    delayMs: 1500,
  });
  await dialog.getByRole("button", { name: "Start copy" }).click();
  await dialog.getByRole("button", { name: "Hide" }).click();

  // Moving the destination pane during the transfer must not retarget it.
  await pane(page, "right").locator(".fm-root-select").click();
  await page.locator(".n-base-select-option", { hasText: "volumes" }).click();
  await expect(pane(page, "right").locator(".fm-root-select")).toContainText("volumes");

  await page
    .locator(".server-files__transfer")
    .getByRole("button", { name: "Destination check" })
    .click();
  await expect(dialog.getByTestId("transfer-destination")).toContainText("prod-02");
  await expect(dialog.getByTestId("transfer-destination")).toContainText("data");
  await expect(dialog).toContainText("Copy finished");

  expect(harness.plans[0]?.body).toMatchObject({
    destination: { server_id: server2, root: "data", path: "" },
  });
  expect(harness.transfers[0]?.body).toEqual(harness.plans[0]?.body);
});

test("a plan hard failure over the single-file limit is human readable", async ({ page }) => {
  await openFiles(page);
  harness.planResponse = () => ({
    status: 413,
    body: { message: "file is too large to edit", code: "too_large" },
  });
  await openNodeCopy(page);

  const dialog = page.locator(".transfer-modal");
  await dialog.getByRole("button", { name: "Check destination" }).click();
  await expect(dialog).toContainText("report.txt");
  await expect(dialog).toContainText("single-file copy limit");
  await expect(dialog).not.toContainText("Unexpected request error");
  await expect(dialog.getByRole("button", { name: "Start copy" })).toHaveCount(0);
});

test("a summary with a failure is partial and offers a bounded retry", async ({ page }) => {
  await openFiles(page);
  harness.transferResponse = () => ({
    status: 200,
    body: summaryBody({
      status: "partial",
      copied: 0,
      failed: 1,
      entries: [{ name: "report.txt", status: "failed", reason: "busy" }],
    }),
    delayMs: 0,
  });
  await openNodeCopy(page);
  await checkNodeCopy(page);

  const dialog = page.locator(".transfer-modal");
  await dialog.getByRole("button", { name: "Start copy" }).click();
  await expect(dialog).toContainText("Copy finished with issues");
  await expect(dialog.locator(".transfer-list__item[data-status='failed']")).toHaveCount(1);

  await dialog.getByRole("button", { name: "Retry selected" }).click();
  await expect.poll(() => harness.plans.length).toBe(2);
  await expect(dialog.getByRole("button", { name: "Start copy" })).toBeEnabled();
  expect(harness.plans[1]?.body).toMatchObject({ sources: ["report.txt"] });
});

test("a stream without a terminal summary is interrupted, never success", async ({ page }) => {
  await openFiles(page);
  harness.transferResponse = () => ({
    status: 200,
    body: `${JSON.stringify({
      type: "phase",
      phase: "publishing",
      bytes: 2048,
      total_bytes: 2458,
    })}\n`,
    delayMs: 0,
  });
  await openNodeCopy(page);
  await checkNodeCopy(page);

  const dialog = page.locator(".transfer-modal");
  await dialog.getByRole("button", { name: "Start copy" }).click();
  await expect(dialog).toContainText("Copy interrupted");
  await expect(dialog).toContainText("Uncertain");
  await expect(dialog).not.toContainText("Copy finished");
});

test("cancelling reports cancelled work and never a success toast", async ({ page }) => {
  await openFiles(page);
  harness.transferResponse = () => ({
    status: 200,
    body: summaryBody(),
    delayMs: 5000,
  });
  await openNodeCopy(page);
  await checkNodeCopy(page);

  const dialog = page.locator(".transfer-modal");
  await dialog.getByRole("button", { name: "Start copy" }).click();
  await expect(dialog.getByRole("button", { name: "Cancel copy" })).toBeVisible();
  await dialog.getByRole("button", { name: "Cancel copy" }).click();

  await expect(dialog).toContainText("Copy cancelled");
  await expect(dialog.locator(".transfer-list__item[data-status='uncertain']")).toHaveCount(1);
  await expect(dialog).not.toContainText("Copy finished");
});

test("a copy that only skipped nested entries is a warning, not success", async ({ page }) => {
  await openFiles(page);
  harness.transferResponse = () => ({
    status: 200,
    body: summaryBody({ status: "copied", copied: 1, skipped_count: 3 }),
    delayMs: 0,
  });
  await openNodeCopy(page);
  await checkNodeCopy(page);

  const dialog = page.locator(".transfer-modal");
  await dialog.getByRole("button", { name: "Start copy" }).click();
  await expect(dialog).toContainText("Copy finished with issues");
  await expect(dialog).toContainText("3 nested entries");
  await expect(
    page.locator(".server-files__transfer"),
  ).toHaveAttribute("data-tone", "warning");
});

test("results survive an interrupted stream and show acknowledged entries", async ({
  page,
}) => {
  await openFiles(page);
  harness.transferResponse = () => ({
    status: 200,
    body: `${JSON.stringify({
      type: "result",
      name: "report.txt",
      status: "copied",
    })}\n`,
    delayMs: 0,
  });
  await openNodeCopy(page);
  await checkNodeCopy(page);

  const dialog = page.locator(".transfer-modal");
  await dialog.getByRole("button", { name: "Start copy" }).click();

  await expect(dialog).toContainText("Copy interrupted");
  await expect(dialog.locator(".transfer-list__item[data-status='copied']")).toHaveCount(
    1,
  );
  await expect(dialog.locator(".transfer-list__item[data-status='uncertain']")).toHaveCount(
    0,
  );
  await expect(dialog).not.toContainText("Copy finished");
});

test("route leave asks to stay or cancel the transfer and leave", async ({ page }) => {
  await openFiles(page);
  harness.transferResponse = () => ({
    status: 200,
    body: summaryBody(),
    delayMs: 5000,
  });
  await openNodeCopy(page);
  await checkNodeCopy(page);
  await page.locator(".transfer-modal").getByRole("button", { name: "Start copy" }).click();
  await page.locator(".transfer-modal").getByRole("button", { name: "Hide" }).click();
  await expect(page.locator(".server-files__transfer")).toBeVisible();

  await page.getByRole("link", { name: "File manager" }).click();
  await expect(
    page.getByRole("heading", { name: "Cancel the running copy?", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Stay" })).toBeFocused();
  await page.getByRole("button", { name: "Stay" }).click();
  await expect(page).toHaveURL(new RegExp(`/servers/${server1}/files`));
  await expect(page.locator(".server-files__transfer")).toBeVisible();

  await page.getByRole("link", { name: "File manager" }).click();
  await page.getByRole("button", { name: "Cancel transfer and leave" }).click();
  await expect(page).toHaveURL(/\/files$/);
});

test("logout asks to stay or cancel the transfer and leave", async ({ page }) => {
  await openFiles(page);
  harness.transferResponse = () => ({
    status: 200,
    body: summaryBody(),
    delayMs: 5000,
  });
  await openNodeCopy(page);
  await checkNodeCopy(page);
  await page.locator(".transfer-modal").getByRole("button", { name: "Start copy" }).click();
  await page.locator(".transfer-modal").getByRole("button", { name: "Hide" }).click();

  await page.locator(".topbar").getByRole("button", { name: "Account" }).click();
  await page.locator(".n-dropdown-option", { hasText: "Sign out" }).click();
  await expect(
    page.getByRole("heading", { name: "Cancel the running copy?", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Stay" }).click();
  await expect(page).toHaveURL(new RegExp(`/servers/${server1}/files`));

  await page.locator(".topbar").getByRole("button", { name: "Account" }).click();
  await page.locator(".n-dropdown-option", { hasText: "Sign out" }).click();
  await page.getByRole("button", { name: "Cancel transfer and leave" }).click();
  await expect(page).toHaveURL(/\/login$/);
});

test("beforeunload warns only while a copy is running", async ({ page }) => {
  await openFiles(page);
  const idlePrevented = await page.evaluate(() => {
    const event = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  });
  expect(idlePrevented).toBe(false);

  harness.transferResponse = () => ({
    status: 200,
    body: summaryBody(),
    delayMs: 5000,
  });
  await openNodeCopy(page);
  await checkNodeCopy(page);
  await page.locator(".transfer-modal").getByRole("button", { name: "Start copy" }).click();

  const runningPrevented = await page.evaluate(() => {
    const event = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(event);
    return event.defaultPrevented;
  });
  expect(runningPrevented).toBe(true);
});

test("closing the copy dialog returns focus to the file operations trigger", async ({ page }) => {
  await openFiles(page);
  await openNodeCopy(page);
  await page.locator(".transfer-modal").getByRole("button", { name: "Cancel" }).click();
  const focus = await page.evaluate(() => ({
    label: document.activeElement?.getAttribute("aria-label") ?? "",
  }));
  expect(focus.label).toBe("File operations");
});

test("closing the leave dialog returns focus to the link that asked to leave", async ({
  page,
}) => {
  await openFiles(page);
  harness.transferResponse = () => ({
    status: 200,
    body: summaryBody(),
    delayMs: 5000,
  });
  await openNodeCopy(page);
  await checkNodeCopy(page);
  await page.locator(".transfer-modal").getByRole("button", { name: "Start copy" }).click();
  await page.locator(".transfer-modal").getByRole("button", { name: "Hide" }).click();

  await page.getByRole("link", { name: "File manager" }).click();
  await expect(page.getByRole("button", { name: "Stay" })).toBeFocused();
  await page.getByRole("button", { name: "Stay" }).click();
  const focus = await page.evaluate(() => ({
    text: document.activeElement?.textContent?.trim() ?? "",
  }));
  expect(focus.text).toContain("File manager");
});

for (const width of [390, 480, 1280, 1366]) {
  test(`the copy dialog fits a ${width}px screen`, async ({ page }) => {
    await openFiles(page, width);
    await openNodeCopy(page);
    const dialog = page.locator(".transfer-modal");
    const box = await dialog.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeLessThanOrEqual(width);
    await expect(
      dialog.getByRole("button", { name: "Check destination" }),
    ).toBeVisible();
    await expect(dialog.locator(".transfer-field--path input")).toBeVisible();
  });
}
