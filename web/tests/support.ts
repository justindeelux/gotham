import type { Page } from "@playwright/test";

/* global document, requestAnimationFrame:readonly */

/**
 * Waits until webfonts and finite CSS animations/transitions have finished.
 * Layout specs read bounding boxes one call at a time: mid-transition the glyph
 * and its frame are sampled from two different animation frames, and `ch`
 * measures depend on the webfont being loaded.
 */
export async function waitForLayoutSettled(page: Page): Promise<void> {
  await page.evaluate(async () => {
    const settle = async (): Promise<void> => {
      await document.fonts.ready;
      const running = document
        .getAnimations()
        .filter(
          (animation) =>
            animation.effect?.getComputedTiming().endTime !== Infinity,
        );
      await Promise.all(
        running.map((animation) => animation.finished.catch(() => undefined)),
      );
    };
    await settle();
    await new Promise<void>((done) => {
      requestAnimationFrame(() => requestAnimationFrame(() => done()));
    });
    await settle();
  });
}
