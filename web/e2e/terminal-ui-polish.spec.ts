import { expect, test } from "./fixtures";
import type { Locator, Page } from "@playwright/test";

import { loadAccount, storageStatePath } from "./support";
import {
  fileManagerURL,
  installSession,
  isPlatformAdmin,
  loginFresh,
  skipOrFail,
  terminalServer,
} from "./terminal-support";

/**
 * JUS-261 terminal UI polish on the REAL stack.
 *
 * Three production defects: the floating window's minimize/close controls did
 * not sit at the header's right edge, the File manager terminal action was an
 * icon-only button in the right-hand cluster instead of a labelled control
 * after "File operations", and the close-confirmation footer buttons touched.
 * The layout assertions below are pixel/box based (no text-only checks) at the
 * four reported widths, and every shell opened here is a real PTY.
 */
// A 900px-tall window leaves the File manager toolbar above the floating
// terminal window, so the ten real clicks are not intercepted by its body.
test.use({
  storageState: storageStatePath,
  viewport: { width: 1366, height: 900 },
});

const account = loadAccount();
/** fresh is this file's own sign-in (see loginFresh). */
let fresh: Awaited<ReturnType<typeof loginFresh>>;

test.beforeAll(async ({ request }) => {
  fresh = await loginFresh(request, account);
});

test.beforeEach(async ({ page }) => {
  await installSession(page, fresh);
});

/** requireTerminalNode skips when the real node is unavailable. */
async function requireTerminalNode(
  request: Parameters<typeof terminalServer>[0],
) {
  skipOrFail(
    !(await isPlatformAdmin(request, account)),
    "the smoke account is not a platform operator (PLATFORM_ADMINS in the UI E2E workflow)",
    test.skip,
  );
  const server = await terminalServer(request, account);
  skipOrFail(
    server === null,
    "no ready node agent is registered (the UI E2E workflow starts one)",
    test.skip,
  );
  return server!;
}

/** boxOf returns a locator's box, failing loudly when it has none. */
async function boxOf(locator: Locator, label: string) {
  const box = await locator.boundingBox();
  if (!box) {
    throw new Error(`${label} has no box`);
  }
  return box;
}

/** terminalButton is the labelled File manager action. */
function terminalButton(page: Page): Locator {
  return page.getByRole("button", { name: "Open terminal here" });
}

