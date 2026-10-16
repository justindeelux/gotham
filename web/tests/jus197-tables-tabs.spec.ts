import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, URL, window:readonly, getComputedStyle:readonly */
// JUS-197 overflow proof: the members table and the Domains tab strip must
// scroll inside their own containers at phone widths — never the page. The
// global `body { overflow-x: hidden }` mask would hide a break, so these
// specs prove the container-level scroller directly: from the wide inner
// element upwards, the first `overflow-x: auto|scroll` ancestor must itself
// scroll (scrollWidth > clientWidth). A page-level scrollWidth check rides
// along as a smoke assertion only.

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
    display_name: "Ada Lovelace",
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

// Long unbreakable values force the 760px table floor past the 390px
// viewport, so the test exercises the scroller instead of a fitting table.
const members = [
  {
    user_id: "u-1",
    email: "ada.lovelace-extremely-long-mailbox@very-long-domain-name.example.com",
    role: "owner",
    created_at: "2026-10-01T00:00:00Z",
  },
  {
    user_id: "u-2",
    email: "grace.hopper-also-quite-long-mailbox@another-very-long-domain.example.com",
    role: "read_only",
    created_at: "2026-10-02T00:00:00Z",
  },
];

const routers = [
  {
    host: "panel.control-plane-with-a-very-long-hostname.example.com",
    rule: "Host(`panel.control-plane-with-a-very-long-hostname.example.com`)",
    service: "gotham-panel",
    target: "http://127.0.0.1:8000",
    entrypoints: ["web", "websecure"],
    tls_resolver: "letsencrypt-production-resolver",
    kind: "control_plane",
    owner_id: "",
    server_id: "node-1",
    server_name: "gotham-prod-01",
  },
];

const routerNodes = [
  { server_id: "node-1", server_name: "gotham-prod-01", sync_status: "synced" },
];

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

/** mockApi seeds the session and answers enough for the pages to render. */
async function mockApi(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  // Catch-all first so the specific routes registered after it win.
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: { user: session.user } });
  });
  await page.route("**/api/v1/teams", async (route) => {
    await route.fulfill({ json: { teams: [team] } });
  });
  await page.route("**/api/v1/teams/*/members", async (route) => {
    await route.fulfill({ json: { members } });
  });
  await page.route("**/api/v1/teams/*/invites", async (route) => {
    await route.fulfill({ json: { invites: [] } });
  });
  await page.route("**/api/v1/proxy/routers", async (route) => {
    await route.fulfill({ json: { routers, nodes: routerNodes } });
  });
  await page.route("**/api/v1/proxy/dns-providers", async (route) => {
    await route.fulfill({ json: { providers: [] } });
  });
  await page.route("**/api/v1/proxy/certificates", async (route) => {
    await route.fulfill({ json: { certificates: [] } });
  });
  await page.route("**/api/v1/proxy/redirects", async (route) => {
    await route.fulfill({ json: { redirects: [] } });
  });
  await page.route("**/api/v1/version", async (route) => {
    await route.fulfill({ json: { version: "v0.2.1-dev" } });
  });
  await page.route("**/api/v1/servers", async (route) => {
    await route.fulfill({ json: { servers: [] } });
  });
  await page.route("**/api/v1/projects", async (route) => {
    await route.fulfill({ json: { projects: [] } });
  });
}

/** pageOverflowX is the smoke check: the page itself never scrolls sideways. */
async function pageOverflowX(page: Page): Promise<number> {
  return page.evaluate(() => {
    const doc = document.scrollingElement as HTMLElement;
    return doc.scrollWidth - window.innerWidth;
  });
}

interface ScrollProof {
  found: boolean;
  scrolls: boolean;
  at: string;
}

/**
 * ownScrollProof walks up from the wide inner element and reports the first
 * `overflow-x: auto|scroll` ancestor: it must exist below the page level and
 * must itself scroll (scrollWidth > clientWidth). This proves the wide
 * content scrolls inside its own container even where the global
 * `body { overflow-x: hidden }` mask would hide a page-level break.
 */
async function ownScrollProof(page: Page, innerSelector: string): Promise<ScrollProof> {
  return page.evaluate((selector: string) => {
    const inner = document.querySelector(selector) as HTMLElement | null;
    if (inner === null) {
      return { found: false, scrolls: false, at: "missing" };
    }
    let current: HTMLElement | null = inner;
    while (current !== null && current !== document.body) {
      const overflowX = getComputedStyle(current).overflowX;
      if (overflowX === "auto" || overflowX === "scroll") {
        return {
          found: true,
          scrolls: current.scrollWidth > current.clientWidth + 1,
          at: `${current.tagName}.${String(current.className).split(" ")[0]}`,
        };
      }
      current = current.parentElement;
    }
    return { found: false, scrolls: false, at: "page" };
  }, innerSelector);
}

test("members table scrolls inside its own container at 390px", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/teams`);
  await page.locator('[data-testid="members-table"] tbody tr').first().waitFor();

  expect(
    await pageOverflowX(page),
    "teams page never scrolls horizontally",
  ).toBeLessThanOrEqual(1);

  // The dedicated wrapper is the overflow container by contract.
  const wrapperOverflow = await page
    .locator('[data-testid="members-table"]')
    .evaluate((node) => {
      const wrapper = node.parentElement as HTMLElement | null;
      return wrapper === null ? "" : getComputedStyle(wrapper).overflowX;
    });
  expect(wrapperOverflow, ".table-scroll carries the horizontal scroll").toBe("auto");

  const proof = await ownScrollProof(page, '[data-testid="members-table"] table');
  expect(proof.found, `wide table has an owning scroller (at ${proof.at})`).toBe(true);
  expect(proof.scrolls, `the owning scroller genuinely scrolls (at ${proof.at})`).toBe(true);
});

test("domains routers table scrolls inside its own container at 390px", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/domains`);
  await page.locator(".n-tabs-tab", { hasText: /^Routers$/ }).click();
  await page.locator('[data-testid="routers-table"] tbody tr').first().waitFor();

  expect(
    await pageOverflowX(page),
    "domains page never scrolls horizontally",
  ).toBeLessThanOrEqual(1);

  const proof = await ownScrollProof(page, '[data-testid="routers-table"] table');
  expect(proof.found, `wide table has an owning scroller (at ${proof.at})`).toBe(true);
  expect(proof.scrolls, `the owning scroller genuinely scrolls (at ${proof.at})`).toBe(true);
});

test("every domains tab fits the viewport at 390px", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/domains`);

  for (const tab of ["Routers", "Certificates", "DNS providers", "Redirects"]) {
    await page.locator(".n-tabs-tab", { hasText: new RegExp(`^${tab}$`) }).click();
    // The tab strip itself never pushes past the viewport: it scrolls
    // internally through the tabs scroll wrapper.
    const navRight = await page.locator(".n-tabs-nav").evaluate((node) => {
      const box = (node as HTMLElement).getBoundingClientRect();
      return box.right - window.innerWidth;
    });
    expect(navRight, `${tab} tab strip stays inside the viewport`).toBeLessThanOrEqual(1);
    expect(
      await pageOverflowX(page),
      `${tab} tab never scrolls the page horizontally`,
    ).toBeLessThanOrEqual(1);
  }
});
