import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";
import type { Page, Route } from "@playwright/test";

test.use({ storageState: storageStatePath });

const widths = [390, 480, 1280, 1366];

function iso(minutesAgo: number): string {
  return new Date(Date.now() - minutesAgo * 60_000).toISOString();
}

type MockIncident = Record<string, unknown> & {
  id: string;
  severity: "warning" | "critical";
  lifecycle: "pending" | "firing" | "resolved";
  resource_type: string;
  first_observed_at: string;
};

function incident(
  id: string,
  overrides: Partial<MockIncident> = {},
): MockIncident {
  const started = iso(30);
  return {
    id,
    detector: "runtime_health",
    scope: "project",
    scope_id: "project-1",
    project_id: "project-1",
    resource_type: "application",
    resource_id: "app-1",
    resource_name: "shopfront",
    severity: "warning",
    lifecycle: "firing",
    coverage: "fresh",
    first_observed_at: started,
    last_observed_at: iso(1),
    last_valid_at: iso(1),
    confirmed_at: started,
    delivery_state: "dispatched",
    delivery_attempts: 1,
    created_at: started,
    updated_at: iso(1),
    ...overrides,
  };
}

function nodeIncident(): MockIncident {
  return incident("node-1", {
    detector: "connectivity",
    scope: "node",
    scope_id: "server-1",
    project_id: undefined,
    node_id: "server-1",
    resource_type: "node",
    resource_id: "server-1",
    resource_name: "web-01",
    severity: "critical",
    coverage: "stale",
    first_observed_at: iso(90),
  });
}

function childIncident(id: string, name: string, type: string): MockIncident {
  return incident(id, {
    detector: "runtime_restart",
    resource_type: type,
    resource_id: `${type}-${id}`,
    resource_name: name,
    severity: "warning",
    coverage: "unknown",
    grouped_under: "node-1",
    first_observed_at: iso(60),
  });
}

async function installIncidentRoutes(
  page: Page,
  state: { active: MockIncident[]; resolved: MockIncident[] },
): Promise<void> {
  await page.route("**/api/v1/incidents?*", async (route: Route) => {
    const url = new URL(route.request().url());
    const status = url.searchParams.get("status") ?? "active";
    const severity = url.searchParams.get("severity");
    const resourceType = url.searchParams.get("resource_type");
    let rows =
      status === "resolved"
        ? state.resolved
        : state.active.filter((row) => row.lifecycle !== "resolved");
    if (severity) {
      rows = rows.filter((row) => row.severity === severity);
    }
    if (resourceType) {
      rows = rows.filter((row) => row.resource_type === resourceType);
    }
    await route.fulfill({
      json: {
        incidents: rows,
        active_count: status === "resolved" ? 0 : rows.length,
      },
    });
  });
  await page.route(/\/api\/v1\/incidents\/[^/]+\/acknowledge$/, async (route) => {
    const id = route.request().url().split("/").at(-2) ?? "";
    const row = state.active.find((item) => item.id === id);
    if (row) {
      row.acknowledged_at = new Date().toISOString();
    }
    await route.fulfill({ json: { incident: row } });
  });
  await page.route(/\/api\/v1\/incidents\/[^/]+\/snooze$/, async (route) => {
    const id = route.request().url().split("/").at(-2) ?? "";
    const row = state.active.find((item) => item.id === id);
    const body = route.request().postDataJSON() as { until: string };
    if (row) {
      row.snoozed_until = body.until;
    }
    await route.fulfill({ json: { incident: row } });
  });
}

async function installServers(page: Page, servers: unknown[]): Promise<void> {
  await page.route("**/api/v1/servers", async (route) => {
    await route.fulfill({ json: { servers } });
  });
}

function readyServer(): Record<string, unknown> {
  return {
    id: "server-1",
    name: "web-01",
    ip: "10.0.0.1",
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status: "ready",
    node_id: "server-1",
    os: "Ubuntu 22.04",
    docker_version: "26.1.0",
    arch: "amd64",
    total_mem: 8_000_000_000,
    total_disk: 100_000_000_000,
    cpu_usage: 0.1,
    mem_usage: 0.2,
    disk_usage: 0.3,
    container_count: 2,
    last_seen: new Date().toISOString(),
    created_at: iso(60),
    updated_at: iso(1),
  };
}

async function expectNoHorizontalOverflow(
  page: Page,
  width: number,
): Promise<void> {
  await page.setViewportSize({ width, height: 900 });
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(1);
}

