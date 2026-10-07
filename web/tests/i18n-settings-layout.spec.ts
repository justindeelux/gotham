import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

// Layout proof for I18N-8: the same flex/grid rules the shipped settings
// pages use must hold the longest English and Vietnamese strings without
// horizontal overflow at 390/900/1280px. Static fixture, no backend — the
// strings are copied verbatim from the feature catalogs.

const testsDir = dirname(fileURLToPath(import.meta.url));
const fixture = `file://${resolve(testsDir, "i18n-settings-layout.fixture.html")}`;

const widths = [390, 900, 1280] as const;
const probes = ["en-head", "en-actions", "en-stats", "en-row", "en-team", "vi-head", "vi-actions", "vi-stats", "vi-row", "vi-team"];

test.beforeEach(async ({ page }) => {
  await page.goto(fixture);
});

for (const width of widths) {
  test(`I18N-8 en/vi settings rows fit without sideways scroll at ${width}px`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: 900 });
    for (const id of probes) {
      const overflow = await page.locator(`#${id}`).evaluate((element) => {
        const range = element.getBoundingClientRect();
        return {
          scrollWidth: element.scrollWidth,
          clientWidth: element.clientWidth,
          rectWidth: range.width,
        };
      });
      expect(
        overflow.scrollWidth,
        `#${id} overflows at ${width}px (scroll ${overflow.scrollWidth} > client ${overflow.clientWidth})`,
      ).toBeLessThanOrEqual(Math.max(overflow.clientWidth, width) + 1);
    }
  });
}
