import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { expect, test } from "@playwright/test";

// Behavioural proof for the JUS-69 modal scroll contract: every card modal
// stays inside the viewport while only its body scrolls — the header and any
// footer actions stay pinned. The fixture stages the Naive card DOM
// (.n-card-header + .n-card-content + footer) against the real
// web/src/shared/styles/main.css. No backend is involved.

const testsDir = dirname(fileURLToPath(import.meta.url));
const fixture = `file://${resolve(testsDir, "modal-scroll.fixture.html")}`;

test.beforeEach(async ({ page }) => {
  await page.goto(fixture);
});

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 1280, height: 640 },
]) {
  test(`JUS-69 plain modal stays in a ${viewport.width}x${viewport.height} viewport and pins its header`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport);
    const card = page.locator("#add-modal");
    const header = page.locator("#add-header");
    const body = page.locator("#add-body");
    // The 2000px body overflows: the card is bounded, the body scrolls.
    expect(await body.evaluate((element) => element.scrollHeight)).toBeGreaterThan(
      await body.evaluate((element) => element.clientHeight),
    );
    const cardBox = await card.boundingBox();
    expect(cardBox, "card has a box").not.toBeNull();
    expect(cardBox!.y).toBeGreaterThanOrEqual(0);
    expect(cardBox!.y + cardBox!.height).toBeLessThanOrEqual(viewport.height);
    const before = await header.boundingBox();
    await body.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    // Scrolling the body to the bottom leaves the header where it was.
    const after = await header.boundingBox();
    expect(after!.y).toBeCloseTo(before!.y, 0);
    expect(after!.y).toBeGreaterThanOrEqual(0);
    const cardAfter = await card.boundingBox();
    expect(cardAfter!.y + cardAfter!.height).toBeLessThanOrEqual(viewport.height);
  });

  test(`JUS-69 wizard modal pins header and footer in a ${viewport.width}x${viewport.height} viewport`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport);
    const card = page.locator("#wizard-modal");
    const header = page.locator("#wizard-header");
    const body = page.locator("#wizard-body");
    const foot = page.locator("#wizard-foot");
    const create = page.locator("#wizard-create");
    // Only the step body scrolls; the card body itself never does.
    expect(await body.evaluate((element) => element.scrollHeight)).toBeGreaterThan(
      await body.evaluate((element) => element.clientHeight),
    );
    await body.evaluate((element) => {
      element.scrollTop = element.scrollHeight;
    });
    const headerBox = await header.boundingBox();
    const footBox = await foot.boundingBox();
    const cardBox = await card.boundingBox();
    expect(headerBox!.y).toBeGreaterThanOrEqual(0);
    expect(footBox!.y + footBox!.height).toBeLessThanOrEqual(viewport.height);
    expect(cardBox!.y + cardBox!.height).toBeLessThanOrEqual(viewport.height);
    // The footer action is the acceptance proof: inside the viewport.
    await expect(create).toBeVisible();
    const createBox = await create.boundingBox();
    expect(createBox!.y + createBox!.height).toBeLessThanOrEqual(viewport.height);
  });
}
