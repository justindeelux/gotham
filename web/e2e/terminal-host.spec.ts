import { expect, test } from "./fixtures";
import { loadAccount, storageStatePath } from "./support";
import {
  focusTerminal,
  isOwner,
  lastResize,
  pasteText,
  pressEnter,
  runCommand,
  screenText,
  terminalLogoLines,
  terminalLogoSHA256,
  terminalPageURL,
  pickServer,
  skipOrFail,
  terminalServer,
  typeReal,
  waitForCreate,
  waitForScreenStable,
  watchTerminal,
  installSession,
  loginFresh,
} from "./terminal-support";
import type { SocketLog } from "./terminal-support";

test.use({ storageState: storageStatePath, viewport: { width: 1366, height: 1200 } });

const account = loadAccount();
let fresh: Awaited<ReturnType<typeof loginFresh>>;

test.beforeAll(async ({ request }) => {
  fresh = await loginFresh(request, account);
});

test.beforeEach(async ({ page }) => {
  await installSession(page, fresh);
});

async function requireTerminalNode(request: Parameters<typeof terminalServer>[0]) {
  skipOrFail(
    !(await isOwner(request, account)),
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

async function openServerPage(page: import("@playwright/test").Page, name: string) {
  await page.goto("/dashboard");
  await page.locator("#app-nav").getByRole("link", { name: "Terminal" }).click();
  await expect(page).toHaveURL(new RegExp(`${terminalPageURL}$`));
  await expect(
    page.getByRole("heading", { name: "Terminal", level: 1 }),
  ).toBeVisible();
  await pickServer(page, name);
}

async function openQuickCommands(page: import("@playwright/test").Page) {
  await page.getByRole("button", { name: "Quick commands" }).click();
  await expect(page.locator(".term-quick-commands")).toBeVisible();
}

async function addQuickCommand(
  page: import("@playwright/test").Page,
  command: { label: string; command: string; abbreviation: string },
) {
  const dialog = page.locator(".term-quick-commands");
  await dialog.getByRole("button", { name: "Add command" }).click();
  await dialog.locator(".term-quick-commands-form input").first().fill(command.label);
  await dialog.locator(".term-quick-commands-form textarea").fill(command.command);
  await dialog.locator(".term-quick-commands-form input").nth(1).fill(command.abbreviation);
  await dialog.getByRole("button", { name: "Save command" }).click();
  await expect(dialog.locator(".term-quick-commands-item")).toContainText(command.label);
}

function inputFrames(socket: SocketLog): string[] {
  return socket.frames.filter((frame) => frame.opcode === 0x01).map((frame) => frame.text);
}

test.describe("host terminal on the real stack", () => {
  test("main-menu selection creates exactly one session with the real target", async ({
    page,
    request,
  }) => {
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);

    await page.goto(terminalPageURL);
    await expect(
      page.getByRole("heading", { name: "Terminal", level: 1 }),
    ).toBeVisible();
    await page.waitForTimeout(1_000);
    expect(traffic.createCount()).toBe(0);
    expect(traffic.sockets).toHaveLength(0);

    await pickServer(page, server.name);
    const create = await waitForCreate(traffic);

    expect(create.status).toBe(201);
    expect(create.url).toContain(`/api/v1/servers/${server.id}/terminal/sessions`);
    expect(create.body).toMatchObject({ target: "host" });
    expect(create.body).not.toHaveProperty("root");
    expect(create.body).not.toHaveProperty("cwd");
    expect(typeof create.body?.cols).toBe("number");
    expect(typeof create.body?.rows).toBe("number");

    await expect
      .poll(async () => /[@#]/.test(await screenText(page)))
      .toBe(true);
    const text = await expectBanner(page, server.name);
    expect(text).toContain("server: " + server.name);
    expect(countOccurrences(text, "server: " + server.name)).toBe(1);
    const firstLogoRow = terminalLogoLines()[0]!.trimEnd();
    expect(countOccurrences(text, firstLogoRow)).toBe(1);
    expect(traffic.createCount()).toBe(1);
    expect(traffic.sockets).toHaveLength(1);

    const socket = traffic.currentSocket();
    expect(socket.sessionId).not.toBe("");
    expect(socket.sessionId).toBe(create.sessionId);
    expect(socket.url).toContain(`/terminal/sessions/${create.sessionId}/ws`);
    expect(traffic.inputFrames()).toBe(0);
    expect(lastResize(socket)).not.toBeNull();
  });

  test("page to window keeps one session, its buffer and its socket; rail restore; close Cancel/Confirm", async ({
    page,
    context,
    request,
  }) => {
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);

    await openServerPage(page, server.name);
    const create = await waitForCreate(traffic);
    await expectBanner(page, server.name);
    const socket = traffic.currentSocket();

    const marker = "PAGE-WINDOW-" + create.sessionId.slice(0, 8);
    await runCommand(context, page, `echo ${marker}`);
    await expect
      .poll(async () => (await screenText(page)).includes(marker))
      .toBe(true);

    await page.getByRole("button", { name: "Open in window" }).first().click();
    await expect(page.locator(".term-window")).toBeVisible();

    expect(traffic.createCount()).toBe(1);
    expect(traffic.sockets).toHaveLength(1);
    expect(traffic.currentSocket()).toBe(socket);
    expect(socket.closed).toBe(false);
    await expect
      .poll(() => screenText(page, ".term-window").then((text) => text.includes(marker)))
      .toBe(true);

    await page.getByRole("button", { name: "Minimize terminal window" }).click();
    await expect(page.locator(".term-window")).not.toBeVisible();
    expect(socket.closed).toBe(false);
    expect(traffic.createCount()).toBe(1);
    const rail = page.getByRole("button", { name: /Terminal window, 1 open/ });
    await expect(rail).toBeVisible();
    await rail.click();
    await expect(page.locator(".term-window")).toBeVisible();
    await expect
      .poll(() => screenText(page, ".term-window").then((text) => text.includes(marker)))
      .toBe(true);

    await page.getByRole("button", { name: "Close terminal window" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Keep terminals" })).toBeVisible();
    await dialog.getByRole("button", { name: "Keep terminals" }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.locator(".term-window")).toBeVisible();
    expect(socket.closed).toBe(false);
    expect(traffic.deletes).toHaveLength(0);

    await page.getByRole("button", { name: "Close terminal window" }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: /^(Close all|Close terminal)$/ })
      .click();
    await expect
      .poll(() => socket.closed || socket.frames.some((frame) => frame.opcode === 3))
      .toBe(true);
    await expect
      .poll(() => traffic.deletes.some((url) => url.includes(create.sessionId)))
      .toBe(true);
  });

  test("theme change repaints the window with zero PTY input and no new session", async ({
    page,
    request,
  }) => {
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);

    await openServerPage(page, server.name);
    await waitForCreate(traffic);
    await expectBanner(page, server.name);
    await page.getByRole("button", { name: "Open in window" }).first().click();
    await expect(page.locator(".term-window")).toBeVisible();

    const socket = traffic.currentSocket();
    const beforeInput = traffic.inputFrames();
    const beforeTheme = await page
      .locator(".term-window [data-theme]")
      .first()
      .getAttribute("data-theme");

    const trigger = page.getByRole("button", { name: "Terminal theme" });
    await trigger.click();
    const light = page.getByRole("radio", { name: "Gotham Light" });
    await expect(light).toBeVisible();
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Space");
    await page.keyboard.press("Enter");
    await page.keyboard.type("x");
    await page.keyboard.press("Escape");
    await expect(trigger).toBeFocused();
    await trigger.click();
    await expect(page.getByRole("radio", { name: "Gotham Light" })).toBeVisible();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Escape");

    await expect
      .poll(async () =>
        page.locator(".term-window [data-theme]").first().getAttribute("data-theme"),
      )
      .not.toBe(beforeTheme);
    expect(traffic.inputFrames()).toBe(beforeInput);
    expect(traffic.createCount()).toBe(1);
    expect(traffic.currentSocket()).toBe(socket);
    expect(socket.closed).toBe(false);
    expect(traffic.inputFrames()).toBe(beforeInput);
  });

  test("real UTF-8 typing, paste, resize (stty), Ctrl-C and shell exit", async ({
    page,
    context,
    request,
  }) => {
    test.setTimeout(90_000);
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);

    await openServerPage(page, server.name);
    await waitForCreate(traffic);
    await expectBanner(page, server.name);
    await expect
      .poll(async () => (await screenText(page)).includes("@"))
      .toBe(true);

    await runCommand(context, page, "echo TYPED-é✓ñ-$((6*7))");
    await expect
      .poll(async () => (await screenText(page)).includes("TYPED-é✓ñ-42"))
      .toBe(true);

    await focusTerminal(page);
    await pasteText(page, "echo PASTED-🚀-$((6*7))");
    await pressEnter(page);
    await expect
      .poll(async () => (await screenText(page)).includes("PASTED-🚀-42"))
      .toBe(true);

    const before = lastResize(traffic.currentSocket());
    await page.setViewportSize({ width: 1100, height: 700 });
    await expect
      .poll(() => lastResize(traffic.currentSocket())?.join("x"))
      .not.toBe(before?.join("x"));
    await runCommand(context, page, "stty size");
    const expected = lastResize(traffic.currentSocket());
    expect(expected).not.toBeNull();
    let fit = "";
    await expect
      .poll(async () => {
        fit = await screenText(page);
        return /\d+ \d+/.test(fit);
      })
      .toBe(true);
    const sizes = [...fit.matchAll(/(\d+) (\d+)/g)].map((match) => [
      Number(match[1]),
      Number(match[2]),
    ]);
    expect(sizes).toContainEqual([expected![1], expected![0]]);

    await runCommand(context, page, "sleep 30");
    await page.locator(".xterm-helper-textarea").first().focus();
    await page.keyboard.press("Control+c");
    await runCommand(context, page, "echo AFTER-$((1+1))");
    await expect
      .poll(async () => (await screenText(page)).includes("AFTER-2"))
      .toBe(true);

    await runCommand(context, page, "exit");
    await expect(page.getByText("Session ended")).toBeVisible();
  });

  test("the quick-commands modal adds, edits, deletes and inserts without Enter", async ({
    page,
    request,
  }) => {
    test.setTimeout(90_000);
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);

    await openServerPage(page, server.name);
    await waitForCreate(traffic);
    await expectBanner(page, server.name);

    await openQuickCommands(page);
    const dialog = page.locator(".term-quick-commands");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("dcup");

    await addQuickCommand(page, {
      label: "Deploy",
      command: "docker-compose up -d --build",
      abbreviation: "dcup",
    });
    await expect(dialog.locator(".term-quick-commands-item")).toHaveCount(1);
    await expect(dialog).toContainText("docker-compose up -d --build");
    await expect(dialog.locator(".term-quick-commands-item .n-tag")).toContainText("dcup");

    await dialog.getByRole("button", { name: "Add command" }).click();
    await expect(dialog.locator(".term-quick-commands-form")).toBeVisible();
    await expect(dialog.locator(".term-quick-commands-list")).toHaveCount(0);
    await expect(
      dialog.getByRole("button", { name: "Save command" }),
    ).toBeInViewport();
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeVisible();
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog.locator(".term-quick-commands-form")).toHaveCount(0);
    await expect(dialog.locator(".term-quick-commands-item")).toHaveCount(1);
    await expect(
      dialog.getByRole("button", { name: "Add command" }),
    ).toBeVisible();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.activeElement?.classList.contains("term-quick-commands-add") ?? false,
        ),
      )
      .toBe(true);

    const socket = traffic.currentSocket();
    const before = inputFrames(socket).length;
    await dialog
      .getByRole("button", { name: "Insert Deploy into the terminal" })
      .click();
    await expect.poll(() => inputFrames(socket).length).toBe(before + 1);
    const inserted = inputFrames(socket).at(-1)!;
    expect(inserted).toBe("docker-compose up -d --build");
    expect(inserted).not.toContain("\r");
    await expect(dialog).toBeHidden();
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            document.activeElement?.classList.contains("xterm-helper-textarea") ??
            false,
        ),
      )
      .toBe(true);

    await openQuickCommands(page);
    await dialog.getByRole("button", { name: "Edit Deploy" }).click();
    await dialog.locator(".term-quick-commands-form input").first().fill("Deploy stack");
    await dialog.getByRole("button", { name: "Save command" }).click();
    await expect(dialog.locator(".term-quick-commands-item")).toContainText("Deploy stack");
    await expect
      .poll(() =>
        page.evaluate(
          () => document.activeElement?.getAttribute("aria-label") ?? "",
        ),
      )
      .toBe("Edit Deploy stack");

    await dialog.getByRole("button", { name: "Delete Deploy stack" }).click();
    await page
      .locator(".n-popconfirm")
      .getByRole("button", { name: "Delete", exact: true })
      .click();
    await expect(dialog.locator(".term-quick-commands-item")).toHaveCount(0);
    await expect(dialog).toContainText("dcup");

    await dialog.getByRole("button", { name: "Done" }).click();
    await expect(dialog).toBeHidden();
  });

  test("a phone viewport keeps the modal bounded, focus-trapped and Escape returns to the shell", async ({
    page,
    request,
  }) => {
    test.setTimeout(90_000);
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);

    await openServerPage(page, server.name);
    await waitForCreate(traffic);
    await expectBanner(page, server.name);

    await page.setViewportSize({ width: 390, height: 800 });
    await openQuickCommands(page);
    const dialog = page.locator(".term-quick-commands");
    await expect(dialog).toBeVisible();
    const box = await dialog.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.x).toBeGreaterThanOrEqual(0);
    expect(box!.width).toBeLessThanOrEqual(390);
    expect(box!.y).toBeGreaterThanOrEqual(0);
    expect(box!.y + box!.height).toBeLessThanOrEqual(800);
    await expect(dialog.getByRole("button", { name: "Add command" })).toBeVisible();

    await dialog.getByRole("button", { name: "Add command" }).click();
    await expect(dialog.locator(".term-quick-commands-form")).toBeVisible();
    await expect(
      dialog.getByRole("button", { name: "Save command" }),
    ).toBeInViewport();
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeVisible();
    await dialog.locator(".term-quick-commands-form input").first().fill("Phone command");
    await dialog.locator(".term-quick-commands-form textarea").fill("echo phone");
    await dialog.getByRole("button", { name: "Save command" }).click();
    await expect(dialog.locator(".term-quick-commands-item")).toHaveCount(1);
    await expect
      .poll(() =>
        page.evaluate(
          () => document.activeElement?.classList.contains("term-quick-commands-add") ?? false,
        ),
      )
      .toBe(true);

    await dialog.getByRole("button", { name: "Edit Phone command" }).click();
    await page.keyboard.press("Escape");
    await expect(dialog.locator(".term-quick-commands-form")).toHaveCount(0);
    await expect
      .poll(() =>
        page.evaluate(
          () => document.activeElement?.getAttribute("aria-label") ?? "",
        ),
      )
      .toBe("Edit Phone command");

    await page.setViewportSize({ width: 390, height: 500 });
    await dialog.getByRole("button", { name: "Add command" }).click();
    await expect(
      dialog.getByRole("button", { name: "Save command" }),
    ).toBeInViewport();
    await expect(
      dialog.getByRole("button", { name: "Cancel" }),
    ).toBeInViewport();
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect
      .poll(() =>
        page.evaluate(
          () => document.activeElement?.classList.contains("term-quick-commands-add") ?? false,
        ),
      )
      .toBe(true);

    await page.setViewportSize({ width: 900, height: 205 });
    await dialog.getByRole("button", { name: "Add command" }).click();
    await expect
      .poll(() =>
        page.evaluate(
          () => {
            const body = document.querySelector<HTMLElement>(
              ".term-quick-commands .n-card-content",
            );
            return body ? body.scrollHeight - body.clientHeight : -1;
          },
        ),
      )
      .toBe(0);
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await page.setViewportSize({ width: 390, height: 800 });

    for (let index = 0; index < 12; index += 1) {
      await page.keyboard.press("Tab");
      const inside = await page.evaluate(
        () => document.activeElement?.closest(".term-quick-commands") !== null,
      );
      expect(inside).toBe(true);
    }

    await page.keyboard.press("Escape");
    await expect(dialog).toBeHidden();
    await expect
      .poll(() =>
        page.evaluate(
          () =>
            document.activeElement?.classList.contains("xterm-helper-textarea") ??
            false,
        ),
      )
      .toBe(true);
  });

  test("a real bash prompt mark drives a saved quick-command suggestion", async ({
    page,
    context,
    request,
  }) => {
    test.setTimeout(90_000);
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);

    await openServerPage(page, server.name);
    await waitForCreate(traffic);
    await expectBanner(page, server.name);

    await openQuickCommands(page);
    await addQuickCommand(page, {
      label: "Docker Compose up",
      command: "docker-compose up -d --build",
      abbreviation: "dcup",
    });
    await page.locator(".term-quick-commands").getByRole("button", { name: "Done" }).click();

    await runCommand(context, page, "printf '\\033]133;A\\007\\033]133;B\\007'");
    await page.waitForTimeout(500);
    await focusTerminal(page);
    const socket = traffic.currentSocket();
    const beforeTyping = inputFrames(socket).length;
    await typeReal(context, page, "dcup");

    const option = page.getByRole("option");
    await expect(option).toBeVisible();
    await expect(option).toContainText("Docker Compose up");
    await expect(option).toContainText("docker-compose up -d --build");

    const beforeAccept = inputFrames(socket).length;
    expect(beforeAccept).toBe(beforeTyping + 4);
    await option.click();
    await expect.poll(() => inputFrames(socket).length).toBe(beforeAccept + 1);
    const accepted = inputFrames(socket).at(-1)!;
    expect(accepted).toBe("\x7f\x7f\x7f\x7fdocker-compose up -d --build");
    expect(accepted).not.toContain("\r");
    await expect
      .poll(async () => (await screenText(page)).includes("docker-compose up -d --build"))
      .toBe(true);

    expect(
      inputFrames(socket)
        .slice(beforeTyping)
        .some((frame) => frame.includes("\r")),
    ).toBe(false);
  });

  test("signing out with a live shell asks to close it and Cancel keeps the PTY", async ({
    page,
    request,
  }) => {
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);
    await openServerPage(page, server.name);
    await waitForCreate(traffic);
    await expectBanner(page, server.name);
    const socket = traffic.currentSocket();

    await page.locator(".me-card").first().click();
    await page.getByText("Sign out", { exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText(server.name);
    await dialog.getByRole("button", { name: "Keep terminals" }).click();

    await expect(page).not.toHaveURL(/\/login/);
    expect(socket.closed).toBe(false);
    expect(traffic.deletes).toHaveLength(0);
    await expect(page.locator(".term-page .xterm-rows")).toBeVisible();
  });
});

async function expectBanner(
  page: import("@playwright/test").Page,
  serverName: string,
): Promise<string> {
  await expect
    .poll(
      async () =>
        (await screenText(page).catch(() => "")).includes(
          `server: ${serverName}`,
        ),
      { timeout: 25_000, intervals: [100, 250, 500] },
    )
    .toBe(true);
  const text = await waitForScreenStable(page);
  expect(text).toContain(`server: ${serverName}`);

  expect(terminalLogoSHA256()).toBe(
    "76a2f2e19ef0f58326c1f79c5ac837cc25df96f40b7a6d606d292f27b177a274",
  );
  const rendered = text.split("\n").map((line) => line.trimEnd());
  let cursor = 0;
  for (const logoLine of terminalLogoLines()) {
    const expected = logoLine.trimEnd();
    const found = rendered.indexOf(expected, cursor);
    expect(found, `banner logo line missing: ${expected}`).toBeGreaterThanOrEqual(0);
    cursor = found + 1;
  }
  return text;
}

function countOccurrences(haystack: string, needle: string): number {
  return haystack.split(needle).length - 1;
}
