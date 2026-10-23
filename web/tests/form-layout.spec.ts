import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

/* global getComputedStyle:readonly */

const testsDir = dirname(fileURLToPath(import.meta.url));
const fixture = `file://${resolve(testsDir, "form-layout.fixture.html")}`;

async function trackCount(page: Page, selector: string): Promise<number> {
  const columns = await page
    .locator(selector)
    .evaluate((element) => getComputedStyle(element).gridTemplateColumns);
  return columns.split(" ").length;
}

test.beforeEach(async ({ page }) => {
  await page.goto(fixture);
});

test("JUS-19 rows pair at every real container width at or above 480px", async ({
  page,
}) => {
  for (const selector of ["#row-512", "#row-590", "#row-622", "#row-640"]) {
    expect(await trackCount(page, selector), selector).toBe(2);
  }
});

test("JUS-19 rows collapse below the 480px threshold", async ({ page }) => {
  expect(await trackCount(page, "#row-420")).toBe(1);
});

test("JUS-19 a lone item spans both columns", async ({ page }) => {
  const widths = await page.locator("#row-640-lone").evaluate((row) => {
    const lone = row.querySelector("#cell-640-lone");
    if (lone === null) {
      return { row: 0, lone: 0 };
    }
    return {
      row: row.clientWidth,
      lone: lone.getBoundingClientRect().width,
    };
  });
  expect(Math.abs(widths.row - widths.lone)).toBeLessThanOrEqual(2);
});

test("JUS-19 no row child overflows its row at any real width", async ({
  page,
}) => {
  for (const selector of [
    "#row-512",
    "#row-590",
    "#row-622",
    "#row-640",
    "#row-640-lone",
    "#row-420",
  ]) {
    const overflow = await page.locator(selector).evaluate((row) => {
      const items = [row, ...Array.from(row.children)];
      return items.map((element) => ({
        html: element.outerHTML.slice(0, 60),
        scroll: element.scrollWidth,
        client: element.clientWidth,
      }));
    });
    for (const item of overflow) {
      expect(
        item.scroll,
        `${selector} overflow in ${item.html}`,
      ).toBeLessThanOrEqual(item.client + 1);
    }
  }
});

test("JUS-19 the container governs the collapse, not the viewport", async ({
  page,
}) => {
  await page.setViewportSize({ width: 500, height: 800 });
  for (const selector of ["#row-512", "#row-590", "#row-622", "#row-640"]) {
    expect(await trackCount(page, selector), selector).toBe(2);
  }
  expect(await trackCount(page, "#row-420")).toBe(1);
});
