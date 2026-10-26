import { expect, test } from "./fixtures";
import type { Page } from "@playwright/test";
import { storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

interface InboxRow {
  id: string;
  event_key: string;
  resource_type?: string;
  resource_id?: string;
  project_id?: string;
  name: string;
  host: string;
  error: string;
  created_at: string;
}

const rows: InboxRow[] = [
  {
    id: "notif-2",
    event_key: "deploy_success",
    resource_type: "application",
    resource_id: "app-1",
    project_id: "proj-1",
    name: "web",
    host: "web.example.com",
    error: "",
    created_at: "2024-05-02T10:00:00.123456Z",
  },
  {
    id: "notif-1",
    event_key: "deploy_failure",
    resource_type: "database",
    resource_id: "db-1",
    project_id: "proj-1",
    name: "orders",
    host: "db.example.com",
    error: "build step failed",
    created_at: "2024-05-01T09:30:00Z",
  },
];

interface InboxMock {
  seenAt: string;
  requests: string[];
}

async function mockInbox(
  page: Page,
  initialUnread: number,
  items: InboxRow[] = rows,
): Promise<InboxMock> {
  const state: InboxMock = { seenAt: "", requests: [] };
  let unread = initialUnread;

  await page.route("**/api/v1/notifications**", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    state.requests.push(url.pathname + url.search);

    if (url.pathname.endsWith("/seen")) {
      const body = request.postDataJSON() as { seen_at?: string };
      state.seenAt = body.seen_at ?? "";
      unread = 0;
      await route.fulfill({ status: 204, body: "" });
      return;
    }

    expect(url.searchParams.get("limit")).toBe("30");
    await route.fulfill({
      json: { notifications: items, unread_count: unread },
    });
  });

  return state;
}

test.describe("notification inbox", () => {
  test("badge, popover content and the seen marker", async ({
    page,
    guardrails,
  }) => {
    const state = await mockInbox(page, 2);

    await page.goto("/dashboard");

    const bell = page.getByRole("button", {
      name: "Notifications, 2 unread",
      exact: true,
    });
    await expect(bell).toBeVisible();
    await expect(page.locator(".notif-bell-badge .n-badge-sup")).toContainText(
      "2",
    );

    await bell.click();
    const dialog = page.getByRole("dialog", { name: "Notifications" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("web deployed to web.example.com");
    await expect(dialog).toContainText("orders failed to deploy on db.example.com");
    await expect(dialog).toContainText("build step failed");

    await expect.poll(() => state.seenAt).toBe(rows[0].created_at);
    await expect(
      page.getByRole("button", { name: "Notifications", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".notif-bell-badge .n-badge-sup")).toHaveCount(0);

    await page.reload();
    await expect(
      page.getByRole("button", { name: "Notifications", exact: true }),
    ).toBeVisible();
    await expect(page.locator(".notif-bell-badge .n-badge-sup")).toHaveCount(0);

    expect(state.requests.every((request) => !request.includes("limit=0"))).toBe(
      true,
    );
    expect(guardrails.apiFailures.filter((line) => line.startsWith("400"))).toEqual(
      [],
    );
  });

  test("keyboard opening stays inside the viewport at 390, 480 and 1280 px", async ({
    page,
  }) => {
    await mockInbox(page, 1, [rows[0]]);

    for (const width of [390, 480, 1280]) {
      await page.setViewportSize({ width, height: 800 });
      await page.goto("/dashboard");

      const bell = page.getByRole("button", { name: /Notifications/ });
      await bell.focus();
      await page.keyboard.press("Enter");

      const dialog = page.getByRole("dialog", { name: "Notifications" });
      await expect(dialog).toBeVisible();
      await expect(dialog.locator(".notif-inbox-row")).toHaveCount(1);
      await expect(dialog).toContainText("web deployed to web.example.com");

      const box = await dialog.boundingBox();
      expect(box).not.toBeNull();
      expect(box!.x).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width).toBeLessThanOrEqual(width + 1);
      expect(box!.y).toBeGreaterThanOrEqual(0);
      expect(box!.y + box!.height).toBeLessThanOrEqual(800 + 1);

      await page.keyboard.press("Escape");
      await expect(dialog).toBeHidden();
    }
  });
});