test.describe("needs attention", () => {
  test("orders, groups and acknowledges incidents without auto-acknowledging", async ({
    page,
  }) => {
    const state = {
      active: [
        nodeIncident(),
        childIncident("child-app", "checkout", "application"),
        childIncident("child-db", "orders", "database"),
        incident("warn-1", { detector: "runtime_restart" }),
      ],
      resolved: [],
    };
    await installIncidentRoutes(page, state);

    await page.goto("/dashboard");
    await expect(
      page.getByRole("heading", { name: "Needs attention" }),
    ).toBeVisible();
    await expect(page.getByText("4 active issues")).toBeVisible();

    const groups = page.locator(".incident-group");
    await expect(groups).toHaveCount(2);
    const primary = groups.nth(0);
    await expect(primary.getByText("The server is not responding.")).toBeVisible();
    await expect(primary.getByText("2 affected resources")).toBeVisible();
    await expect(primary.getByText("checkout")).toBeVisible();
    await expect(primary.getByText("orders")).toBeVisible();
    await expect(primary.getByText("Acknowledged")).toHaveCount(0);

    const warning = groups.nth(1);
    await expect(warning.getByText("The application keeps restarting.")).toBeVisible();

    const acknowledge = primary.getByRole("button", { name: "Acknowledge" }).first();
    await acknowledge.focus();
    await page.keyboard.press("Enter");
    await expect(primary.getByText("Acknowledged")).toBeVisible();

    await page.reload();
    await expect(
      page.locator(".incident-group").nth(0).getByText("Acknowledged"),
    ).toBeVisible();
  });

  test("filters by severity and resource and keeps an empty filter clearable", async ({
    page,
  }) => {
    const state = {
      active: [
        nodeIncident(),
        incident("warn-1", {
          detector: "host_cpu",
          resource_type: "node",
          resource_id: "server-1",
          node_id: "server-1",
          resource_name: "web-01",
          severity: "warning",
        }),
      ],
      resolved: [],
    };
    await installIncidentRoutes(page, state);
    await page.goto("/dashboard");
    await expect(page.getByText("2 active issues")).toBeVisible();

    const selects = page.locator(".attention-filters .n-select");
    await selects.nth(0).click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: "Warning" })
      .first()
      .click();
    await expect(page.getByText("1 active issue")).toBeVisible();

    await selects.nth(1).click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: "Database" })
      .first()
      .click();
    await expect(page.getByText("No matching issues")).toBeVisible();
    await expect(selects.nth(1)).toBeVisible();

    await selects.nth(1).click();
    await page
      .locator(".n-base-select-option")
      .filter({ hasText: "All resources" })
      .first()
      .click();
    await expect(page.getByText("No matching issues")).toHaveCount(0);
  });

  test("snoozes for one hour and shows the effective expiration", async ({
    page,
  }) => {
    const state = { active: [incident("warn-1")], resolved: [] };
    await installIncidentRoutes(page, state);
    await page.goto("/dashboard");

    await page.getByRole("button", { name: "Snooze options" }).click();
    await page.getByText("1 hour", { exact: true }).click();
    await expect(page.getByText(/^Snoozed until/)).toBeVisible();
    expect(state.active[0]?.snoozed_until).toBeTruthy();
  });

  test("refetches on reconnect and keeps keyboard focus stable", async ({
    page,
  }) => {
    const state = { active: [incident("warn-1")], resolved: [] };
    await installIncidentRoutes(page, state);
    await page.goto("/dashboard");

    const acknowledge = page.getByRole("button", { name: "Acknowledge" }).first();
    await acknowledge.focus();
    state.active = [
      nodeIncident(),
      incident("warn-1"),
    ];
    await page.evaluate(() => window.dispatchEvent(new Event("online")));
    await expect(page.getByText("2 active issues")).toBeVisible();

    const focused = await page.evaluate(() => ({
      text: document.activeElement?.textContent?.trim() ?? "",
      tag: document.activeElement?.tagName ?? "",
    }));
    expect(focused.tag).toBe("BUTTON");
    expect(focused.text).toBe("Acknowledge");
  });

  test("distinguishes an API error, missing monitoring and healthy state", async ({
    page,
  }) => {
    await installServers(page, [readyServer()]);
    await page.route("**/api/v1/incidents?*", async (route) => {
      await route.abort("failed");
    });
    await page.goto("/dashboard");
    await expect(page.getByTestId("incidents-error")).toBeVisible();
    await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();

    await page.unroute("**/api/v1/incidents?*");
    await page.route("**/api/v1/incidents?*", async (route) => {
      await route.fulfill({ status: 404, json: { message: "not found" } });
    });
    await page.reload();
    await expect(page.getByTestId("incidents-unavailable")).toBeVisible();

    await page.unroute("**/api/v1/incidents?*");
    await page.route("**/api/v1/incidents?*", async (route) => {
      await route.fulfill({ json: { incidents: [], active_count: 0 } });
    });
    await page.reload();
    await expect(page.getByText("Nothing needs attention")).toBeVisible();
    await expect(page.getByText("No monitoring data yet")).toHaveCount(0);
  });

  test("shows no monitoring data when no server has reported", async ({ page }) => {
    await installServers(page, []);
    await page.route("**/api/v1/incidents?*", async (route) => {
      await route.fulfill({ json: { incidents: [], active_count: 0 } });
    });
    await page.goto("/dashboard");
    await expect(page.getByText("No monitoring data yet")).toBeVisible();
  });

  test("keeps long names and controls usable at phone and desktop widths", async ({
    page,
  }) => {
    const longName = `service-${"extremely-long-resource-name-".repeat(8)}`;
    const state = {
      active: [
        nodeIncident(),
        childIncident("child-long", longName, "application"),
        incident("warn-long", {
          detector: "runtime_restart",
          resource_name: longName,
        }),
      ],
      resolved: [
        incident("resolved-1", {
          lifecycle: "resolved",
          resolved_at: iso(10),
          resource_name: longName,
        }),
      ],
    };
    await installIncidentRoutes(page, state);
    await page.goto("/dashboard");
    await page.getByRole("button", { name: "Recently resolved" }).click();
    await expect(page.locator(".resolved-row")).toHaveCount(1);

    for (const width of widths) {
      await page.setViewportSize({ width, height: 900 });
      await expectNoHorizontalOverflow(page, width);
      const box = await page
        .getByRole("button", { name: "Acknowledge" })
        .first()
        .boundingBox();
      expect(box).not.toBeNull();
      if (box) {
        expect(box.x).toBeGreaterThanOrEqual(0);
        expect(box.x + box.width).toBeLessThanOrEqual(width + 1);
      }
      await expect(page.getByRole("button", { name: "Recently resolved" })).toBeVisible();
    }
  });
});
