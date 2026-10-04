import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

/* global getComputedStyle:readonly */
// getComputedStyle runs inside page.evaluate (browser context); the repo
// lint envs do not cover tests/, so the global is declared here.

// Behavioural proof for the global form-feedback rules (JUS-16/17/18): real
// Chromium computes styles from the real web/src/styles/main.css against DOM
// shaped exactly like naive-ui 2.45 renders NFormItem (see the fixture). No
// backend is involved — the fixture is a static file.

const testsDir = dirname(fileURLToPath(import.meta.url));
const webRoot = resolve(testsDir, "..");
const fixture = `file://${resolve(testsDir, "form-feedback.fixture.html")}`;

test.beforeEach(async ({ page }) => {
  await page.goto(fixture);
});

test("JUS-16 error text is 12px once the App theme token applies", async ({
  page,
}) => {
  // The token half: App.vue sets all three Naive feedback sizes to 12px, and
  // Naive maps them onto --n-feedback-font-size on every item (verified in
  // naive-ui es/form source). The fixture replicates that inline var.
  const appVue = readFileSync(resolve(webRoot, "src/App.vue"), "utf8");
  for (const size of ["Small", "Medium", "Large"]) {
    expect(appVue).toContain(`feedbackFontSize${size}: "12px"`);
  }
  const fontSize = await page
    .locator("#error-line")
    .evaluate((element) => getComputedStyle(element).fontSize);
  expect(fontSize).toBe("12px");
});

test("JUS-17 no reserved feedback row when the field is valid", async ({
  page,
}) => {
  const display = await page
    .locator("#valid-wrapper")
    .evaluate((element) => getComputedStyle(element).display);
  expect(display).toBe("none");
  const height = await page.locator("#valid-item").evaluate((element) => {
    const wrapper = element.querySelector(".n-form-item-feedback-wrapper");
    return wrapper === null ? 0 : wrapper.getBoundingClientRect().height;
  });
  expect(height).toBe(0);
});

test("JUS-18 hint hides while the error shows, error stays visible", async ({
  page,
}) => {
  const hintDisplay = await page
    .locator("#error-hint")
    .evaluate((element) => getComputedStyle(element).display);
  expect(hintDisplay).toBe("none");
  const errorDisplay = await page
    .locator("#error-line")
    .evaluate((element) => getComputedStyle(element).display);
  expect(errorDisplay).not.toBe("none");
  const validHintDisplay = await page
    .locator("#valid-hint")
    .evaluate((element) => getComputedStyle(element).display);
  expect(validHintDisplay).not.toBe("none");
});

test("JUS-17 stacked fields share one 16px item-to-item gap", async ({
  page,
}) => {
  const marginTop = await page
    .locator("#stacked-second")
    .evaluate((element) => getComputedStyle(element).marginTop);
  expect(marginTop).toBe("16px");
});
