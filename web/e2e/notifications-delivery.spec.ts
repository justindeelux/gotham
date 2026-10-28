import { expect, test } from "./fixtures";
import type { Page, Route } from "@playwright/test";
import { storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

const channel = {
  id: "ch1",
  name: "Deploy alerts",
  kind: "discord",
  enabled: true,
  events: ["deploy_failure", "deploy_success"],
  config: { webhook_url: "https://…/••••" },
  secrets_configured: true,
  health: {
    status: "failing",
    last_failed_at: "2024-05-01T10:00:00Z",
    last_error: "answered status 500",
    pending: 0,
  },
  created_at: "2024-05-01T09:00:00Z",
  updated_at: "2024-05-01T09:00:00Z",
};

interface DeliveryRow {
  id: string;
  channel_id: string;
  event_type: string;
  status: string;
  error_summary?: string;
  created_at: string;
  attempts: number;
  retryable: boolean;
  completed_at?: string;
}

const deliveries: DeliveryRow[] = [
  {
    id: "d2",
    channel_id: "ch1",
    event_type: "test",
    status: "terminal_failed",
    error_summary: "answered status 500",
    created_at: "2024-05-02T10:00:00Z",
    attempts: 1,
    retryable: false,
    completed_at: "2024-05-02T10:00:00Z",
  },
  {
    id: "d1",
    channel_id: "ch1",
    event_type: "deploy_failure",
    status: "terminal_failed",
    error_summary: "answered status 500",
    created_at: "2024-05-01T10:00:00Z",
    attempts: 3,
    retryable: true,
    completed_at: "2024-05-01T10:00:00Z",
  },
];

function json(route: Route, status: number, body: unknown): Promise<void> {
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

async function mockDeliveryHistory(page: Page): Promise<void> {
  let rows = deliveries.map((row) => ({ ...row }));
  await page.route("**/api/v1/**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    const path = url.pathname;
    if (request.method() === "GET" && path === "/api/v1/notification-channels") {
      await json(route, 200, { channels: [channel] });
      return;
    }
    if (
      request.method() === "GET" &&
      path === "/api/v1/notification-channels/ch1/deliveries"
    ) {
      await json(route, 200, { deliveries: rows });
      return;
    }
    if (
      request.method() === "POST" &&
      path === "/api/v1/notification-channels/deliveries/d1/retry"
    ) {
      rows = rows.map((row) =>
        row.id === "d1"
          ? { ...row, status: "delivered", retryable: false, error_summary: "" }
          : row,
      );
      await route.fulfill({ status: 204, body: "" });
      return;
    }
    if (request.method() === "GET" && path === "/api/v1/applications") {
      await json(route, 200, { applications: [] });
      return;
    }
    if (request.method() === "GET" && path === "/api/v1/databases") {
      await json(route, 200, { databases: [] });
      return;
    }
    await route.continue();
  });
}

test.describe("notification delivery history", () => {
  test("retries a real failure and never offers Retry for a test message", async ({
    page,
  }) => {
    await mockDeliveryHistory(page);
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto("/settings/notifications");

    await expect(page.getByText("Deploy alerts")).toBeVisible();
    await page.getByRole("button", { name: "Delivery history" }).click();

    const rows = page.locator("[data-test-channel-history-row]");
    await expect(rows).toHaveCount(2);

    const failureRow = rows.filter({ hasText: "Deploy failed" });
    const testRow = rows.filter({ hasText: "Test message" });
    await expect(failureRow.getByRole("button", { name: "Retry" })).toBeVisible();
    await expect(testRow.getByRole("button", { name: "Retry" })).toHaveCount(0);
    await expect(testRow).toContainText("answered status 500");
    await expect(testRow).toContainText("1 attempts");

    const retry = failureRow.getByRole("button", { name: "Retry" });
    await retry.focus();
    await page.keyboard.press("Enter");

    await expect(failureRow).toContainText("Delivered");
    await expect(failureRow.getByRole("button", { name: "Retry" })).toHaveCount(0);

    const text = (await page.locator("body").innerText()).replace(/\s+/g, " ");
    expect(text).not.toMatch(/monitoring\.[A-Za-z]+\.|notifications\.[A-Za-z]+\./);
    expect(text).not.toContain("undefined");
    expect(text).not.toContain("NaN");
  });
});