test.describe("terminal UI polish", () => {
  test("JUS-261: Terminal follows File operations at 390/480/1280/1366", async ({
    page,
    request,
  }) => {
    test.setTimeout(60_000);
    const server = await requireTerminalNode(request);
    await page.goto(fileManagerURL(server.id));
    await expect(terminalButton(page)).toBeVisible();

    for (const width of [1366, 1280, 480, 390]) {
      await page.setViewportSize({ width, height: 900 });
      const operations = page.getByRole("button", { name: "File operations" });
      const terminal = terminalButton(page);
      await terminal.scrollIntoViewIfNeeded();
      const operationsBox = await boxOf(operations, "File operations");
      const terminalBox = await boxOf(terminal, "Terminal");

      // Directly after File operations, on the same row.
      expect(terminalBox.x).toBeGreaterThanOrEqual(
        operationsBox.x + operationsBox.width - 0.5,
      );
      // Adjacent, not merely somewhere to the right: the old icon-only action
      // lived in the far-right cluster, which this gap bound rules out.
      expect(
        terminalBox.x - (operationsBox.x + operationsBox.width),
      ).toBeLessThanOrEqual(16);
      expect(
        Math.abs(
          terminalBox.y +
            terminalBox.height / 2 -
            (operationsBox.y + operationsBox.height / 2),
        ),
      ).toBeLessThanOrEqual(1);

      // DOM order as well: nothing sits between the two controls.
      const order = await page.evaluate(() =>
        [...document.querySelectorAll(".fm-toolbar-group button")].map(
          (button) =>
            button.getAttribute("aria-label") ?? button.textContent?.trim(),
        ),
      );
      expect(order[order.indexOf("File operations") + 1]).toBe(
        "Open terminal here",
      );

      if (width <= 640) {
        // The shared toolbar breakpoint collapses labels; the aria-label and
        // tooltip keep naming the control.
        await expect(terminal.locator(".fm-btn-label")).toBeHidden();
        await expect(terminal).toHaveAttribute(
          "aria-label",
          "Open terminal here",
        );
      } else {
        await expect(terminal.locator(".fm-btn-label")).toBeVisible();
        await expect(terminal.locator(".fm-btn-label")).toHaveText("Terminal");
      }

      // The toolbar never wraps badly: each group keeps its controls on one
      // row and the whole bar stays within two rows (one at 1366px).
      const layout = await page.evaluate(() => {
        const rows = [...document.querySelectorAll(".fm-toolbar-group")].map(
          (group) => [
            ...new Set(
              [...group.children]
                .filter(
                  (element) => element.getBoundingClientRect().height > 0,
                )
                .map((element) => {
                  const rect = element.getBoundingClientRect();
                  return Math.round(rect.top + rect.height / 2);
                }),
            ),
          ],
        );
        return {
          rows,
          height: document.querySelector(".fm-toolbar")!.getBoundingClientRect()
            .height,
        };
      });
      expect(layout.rows.every((centres) => centres.length === 1)).toBe(true);
      // The labelled action keeps the whole toolbar on its 32px row.
      expect(layout.height).toBeLessThanOrEqual(44);
    }
  });

  test("JUS-261: window controls stay at the right end and the dialog keeps its gap", async ({
    page,
    request,
  }) => {
    test.setTimeout(120_000);
    const server = await requireTerminalNode(request);
    await page.goto(fileManagerURL(server.id));

    // Ten real PTYs make the 390px tab strip genuinely scroll. The button's
    // own single-flight guard means each click waits for the previous create
    // to settle; aria-busy is that settle signal.
    const open = terminalButton(page);
    for (let index = 1; index <= 10; index += 1) {
      await open.scrollIntoViewIfNeeded();
      await open.click();
      await expect(page.locator(".term-tab")).toHaveCount(index);
      await expect(open).not.toHaveAttribute("aria-busy", "true");
    }
    const windowRegion = page.getByRole("region", { name: "Terminal window" });
    await expect(windowRegion).toBeVisible();

    for (const width of [1366, 1280, 480, 390]) {
      await page.setViewportSize({ width, height: 900 });
      const geometry = await page.evaluate(() => {
        const box = (element: Element) => {
          const rect = element.getBoundingClientRect();
          return {
            left: rect.left,
            right: rect.right,
            top: rect.top,
            bottom: rect.bottom,
          };
        };
        const bar = document.querySelector(".term-window-bar") as HTMLElement;
        const tabs = document.querySelector(".term-tabs") as HTMLElement;
        const add = document.querySelector(".term-add-tab") as HTMLElement;
        const min = document.querySelector(".term-window-min") as HTMLElement;
        const close = document.querySelector(".term-window-close") as HTMLElement;
        const addBox = box(add);
        const minBox = box(min);
        const closeBox = box(close);
        return {
          bar: box(bar),
          paddingRight: Number.parseFloat(getComputedStyle(bar).paddingRight),
          tabs: box(tabs),
          scroll: { width: tabs.scrollWidth, client: tabs.clientWidth },
          tabRects: [...document.querySelectorAll(".term-tab")].map(box),
          add: addBox,
          min: minBox,
          close: closeBox,
          addHit: document
            .elementsFromPoint(
              (addBox.left + addBox.right) / 2,
              (addBox.top + addBox.bottom) / 2,
            )
            .some((element) => element === add || add.contains(element)),
          minHit: document
            .elementsFromPoint(
              (minBox.left + minBox.right) / 2,
              (minBox.top + minBox.bottom) / 2,
            )
            .some((element) => element === min || min.contains(element)),
          closeHit: document
            .elementsFromPoint(
              (closeBox.left + closeBox.right) / 2,
              (closeBox.top + closeBox.bottom) / 2,
            )
            .some((element) => element === close || close.contains(element)),
        };
      });

      // The close control's right edge is the header's content right edge and
      // the pair is right of the whole tab strip, with nothing painted over it.
      expect(
        Math.abs(
          geometry.close.right - (geometry.bar.right - geometry.paddingRight),
        ),
      ).toBeLessThanOrEqual(1);
      expect(geometry.tabs.right).toBeLessThanOrEqual(
        geometry.add.left + 0.5,
      );
      expect(geometry.add.right).toBeLessThanOrEqual(geometry.min.left + 0.5);
      expect(geometry.min.right).toBeLessThanOrEqual(geometry.close.left);
      expect(geometry.addHit).toBe(true);
      expect(geometry.minHit).toBe(true);
      expect(geometry.closeHit).toBe(true);

      if (width <= 480) {
        expect(geometry.scroll.width).toBeGreaterThan(geometry.scroll.client);
      } else {
        for (const tab of geometry.tabRects) {
          expect(tab.right).toBeLessThanOrEqual(geometry.add.left + 0.5);
        }
      }
    }

    // Keyboard reachable in DOM order: active tab → add → minimize → close.
    await page.locator('.term-tab-name[aria-selected="true"]').focus();
    await page.keyboard.press("Tab");
    await expect
      .poll(() => page.evaluate(() => document.activeElement?.className ?? ""))
      .toContain("term-add-tab");
    await page.keyboard.press("Tab");
    await expect
      .poll(() => page.evaluate(() => document.activeElement?.className ?? ""))
      .toContain("term-window-min");
    await page.keyboard.press("Tab");
    await expect
      .poll(() => page.evaluate(() => document.activeElement?.className ?? ""))
      .toContain("term-window-close");

    // Real-pointer minimize → rail restore, then close → dialog → cancel.
    await page.setViewportSize({ width: 1366, height: 900 });
    const minimize = page.locator(".term-window-min");
    const minimizeBox = await boxOf(minimize, "minimize");
    await page.mouse.click(
      minimizeBox.x + minimizeBox.width / 2,
      minimizeBox.y + minimizeBox.height / 2,
    );
    await expect(windowRegion).toBeHidden();
    await page.locator(".rail-btn--terminal").click();
    await expect(windowRegion).toBeVisible();

    const close = page.locator(".term-window-close");
    const closeBox = await boxOf(close, "close");
    await page.mouse.click(
      closeBox.x + closeBox.width / 2,
      closeBox.y + closeBox.height / 2,
    );
    const dialog = page.locator(".term-close-modal");
    await expect(dialog).toBeVisible();

    // The footer gap is the spacing token (Naive zooms the card in, so wait
    // for the settled scale before measuring).
    await expect
      .poll(async () =>
        page.evaluate(() => {
          const button = document.querySelector(
            ".term-close-modal .dialog-foot button",
          );
          if (!button) {
            return 0;
          }
          const rect = button.getBoundingClientRect();
          const width = Number.parseFloat(getComputedStyle(button).width);
          return width === 0 ? 0 : rect.width / width;
        }),
      )
      .toBeGreaterThan(0.99);
    const footerGap = await page.evaluate(() => {
      const buttons = [
        ...document.querySelectorAll(".term-close-modal .dialog-foot button"),
      ];
      const first = buttons[0]?.getBoundingClientRect();
      const last = buttons.at(-1)?.getBoundingClientRect();
      const token = Number.parseFloat(
        getComputedStyle(document.documentElement).getPropertyValue("--space-2"),
      );
      return {
        actual: first && last ? last.left - first.right : -1,
        token,
      };
    });
    expect(footerGap.actual).toBeGreaterThanOrEqual(footerGap.token - 0.5);

    await dialog.getByRole("button", { name: "Keep terminals" }).click();
    await expect(dialog).toBeHidden();
    await expect(page.locator(".term-tab")).toHaveCount(10);

    // Cleanup through the UI: close every session and confirm.
    await close.click();
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Close all" }).click();
    await expect(dialog).toBeHidden();
    await expect(windowRegion).toBeHidden();
    await expect(page.locator(".term-tab")).toHaveCount(0);
  });
});
