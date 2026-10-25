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

test.use({
  storageState: storageStatePath,
  viewport: { width: 1366, height: 900 },
});

const account = loadAccount();
let fresh: Awaited<ReturnType<typeof loginFresh>>;

test.beforeAll(async ({ request }) => {
  fresh = await loginFresh(request, account);
});

test.beforeEach(async ({ page }) => {
  await installSession(page, fresh);
});

async function requireTerminalNode(
  request: Parameters<typeof terminalServer>[0],
) {
  skipOrFail(
    !(await isPlatformAdmin(request, account)),
    "the smoke account is not the instance owner",
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

async function boxOf(locator: Locator, label: string) {
  const box = await locator.boundingBox();
  if (!box) {
    throw new Error(`${label} has no box`);
  }
  return box;
}

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

      expect(terminalBox.x).toBeGreaterThanOrEqual(
        operationsBox.x + operationsBox.width - 0.5,
      );
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
        await expect(terminal.locator(".fm-btn-label")).toBeHidden();
        await expect(terminal).toHaveAttribute(
          "aria-label",
          "Open terminal here",
        );
      } else {
        await expect(terminal.locator(".fm-btn-label")).toBeVisible();
        await expect(terminal.locator(".fm-btn-label")).toHaveText("Terminal");
      }

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
        const strip = document.querySelector(".term-tabstrip") as HTMLElement;
        const tabs = document.querySelector(".term-tabs") as HTMLElement;
        const add = document.querySelector(".term-add-tab") as HTMLElement;
        const min = document.querySelector(".term-window-min") as HTMLElement;
        const close = document.querySelector(".term-window-close") as HTMLElement;
        const addBox = box(add);
        const minBox = box(min);
        const closeBox = box(close);
        return {
          bar: box(bar),
          strip: box(strip),
          tabsInBar: bar.contains(tabs),
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

      expect(geometry.tabsInBar).toBe(false);
      expect(geometry.strip.top).toBeGreaterThanOrEqual(
        geometry.bar.bottom - 0.5,
      );
      expect(
        Math.abs(
          geometry.close.right - (geometry.bar.right - geometry.paddingRight),
        ),
      ).toBeLessThanOrEqual(1);
      expect(geometry.min.right).toBeLessThanOrEqual(geometry.close.left);
      expect(geometry.tabs.right).toBeLessThanOrEqual(
        geometry.add.left + 0.5,
      );
      expect(geometry.add.right).toBeLessThanOrEqual(
        geometry.strip.right + 0.5,
      );
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

    await page.locator('.term-tab-name[aria-selected="true"]').focus();
    await page.keyboard.press("Tab");
    await expect
      .poll(() => page.evaluate(() => document.activeElement?.className ?? ""))
      .toContain("term-add-tab");
    await page.keyboard.press("Shift+Tab");
    await expect
      .poll(() =>
        page.evaluate(() => document.activeElement?.getAttribute("role") ?? ""),
      )
      .toBe("tab");
    await page.locator(".term-window-min").focus();
    await page.keyboard.press("Tab");
    await expect
      .poll(() => page.evaluate(() => document.activeElement?.className ?? ""))
      .toContain("term-window-close");

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

    await close.click();
    await expect(dialog).toBeVisible();
    await dialog.getByRole("button", { name: "Close all" }).click();
    await expect(dialog).toBeHidden();
    await expect(windowRegion).toBeHidden();
    await expect(page.locator(".term-tab")).toHaveCount(0);
  });
});
