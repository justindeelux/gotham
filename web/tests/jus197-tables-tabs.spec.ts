import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global document:readonly, HTMLElement:readonly, URL, window:readonly, getComputedStyle:readonly */
// global `body { overflow-x: hidden }` mask would hide a break, so these

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
    role: "owner",
  },
  accessToken: "test-access",
  refreshToken: "test-refresh",
};

const users = [
  {
    id: "u-1",
    email: "ada.lovelace-extremely-long-mailbox@very-long-domain-name.example.com",
    role: "owner",
    created_at: "2026-10-01T00:00:00Z",
    projects: [],
  },
  {
    id: "u-2",
    email: "grace.hopper-also-quite-long-mailbox@another-very-long-domain.example.com",
    role: "member",
    created_at: "2026-10-02T00:00:00Z",
    projects: [],
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

async function mockApi(
  page: Page,
  role: "owner" | "member" = "owner",
): Promise<void> {
  const signedIn = {
    ...session,
    user: { ...session.user, role },
  };
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(signedIn));
  await page.route("**/api/v1/**", async (route) => {
    await route.fulfill({ json: {} });
  });
  await page.route("**/api/v1/auth/me", async (route) => {
    await route.fulfill({ json: { user: signedIn.user } });
  });
  await page.route("**/api/v1/users", async (route) => {
    await route.fulfill({ json: { users } });
  });
  await page.route("**/api/v1/invites", async (route) => {
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

test("users table scrolls inside its own container at 390px", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await mockApi(page);
  await page.goto(`${baseURL}/users`);
  await page.locator('[data-testid="users-table"] tbody tr').first().waitFor();

  expect(
    await pageOverflowX(page),
    "users page never scrolls horizontally",
  ).toBeLessThanOrEqual(1);

  const proof = await ownScrollProof(page, '[data-testid="users-table"] table');
  expect(proof.found, `wide table has an owning scroller (at ${proof.at})`).toBe(true);
});

test("the invite dialog is keyboard reachable", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await mockApi(page);
  await page.goto(`${baseURL}/users`);
  await page.getByRole("heading", { name: "Users", exact: true }).waitFor();

  let reached = false;
  for (let index = 0; index < 21; index += 1) {
    await page.keyboard.press("Tab");
    reached = await page.evaluate(() =>
      (document.activeElement?.textContent ?? "").includes("Invite user"),
    );
    if (reached) {
      break;
    }
  }
  expect(reached, "Invite user is reachable with Tab").toBe(true);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog")).toBeVisible();
});

for (const width of [390, 1280]) {
  test(`owner users layout at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page, "owner");
    await page.goto(`${baseURL}/users`);
    await page.getByRole("heading", { name: "Users", exact: true }).waitFor();
    await expect(page.locator('#app-nav a[href="/users"]')).toHaveCount(1);
    expect(await pageOverflowX(page), `users page fits ${width}px`).toBeLessThanOrEqual(1);
  });

  test(`member permission layout at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await mockApi(page, "member");
    await page.goto(`${baseURL}/users`);
    await expect(
      page.getByRole("heading", { name: "You don't have permission to view this" }),
    ).toBeVisible();
    for (const href of ["/domains", "/servers", "/users", "/settings/updates"]) {
      await expect(page.locator(`#app-nav a[href="${href}"]`)).toHaveCount(0);
    }
    expect(await pageOverflowX(page), `denial page fits ${width}px`).toBeLessThanOrEqual(1);
  });
}

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
