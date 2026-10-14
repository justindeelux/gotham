import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, URL, window:readonly */
// JUS-135 shell proof: below the tablet breakpoint the server rail leaves the
// grid (it becomes a drawer), so the content column starts at x=0 on every
// page — no blank 60/72px gutter and no overlap — and the rail drawer closes
// on a tap even when the tapped link's route does not change.

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
    is_platform_admin: true,
  },
  accessToken: "test-access",
  refreshToken: "test-refresh",
};

const team = {
  id: "team-1",
  name: "Acme",
  is_personal: false,
  role: "owner",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

const serverId = "33333333-3333-4333-8333-333333333333";
const serverRow = {
  id: serverId,
  name: "prod-01",
  ip: "10.0.0.1",
  status: "ready",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

// Every shell surface that has its own page: the drawer/gutter regression must
// not depend on which page is open.
const shellRoutes = ["/dashboard", "/servers", "/projects", "/settings/profile"];

let server: Server;
let baseURL = "";

test.beforeAll(async () => {
  server = createServer(async (request, response) => {
    try {
      const url = new URL(request.url ?? "/", "http://127.0.0.1");
      let path = decodeURIComponent(url.pathname);
      if (path === "/" || !extname(path)) {
        // SPA fallback: client-side routes serve index.html.
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
  baseURL = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});

test.afterAll(async () => {
  await new Promise<void>((done) => {
    server.close(() => done());
  });
});

/** mockApi seeds the session and answers enough for the shell to render. */
async function mockApi(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  // Catch-all first so the specific routes registered after it win.
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: session.user });
  });
  await page.route("**/api/v1/teams", async (route) => {
    await route.fulfill({ json: { teams: [team] } });
  });
  await page.route("**/api/v1/version", async (route) => {
    await route.fulfill({ json: { version: "v0.2.1-dev" } });
  });
  await page.route("**/api/v1/servers", async (route) => {
    await route.fulfill({ json: { servers: [serverRow] } });
  });
  await page.route("**/api/v1/projects", async (route) => {
    await route.fulfill({ json: { projects: [] } });
  });
}

interface ShellMetrics {
  viewLeft: number | null;
  viewWidth: number | null;
  railInGrid: boolean;
  overflowX: number;
  toggleHit: boolean;
}

/** shellMetrics reads the content column's geometry and the topbar hit-test. */
async function shellMetrics(page: Page): Promise<ShellMetrics> {
  return page.evaluate(() => {
    const view = document.querySelector("main.view") as HTMLElement | null;
    const rail = document.querySelector(".app > .rail") as HTMLElement | null;
    const toggle = document.querySelector(".rail-toggle") as HTMLElement | null;
    const doc = document.scrollingElement as HTMLElement;
    const box = view ? view.getBoundingClientRect() : null;
    let toggleHit = false;
    if (toggle) {
      const toggleBox = toggle.getBoundingClientRect();
      const target = document.elementFromPoint(
        toggleBox.x + toggleBox.width / 2,
        toggleBox.y + toggleBox.height / 2,
      );
      toggleHit = target !== null && (target === toggle || toggle.contains(target));
    }
    return {
      viewLeft: box ? box.left : null,
      viewWidth: box ? box.width : null,
      railInGrid: rail !== null,
      overflowX: doc.scrollWidth - window.innerWidth,
      toggleHit,
    };
  });
}

for (const width of [390, 768]) {
  test(`the shell fills the width with no rail gutter at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 844 });
    await mockApi(page);

    for (const route of shellRoutes) {
      await page.goto(`${baseURL}${route}`);
      await expect(page.locator("main.view")).toBeVisible();
      const metrics = await shellMetrics(page);

      expect(metrics.viewLeft, `${route}: content column has a box`).not.toBeNull();
      expect(
        Math.abs(metrics.viewLeft!),
        `${route}@${width}: content starts at the viewport's left edge (no rail gutter)`,
      ).toBeLessThanOrEqual(1);
      expect(
        Math.abs(metrics.viewWidth! - width),
        `${route}@${width}: content spans the viewport (no overlap/gap)`,
      ).toBeLessThanOrEqual(1);
      expect(
        metrics.railInGrid,
        `${route}@${width}: the rail is not an always-on grid column`,
      ).toBe(false);
      expect(
        metrics.overflowX,
        `${route}@${width}: no horizontal page overflow`,
      ).toBeLessThanOrEqual(1);
      expect(
        metrics.toggleHit,
        `${route}@${width}: the rail drawer trigger is hit-testable`,
      ).toBe(true);
    }
  });
}

test("the rail drawer closes on a tap even when the route does not change", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/dashboard`);

  const toggle = page.getByRole("button", { name: "Toggle servers" });
  await toggle.click();
  const drawer = page.locator(".n-drawer[role='dialog']");
  await expect(drawer).toBeVisible();

  // The rail's home link targets /dashboard — the current route — so only the
  // drawer's own click handler can close it (the route watcher never fires).
  await drawer.getByRole("link", { name: "Gotham — home", exact: true }).click();
  await expect(drawer).toBeHidden();
  await expect(toggle).toHaveAttribute("aria-expanded", "false");
  await expect(page).toHaveURL(/\/dashboard$/);
});
