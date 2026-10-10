import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

// Reuse the authenticated session so the page starts signed in.
test.use({ storageState: storageStatePath });

const testsDir = dirname(fileURLToPath(import.meta.url));
const shot = (name: string) =>
  resolve(testsDir, "../../.playwright-mcp", name);

/**
 * JUS-90 fix 4: every multi-line cell of the router list stacks its lines
 * with the shared 4px step (var(--space-1)), left-aligned, with no overlap.
 * Real Chromium measures the real table against intercepted router rows
 * covering all three kinds (application with TLS, plain-HTTP service,
 * redirect) and two node sync states. Screenshots land in .playwright-mcp/.
 */
test.describe("routers layout", () => {
  test.beforeEach(async ({ page }) => {
    await page.route("**/api/v1/proxy/routers*", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          routers: [
            {
              host: "shop.example.com",
              rule: "Host(`shop.example.com`)",
              service: "app-shop",
              target: "http://172.17.0.1:8080",
              entrypoints: ["web", "websecure"],
              middlewares: ["gotham-https-redirect"],
              tls_resolver: "letsencrypt",
              kind: "application",
              owner_id: "11111111-1111-1111-1111-111111111111",
              owner_name: "shop",
              server_id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
              server_name: "prod-01",
            },
            {
              host: "metrics.example.com",
              rule: "Host(`metrics.example.com`)",
              service: "svc-metrics-0",
              target: "http://172.17.0.1:9090",
              entrypoints: ["web"],
              kind: "service",
              owner_id: "22222222-2222-2222-2222-222222222222",
              owner_name: "observability",
              server_id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
              server_name: "prod-01",
            },
            {
              host: "old.example.com",
              rule: "Host(`old.example.com`)",
              service: "gotham-redirect-noop",
              target: "https://shop.example.com",
              entrypoints: ["web"],
              middlewares: ["gotham-redirect-rule"],
              kind: "redirect",
              owner_id: "33333333-3333-3333-3333-333333333333",
              owner_name: "shop",
              server_id: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
              server_name: "edge-01",
            },
          ],
          nodes: [
            {
              server_id: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
              server_name: "prod-01",
              sync_status: "synced",
            },
            {
              server_id: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
              server_name: "edge-01",
              sync_status: "pending",
            },
          ],
        }),
      });
    });
    await page.goto("/domains");
    await page
      .locator(".n-tabs-tab")
      .filter({ hasText: "Routers" })
      .click();
    await expect(page.locator(".router-table tbody tr")).toHaveCount(3);
  });

  test("stacks every multi-line cell with a 4px gap and no overlap", async ({
    page,
  }) => {
    const cells = await page
      .locator(".router-table .cell-main")
      .evaluateAll((elements) =>
        elements.map((element) =>
          [...element.children].map((child) => {
            const rect = child.getBoundingClientRect();
            return { top: rect.top, bottom: rect.bottom, left: rect.left };
          }),
        ),
      );
    const multiLine = cells.filter((kids) => kids.length >= 2);
    // Three rows of host/source/node cells all render two lines.
    expect(multiLine.length).toBe(9);
    for (const kids of multiLine) {
      for (let i = 1; i < kids.length; i++) {
        expect(
          kids[i].top - kids[i - 1].bottom,
          `cell line ${i} overlaps the previous line`,
        ).toBeGreaterThanOrEqual(3);
        expect(
          Math.abs(kids[i].left - kids[0].left),
          "cell lines share the same left edge",
        ).toBeLessThanOrEqual(1);
      }
    }
    await page.setViewportSize({ width: 1280, height: 900 });
    await page
      .locator(".router-table")
      .screenshot({ path: shot("routers-1280.png") });
  });

  test("keeps the stack at a 480px viewport", async ({ page }) => {
    await page.setViewportSize({ width: 480, height: 900 });
    const cells = await page
      .locator(".router-table .cell-main")
      .evaluateAll((elements) =>
        elements.map((element) =>
          [...element.children].map((child) => {
            const rect = child.getBoundingClientRect();
            return { top: rect.top, bottom: rect.bottom, left: rect.left };
          }),
        ),
      );
    const multiLine = cells.filter((kids) => kids.length >= 2);
    expect(multiLine.length).toBe(9);
    for (const kids of multiLine) {
      for (let i = 1; i < kids.length; i++) {
        expect(
          kids[i].top - kids[i - 1].bottom,
          `cell line ${i} overlaps the previous line at 480px`,
        ).toBeGreaterThanOrEqual(3);
      }
    }
    await page
      .locator(".router-table")
      .screenshot({ path: shot("routers-480.png") });
  });
});
