import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

/* global getComputedStyle:readonly */
// getComputedStyle runs inside page.evaluate (browser context); the repo
// lint envs do not cover tests/, so the global is declared here.

// Behavioural proof for the shared .form-row utility (JUS-19 fix 1): real
// Chromium computes styles from the real web/src/styles/main.css against
// plain DOM shaped like paired NFormItems. No backend is involved — the
// fixture is a static file. The collapse keys off the CONTAINER width
// (form-container), not the viewport, so a fixed 560px modal stays
// single-column at any viewport while wide wizards keep two columns.

const testsDir = dirname(fileURLToPath(import.meta.url));
const fixture = `file://${resolve(testsDir, "form-layout.fixture.html")}`;

/** trackCount returns the number of grid columns an element renders. */
async function trackCount(
  page: import("@playwright/test").Page,
  selector: string,
): Promise<number> {
  const columns = await page
    .locator(selector)
    .evaluate((element) => getComputedStyle(element).gridTemplateColumns);
  return columns.split(" ").length;
}

test.beforeEach(async ({ page }) => {
  await page.goto(fixture);
});

test("JUS-19 rows render two columns in a 900px container", async ({
  page,
}) => {
  expect(await trackCount(page, "#wide-row")).toBe(2);
});

test("JUS-19 rows collapse to one column in a 400px container", async ({
  page,
}) => {
  expect(await trackCount(page, "#narrow-row")).toBe(1);
});

test("JUS-19 a lone item spans both columns", async ({ page }) => {
  const widths = await page.locator("#wide-lone-row").evaluate((row) => {
    const lone = row.querySelector("#wide-lone");
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

test("JUS-19 no row child overflows its row", async ({ page }) => {
  for (const selector of ["#wide-row", "#wide-lone-row", "#narrow-row"]) {
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
  // At a 500px viewport the old viewport-keyed rule would stack everything;
  // the container query keeps the 900px container at two columns.
  await page.setViewportSize({ width: 500, height: 800 });
  expect(await trackCount(page, "#wide-row")).toBe(2);
  expect(await trackCount(page, "#narrow-row")).toBe(1);
});
