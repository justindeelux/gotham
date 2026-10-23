import { expect, test } from "./fixtures";
import { storageStatePath } from "./support";

test.use({ storageState: storageStatePath });

test.describe("background task progress card", () => {
  test("shows the card for a running task and keeps it across navigation", async ({
    page,
  }) => {
    const running = JSON.stringify({
      channel: "tasks:team-1",
      type: "task",
      data: JSON.stringify({
        task_id: "dep-1",
        kind: "deploy",
        name: "web",
        status: "running",
        step: "building",
        progress: 50,
        app_id: "app-1",
        deployment_id: "dep-1",
        server_id: "srv-1",
        project_id: "proj-1",
        environment_id: "env-1",
        team_id: "team-1",
      }),
    });
    const snapshotEnd = JSON.stringify({
      channel: "tasks:team-1",
      type: "task_snapshot",
    });
    await page.routeWebSocket(/\/api\/v1\/ws/, (ws) => {
      ws.onMessage((message) => {
        const text = typeof message === "string" ? message : "";
        const channel = text.includes("tasks:") ? text.split('"').find((part) => part.startsWith("tasks:")) : undefined;
        if (channel) {
          ws.send(running.replaceAll("tasks:team-1", channel));
          ws.send(snapshotEnd.replaceAll("tasks:team-1", channel));
        }
      });
    });

    await page.goto("/dashboard");
    const card = page.locator('[data-task-id="dep-1"]');
    await expect(card).toBeVisible();
    await expect(card).toContainText("web");
    await expect(card).toContainText("Building");
    await expect(card).toContainText("Running");
    await expect(card.getByRole("link", { name: "View logs" })).toBeVisible();

    await page.goto("/projects");
    await expect(page.locator('[data-task-id="dep-1"]')).toBeVisible();
  });

  test("auto-dismisses success and keeps failure until closed", async ({
    page,
  }) => {
    const frame = (status: string, error = ""): string =>
      JSON.stringify({
        channel: "tasks:team-1",
        type: "task",
        data: JSON.stringify({
          task_id: `dep-${status}`,
          kind: "deploy",
          name: status === "failed" ? "api" : "web",
          status,
          step: status,
          progress: status === "succeeded" ? 100 : 0,
          app_id: "app-1",
          deployment_id: `dep-${status}`,
          team_id: "team-1",
          error,
        }),
      });
    await page.routeWebSocket(/\/api\/v1\/ws/, (ws) => {
      ws.onMessage((message) => {
        const text = typeof message === "string" ? message : "";
        const channel = text.includes("tasks:") ? text.split('"').find((part) => part.startsWith("tasks:")) : undefined;
        if (channel) {
          ws.send(frame("succeeded").replaceAll("tasks:team-1", channel));
          ws.send(frame("failed", " Boom").replaceAll("tasks:team-1", channel));
        }
      });
    });

    await page.goto("/dashboard");
    const failed = page.locator('[data-task-id="dep-failed"]');
    await expect(failed).toBeVisible();
    await expect(failed).toContainText("Boom");
    await expect(page.locator('[data-task-id="dep-succeeded"]')).toBeVisible();
    await expect(page.locator('[data-task-id="dep-succeeded"]')).toBeHidden({
      timeout: 15_000,
    });
    await expect(failed).toBeVisible();
    await failed.getByRole("button", { name: "Dismiss" }).click();
    await expect(failed).toBeHidden();
  });
});
