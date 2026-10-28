import { createServer } from "node:http";
import type { AddressInfo, Server } from "node:net";
import { readFile } from "node:fs/promises";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

import { waitForLayoutSettled } from "./support";

/* global URL, document, getComputedStyle, window:readonly, Element, HTMLElement */

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

const ordinaryRoutes = [
  "/dashboard",
  "/servers",
  "/files",
  "/projects",
  "/terminal",
  "/domains",
  "/users",
  "/settings/notifications",
  "/settings/git-sources",
  "/settings/instance",
  "/settings/updates",
  "/settings/profile",
  "/settings/api-tokens",
  "/settings/mcp",
  "/toolchain",
];

const actionRoutes = new Set([
  "/dashboard",
  "/servers",
  "/projects",
  "/domains",
  "/users",
  "/settings/notifications",
  "/settings/git-sources",
  "/settings/api-tokens",
]);

function json(status: number, body: unknown) {
  return { status, contentType: "application/json", body: JSON.stringify(body) };
}

function color(value: string): string {
  return value.replaceAll(" ", "");
}

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
  const address = server.address() as AddressInfo;
  baseURL = `http://127.0.0.1:${address.port}`;
});

test.afterAll(async () => {
  await new Promise<void>((done) => {
    server.close(() => done());
  });
});

async function mockApi(page: Page): Promise<void> {
  await page.addInitScript((value) => {
    window.localStorage.setItem("gotham.auth.session", value);
  }, JSON.stringify(session));
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const method = request.method();
    const path = request.url().split("/api/v1")[1]!.split("?")[0]!;
    const get = method === "GET";
    if (get && path === "/auth/me") {
      await route.fulfill(json(200, { user: session.user }));
      return;
    }
    if (get && path === "/auth/config") {
      await route.fulfill(json(200, { registrationOpen: false }));
      return;
    }
    if (get && path === "/version") {
      await route.fulfill(json(200, { version: "v0.2.1-dev" }));
      return;
    }
    await route.fulfill(json(404, { message: `unmocked ${method} ${path}` }));
  });
}

interface HeaderStyles {
  gap: string;
  marginBottom: string;
  eyebrow: Record<string, string>;
  title: Record<string, string>;
  desc: Record<string, string>;
  actionBox: { x: number; width: number } | null;
  headBox: { x: number; width: number };
  overflow: boolean;
}

async function measure(page: Page): Promise<HeaderStyles> {
  return page.evaluate(() => {
    const head = document.querySelector<HTMLElement>(".page-head")!;
    const read = (element: Element | null): Record<string, string> => {
      if (!element) {
        throw new Error("missing header element");
      }
      const style = getComputedStyle(element);
      return {
        fontFamily: style.fontFamily,
        fontSize: style.fontSize,
        fontWeight: style.fontWeight,
        lineHeight: style.lineHeight,
        letterSpacing: style.letterSpacing,
        textTransform: style.textTransform,
        color: style.color,
        marginBottom: style.marginBottom,
        maxWidth: style.maxWidth,
      };
    };
    const actions = head.querySelector<HTMLElement>(".page-actions");
    const headRect = head.getBoundingClientRect();
    const actionRect = actions?.getBoundingClientRect();
    return {
      gap: getComputedStyle(head).gap,
      marginBottom: getComputedStyle(head).marginBottom,
      eyebrow: read(head.querySelector(".eyebrow")),
      title: read(head.querySelector("h1")),
      desc: read(head.querySelector(".page-desc")),
      actionBox: actionRect ? { x: actionRect.x, width: actionRect.width } : null,
      headBox: { x: headRect.x, width: headRect.width },
      overflow:
        document.documentElement.scrollWidth >
        document.documentElement.clientWidth + 1,
    };
  });
}

test("ordinary page headers share one computed style contract", async ({ page }) => {
  test.setTimeout(180_000);
  await mockApi(page);

  for (const route of ordinaryRoutes) {
    await page.setViewportSize({ width: 1366, height: 900 });
    await page.goto(`${baseURL}${route}`);
    await page.locator(".page-head .eyebrow").first().waitFor();
    await waitForLayoutSettled(page);
    const desktop = await measure(page);

    expect(desktop.gap, `${route} header gap`).toBe("16px");
    expect(desktop.marginBottom, `${route} header bottom margin`).toBe("24px");

    expect(desktop.eyebrow.fontFamily, `${route} eyebrow family`).toContain(
      "JetBrains Mono",
    );
    expect(desktop.eyebrow.fontSize, `${route} eyebrow size`).toBe("12px");
    expect(desktop.eyebrow.fontWeight, `${route} eyebrow weight`).toBe("400");
    expect(desktop.eyebrow.letterSpacing, `${route} eyebrow tracking`).toBe(
      "0.96px",
    );
    expect(desktop.eyebrow.textTransform, `${route} eyebrow transform`).toBe(
      "uppercase",
    );
    expect(color(desktop.eyebrow.color), `${route} eyebrow color`).toBe(
      "rgb(148,155,164)",
    );

    expect(desktop.title.fontFamily, `${route} title family`).toContain(
      "Google Sans",
    );
    expect(desktop.title.fontSize, `${route} title size`).toBe("24px");
    expect(desktop.title.fontWeight, `${route} title weight`).toBe("700");
    expect(desktop.title.lineHeight, `${route} title leading`).toBe("30px");
    expect(color(desktop.title.color), `${route} title color`).toBe(
      "rgb(242,243,245)",
    );

    expect(desktop.desc.fontFamily, `${route} description family`).toContain(
      "Google Sans",
    );
    expect(desktop.desc.fontSize, `${route} description size`).toBe("14px");
    expect(desktop.desc.fontWeight, `${route} description weight`).toBe("400");
    expect(desktop.desc.lineHeight, `${route} description leading`).toBe(
      "19.25px",
    );
    expect(color(desktop.desc.color), `${route} description color`).toBe(
      "rgb(148,155,164)",
    );
    expect(
      parseFloat(desktop.desc.maxWidth),
      `${route} description measure`,
    ).toBeGreaterThan(600);

    if (actionRoutes.has(route)) {
      expect(desktop.actionBox, `${route} action row`).not.toBeNull();
      const right = desktop.actionBox!.x + desktop.actionBox!.width;
      expect(
        Math.abs(right - (desktop.headBox.x + desktop.headBox.width)),
        `${route} action row ends at the header right edge`,
      ).toBeLessThanOrEqual(1);
    }

    await page.setViewportSize({ width: 390, height: 844 });
    await waitForLayoutSettled(page);
    const mobile = await measure(page);
    expect(mobile.overflow, `${route} horizontal overflow at 390px`).toBe(false);
    expect(mobile.eyebrow.fontSize, `${route} eyebrow size at 390px`).toBe(
      "12px",
    );
    expect(mobile.title.fontSize, `${route} title size at 390px`).toBe("24px");
    expect(mobile.desc.fontSize, `${route} description size at 390px`).toBe(
      "14px",
    );
    if (actionRoutes.has(route)) {
      expect(mobile.actionBox, `${route} action row at 390px`).not.toBeNull();
      expect(
        Math.abs(mobile.actionBox!.x - mobile.headBox.x),
        `${route} action row is left-aligned at 390px`,
      ).toBeLessThanOrEqual(1);
    }
  }
});
