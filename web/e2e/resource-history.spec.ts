import { expect, test } from "./fixtures";
import {
  cloneURL,
  loadAccount,
  nestedURL,
  seedNodeAddress,
  seedProjectEnvironment,
  storageStatePath,
  uniqueSuffix,
} from "./support";

const widths = [390, 480, 1280, 1366];

async function expectNoHorizontalOverflow(
  page: import("@playwright/test").Page,
  width: number,
): Promise<void> {
  await page.setViewportSize({ width, height: 900 });
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth - window.innerWidth,
  );
  expect(overflow, `horizontal overflow at ${width}px`).toBeLessThanOrEqual(1);
}

function iso(offsetMs: number): string {
  return new Date(Date.now() + offsetMs).toISOString();
}

function history(step: "30s" | "5m" = "30s") {
  const bucketStep = step === "30s" ? 30_000 : 300_000;
  const points = Array.from({ length: 6 }, (_, index) => {
    const at = iso(-5 * bucketStep + index * bucketStep);
    return {
      bucket: at,
      cpu_cores: 0.2 + index * 0.05,
      mem_working_set_bytes: 20_000_000 + index * 1_000_000,
      mem_limit_bytes: 0,
      mem_unlimited: true,
      net_rx_bps: index === 3 ? null : 1_000 + index * 100,
      net_tx_bps: 500,
      disk_read_bps: null,
      disk_write_bps: 250,
      latest_at: at,
      replicas: index === 4 ? 1 : 2,
      expected_replicas: 2,
      coverage: index === 4 ? "partial" : "complete",
    };
  });
  return {
    range: "1h",
    step,
    from: iso(-3_600_000),
    to: iso(0),
    capability: "available",
    coverage: "partial",
    series: [
      {
        service: "web",
        deployment_id: "deployment-old-0001",
        points: points.slice(0, 3),
      },
      {
        service: "web",
        deployment_id: "deployment-new-0002",
        points: points.slice(3),
      },
    ],
    markers: [
      { at: points[3]!.bucket, deployment_id: "deployment-new-0002", service: "web" },
    ],
  };
}

test.use({ storageState: storageStatePath });

test.describe("application resource history", () => {
  test("charts samples, gaps, coverage and deployment markers", async ({
    page,
    request,
    guardrails,
  }) => {
    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const server = await request.post("/api/v1/servers", {
      headers,
      data: { name: `ui-e2e-rh-${uniqueSuffix()}`, ip: seedNodeAddress, ssh_user: "root" },
    });
    expect(server.status(), await server.text()).toBe(201);
    const { server: node } = (await server.json()) as { server: { id: string } };
    const { projectId, environmentId } = await seedProjectEnvironment(request, headers);
    const created = await request.post("/api/v1/applications", {
      headers,
      data: {
        name: `ui-e2e-rh-${uniqueSuffix()}`,
        environment_id: environmentId,
        provider: "github",
        repo: "docker/welcome-to-docker",
        clone_url: cloneURL,
        branch: "main",
        build_pack: "auto",
        server_id: node.id,
      },
    });
    expect(created.status(), await created.text()).toBe(201);
    const { application } = (await created.json()) as {
      application: { id: string };
    };

    const seenRanges: string[] = [];
    await page.route(`**/api/v1/applications/${application.id}/metrics?*`, async (route) => {
      const url = new URL(route.request().url());
      const range = url.searchParams.get("range") ?? "1h";
      seenRanges.push(range);
      await route.fulfill({ json: { ...history(range === "1h" ? "30s" : "5m"), range } });
    });

    await page.goto(
      `${nestedURL(projectId, environmentId, "applications", application.id)}?tab=metrics`,
    );
    await expect(page.locator('[data-chart="cpu"] svg')).toBeVisible();
    await expect(page.locator('[data-chart="network"] svg')).toBeVisible();
    await expect(page.locator('[data-chart="cpu"] [data-chart-marker]')).toHaveCount(1);
    await expect(page.getByTestId("resource-history-partial")).toBeVisible();
    await expect(page.getByText("Memory limit: unlimited")).toBeVisible();
    await expect(page.getByRole("radio", { name: "24h" })).toBeVisible();

    await page.getByRole("radio", { name: "24h" }).click({ force: true });
    await expect.poll(() => seenRanges).toContain("24h");
    await expect(page.locator('[data-chart="cpu"] svg')).toBeVisible();

    // Keyboard operation: the range control moves with arrow keys.
    await page.getByRole("radio", { name: "1h" }).click({ force: true });
    await page.getByRole("radio", { name: "1h" }).focus();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("radio", { name: "24h" })).toBeChecked();
    await expect.poll(() => seenRanges.filter((range) => range === "24h").length).toBeGreaterThan(1);

    for (const width of widths) {
      await page.setViewportSize({ width, height: 900 });
      await expectNoHorizontalOverflow(page, width);
      await expect(page.locator('[data-chart="cpu"] svg')).toBeVisible();
    }
    expect(guardrails.apiFailures).toEqual([]);
  });

  test("an older agent reports history unavailable instead of a healthy empty chart", async ({
    page,
    request,
  }) => {
    const account = loadAccount();
    const headers = { Authorization: `Bearer ${account.accessToken}` };
    const server = await request.post("/api/v1/servers", {
      headers,
      data: { name: `ui-e2e-rh-${uniqueSuffix()}`, ip: seedNodeAddress, ssh_user: "root" },
    });
    expect(server.status(), await server.text()).toBe(201);
    const { server: node } = (await server.json()) as { server: { id: string } };
    const { projectId, environmentId } = await seedProjectEnvironment(request, headers);
    const created = await request.post("/api/v1/applications", {
      headers,
      data: {
        name: `ui-e2e-rh-${uniqueSuffix()}`,
        environment_id: environmentId,
        provider: "github",
        repo: "docker/welcome-to-docker",
        clone_url: cloneURL,
        branch: "main",
        build_pack: "auto",
        server_id: node.id,
      },
    });
    expect(created.status(), await created.text()).toBe(201);
    const { application } = (await created.json()) as { application: { id: string } };

    await page.route(`**/api/v1/applications/${application.id}/metrics?*`, async (route) => {
      await route.fulfill({
        json: {
          range: "1h",
          step: "30s",
          from: iso(-3_600_000),
          to: iso(0),
          capability: "unavailable",
          coverage: "unknown",
          series: [],
          markers: [],
        },
      });
    });
    await page.goto(
      `${nestedURL(projectId, environmentId, "applications", application.id)}?tab=metrics`,
    );
    await expect(page.getByText("Resource history unavailable")).toBeVisible();
    await expect(page.locator('[data-chart="cpu"]')).toHaveCount(0);
  });
});
