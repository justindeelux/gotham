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

function overlaps(
  a: { x: number; y: number; width: number; height: number },
  b: { x: number; y: number; width: number; height: number },
): boolean {
  return !(
    a.x + a.width <= b.x ||
    b.x + b.width <= a.x ||
    a.y + a.height <= b.y ||
    b.y + b.height <= a.y
  );
}

async function expectChartLabelsInside(
  page: import("@playwright/test").Page,
  width: number,
): Promise<void> {
  const charts = page.locator(".resource-history__chart");
  const chartCount = await charts.count();
  expect(chartCount, `charts at ${width}px`).toBeGreaterThan(0);
  for (let index = 0; index < chartCount; index += 1) {
    const chart = charts.nth(index);
    const box = await chart.boundingBox();
    expect(box, `chart ${index} box at ${width}px`).not.toBeNull();
    const labels = chart.locator(".chart__ylabels text, .chart__xlabels text");
    const labelCount = await labels.count();
    expect(labelCount, `axis labels in chart ${index} at ${width}px`).toBeGreaterThan(0);
    for (let labelIndex = 0; labelIndex < labelCount; labelIndex += 1) {
      const label = labels.nth(labelIndex);
      const labelBox = await label.boundingBox();
      expect(labelBox, `axis label ${labelIndex} box at ${width}px`).not.toBeNull();
      expect(labelBox!.x, `left clip at ${width}px: ${await label.textContent()}`)
        .toBeGreaterThanOrEqual(box!.x - 1);
      expect(labelBox!.x + labelBox!.width, `right clip at ${width}px: ${await label.textContent()}`)
        .toBeLessThanOrEqual(box!.x + box!.width + 1);
      expect(labelBox!.y, `top clip at ${width}px: ${await label.textContent()}`)
        .toBeGreaterThanOrEqual(box!.y - 1);
      expect(labelBox!.y + labelBox!.height, `bottom clip at ${width}px`)
        .toBeLessThanOrEqual(box!.y + box!.height + 1);
    }

    const marker = chart.locator("[data-chart-marker-label]");
    if ((await marker.count()) === 0) {
      continue;
    }
    const markerBox = await marker.first().boundingBox();
    expect(markerBox, `marker label visible at ${width}px`).not.toBeNull();
    for (let labelIndex = 0; labelIndex < labelCount; labelIndex += 1) {
      const label = labels.nth(labelIndex);
      if (!(await label.evaluate((node) => node.closest(".chart__ylabels") !== null))) {
        continue;
      }
      const labelBox = await label.boundingBox();
      if (!labelBox) {
        continue;
      }
      expect(
        overlaps(markerBox!, labelBox),
        `marker overlaps an axis label at ${width}px`,
      ).toBe(false);
    }
  }
}

async function expectNoBrokenText(
  page: import("@playwright/test").Page,
): Promise<void> {
  const panel = page.getByTestId("resource-history");
  await expect(panel.getByText(/monitoring\.[A-Za-z]+\./)).toHaveCount(0);
  await expect(panel.getByText(/undefined|NaN/)).toHaveCount(0);
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
      net_rx_bps: index === 3 ? null : index % 2,
      net_tx_bps: 0,
      disk_read_bps: 1,
      disk_write_bps: 0,
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

function tinyCPUHistory() {
  const data = history();
  const values = [0.0004, 0.0008, 0.0012, 0.002, 0.003, 0.004];
  data.series.forEach((series) => {
    series.points.forEach((point, index) => {
      point.cpu_cores = values[index] ?? values[values.length - 1]!;
    });
  });
  return data;
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
    await expectNoBrokenText(page);

    await page.getByRole("radio", { name: "24h" }).click({ force: true });
    await expect.poll(() => seenRanges).toContain("24h");
    await expect(page.locator('[data-chart="cpu"] svg')).toBeVisible();

    // Keyboard operation: the range control moves with arrow keys.
    await page.getByRole("radio", { name: "1h" }).click({ force: true });
    await page.getByRole("radio", { name: "1h" }).focus();
    await page.keyboard.press("ArrowRight");
    await expect(page.getByRole("radio", { name: "24h" })).toBeChecked();
    await expect.poll(() => seenRanges.filter((range) => range === "24h").length).toBeGreaterThan(1);
    await expectNoBrokenText(page);

    for (const width of widths) {
      await page.setViewportSize({ width, height: 900 });
      await expectNoHorizontalOverflow(page, width);
      await expect(page.locator('[data-chart="cpu"] svg')).toBeVisible();
      await expectNoBrokenText(page);
      await expectChartLabelsInside(page, width);
    }
    expect(guardrails.apiFailures).toEqual([]);
  });

  test("tiny CPU values keep the three Y-axis ticks distinct", async ({
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
    const { application } = (await created.json()) as {
      application: { id: string };
    };

    await page.route(
      `**/api/v1/applications/${application.id}/metrics?*`,
      (route) => route.fulfill({ json: tinyCPUHistory() }),
    );
    await page.goto(
      `${nestedURL(projectId, environmentId, "applications", application.id)}?tab=metrics`,
    );
    const ticks = page.locator('[data-chart="cpu"] .chart__ylabels text');
    await expect(ticks).toHaveCount(3);
    const labels = (await ticks.allTextContents()).map((label) => label.trim());
    expect(new Set(labels).size, `CPU ticks: ${labels.join(", ")}`).toBe(3);
    await expect(page.getByText("0.00", { exact: true })).toHaveCount(0);
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
