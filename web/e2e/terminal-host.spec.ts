import { expect, test } from "./fixtures";
import { loadAccount, storageStatePath } from "./support";
import {
  focusTerminal,
  isPlatformAdmin,
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

/**
 * JUS-251 host-terminal journeys against the REAL stack.
 *
 * The control plane is the embedded `gotham serve` the UI E2E workflow
 * started; the node is a real `gotham-agent` built from this repository and
 * registered to it; every shell below is a real PTY. The suite asserts what a
 * fake cannot: the exact create payload, session-creation counts, the create
 * response's session id against the WebSocket session id, real binary frames
 * (initial resize, zero banner input), real UTF-8 round-trips, and the real
 * process/fd cleanup on close.
 *
 * Tests skip with an explicit reason when this environment has no registered
 * node or the signed-in account is not a platform operator. The UI E2E
 * workflow provisions both, so CI always runs them.
 */

// A viewport tall enough for the full 19-line logo AND for two banners to fit
// on screen at once: the duplicate-banner regression (6b) then shows two title
// rows instead of pushing the first copy out of the viewport. The compact form
// below the minimum rows is by design; this suite pins the full form.
test.use({ storageState: storageStatePath, viewport: { width: 1366, height: 1200 } });

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
async function requireTerminalNode(request: Parameters<typeof terminalServer>[0]) {
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

/** openServerPage opens /terminal from the main menu and picks the test node. */
async function openServerPage(page: import("@playwright/test").Page, name: string) {
  await page.goto("/dashboard");
  await page.locator("#app-nav").getByRole("link", { name: "Terminal" }).click();
  await expect(page).toHaveURL(new RegExp(`${terminalPageURL}$`));
  await expect(
    page.getByRole("heading", { name: "Terminal", level: 1 }),
  ).toBeVisible();
  await pickServer(page, name);
}

/** openQuickCommands opens the JUS-264 manager modal from the meta bar. */
async function openQuickCommands(page: import("@playwright/test").Page) {
  await page.getByRole("button", { name: "Quick commands" }).click();
  await expect(page.locator(".term-quick-commands")).toBeVisible();
}

/** addQuickCommand fills the modal's add form and saves one command. */
async function addQuickCommand(
  page: import("@playwright/test").Page,
  command: { label: string; command: string; abbreviation: string },
) {
  const dialog = page.locator(".term-quick-commands");
  await dialog.getByRole("button", { name: "Add command" }).click();
  await dialog.locator(".qc-form input").first().fill(command.label);
  await dialog.locator(".qc-form textarea").fill(command.command);
  await dialog.locator(".qc-form input").nth(1).fill(command.abbreviation);
  await dialog.getByRole("button", { name: "Save command" }).click();
  await expect(dialog.locator(".qc-item")).toContainText(command.label);
}

/** inputFrames returns the client input frames of one terminal socket. */
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
    // The server picker is lazy: loading the page (and its server list) must
    // not create a session or open a socket.
    await page.waitForTimeout(1_000);
    expect(traffic.createCount()).toBe(0);
    expect(traffic.sockets).toHaveLength(0);

    await pickServer(page, server.name);
    const create = await waitForCreate(traffic);

    expect(create.status).toBe(201);
    expect(create.url).toContain(`/api/v1/servers/${server.id}/terminal/sessions`);
    // JUS-253: a page host shell has no file root and no cwd. Sending empty
    // root/cwd (or requiring them) was the regression the browser proof found.
    expect(create.body).toMatchObject({ target: "host" });
    expect(create.body).not.toHaveProperty("root");
    expect(create.body).not.toHaveProperty("cwd");
    expect(typeof create.body?.cols).toBe("number");
    expect(typeof create.body?.rows).toBe("number");

    // Wait for the prompt and a quiet buffer before counting: a banner written
    // on every output frame (mutation 6b) adds a second copy after the prompt.
    await expect
      .poll(async () => /[@#]/.test(await screenText(page)))
      .toBe(true);
    const text = await expectBanner(page, server.name);
    expect(text).toContain("server: " + server.name);
    // Exactly one banner: both the title row and the first logo row appear
    // once in the settled screen (a tall viewport keeps two copies visible).
    expect(countOccurrences(text, "server: " + server.name)).toBe(1);
    const firstLogoRow = terminalLogoLines()[0]!.trimEnd();
    expect(countOccurrences(text, firstLogoRow)).toBe(1);
    expect(traffic.createCount()).toBe(1);
    expect(traffic.sockets).toHaveLength(1);

    const socket = traffic.currentSocket();
    expect(socket.sessionId).not.toBe("");
    expect(socket.sessionId).toBe(create.sessionId);
    expect(socket.url).toContain(`/terminal/sessions/${create.sessionId}/ws`);
    // The banner is DISPLAY output; it never reaches the PTY.
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

    // One-way transfer: no second create, no reconnect, same buffer.
    expect(traffic.createCount()).toBe(1);
    expect(traffic.sockets).toHaveLength(1);
    expect(traffic.currentSocket()).toBe(socket);
    expect(socket.closed).toBe(false);
    await expect
      .poll(() => screenText(page, ".term-window").then((text) => text.includes(marker)))
      .toBe(true);

    // Minimize keeps the live shell and puts it on the rail.
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

    // Cancel keeps the session, its socket and the window.
    await page.getByRole("button", { name: "Close terminal window" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("button", { name: "Keep terminals" })).toBeVisible();
    await dialog.getByRole("button", { name: "Keep terminals" }).click();
    await expect(dialog).toHaveCount(0);
    await expect(page.locator(".term-window")).toBeVisible();
    expect(socket.closed).toBe(false);
    expect(traffic.deletes).toHaveLength(0);

    // Confirm ends it: the socket closes (close frame or transport close) and
    // the control plane deletes exactly that session.
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
    // Exercise every documented menu key path while the popover is open. None
    // of ArrowDown/Space/Enter/Tab/a printable character may reach the PTY,
    // and Escape must close the menu and return focus to the trigger.
    await page.keyboard.press("ArrowDown");
    await page.keyboard.press("Space");
    await page.keyboard.press("Enter");
    await page.keyboard.type("x");
    await page.keyboard.press("Escape");
    await expect(trigger).toBeFocused();
    // Re-open and exercise Tab while the menu owns focus too; the key must not
    // reach the PTY even when focus leaves the radio group.
    await trigger.click();
    await expect(page.getByRole("radio", { name: "Gotham Light" })).toBeVisible();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Escape");

    await expect
      .poll(async () =>
        page.locator(".term-window [data-theme]").first().getAttribute("data-theme"),
      )
      .not.toBe(beforeTheme);
    // Choosing a theme is pure rendering: no keystroke reaches the PTY and no
    // session is created or replaced.
    expect(traffic.inputFrames()).toBe(beforeInput);
    expect(traffic.createCount()).toBe(1);
    expect(traffic.currentSocket()).toBe(socket);
    expect(socket.closed).toBe(false);
    // The key sequence never typed into the shell (zero input frames) and the
    // trigger owned focus again after Escape.
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

    // BMP characters typed as real CDP key events (xterm ignores synthetic
    // insertText), then the astral emoji through a real paste event.
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

    // A viewport change must fit the PTY and the shell must agree.
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

    // Ctrl-C interrupts a foreground sleep without ending the shell.
    await runCommand(context, page, "sleep 30");
    await page.locator(".xterm-helper-textarea").first().focus();
    await page.keyboard.press("Control+c");
    await runCommand(context, page, "echo AFTER-$((1+1))");
    await expect
      .poll(async () => (await screenText(page)).includes("AFTER-2"))
      .toBe(true);

    // An explicit shell exit ends the session with the exited overlay.
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

    // JUS-264: the button opens one bounded dialog, not a floating popover.
    await openQuickCommands(page);
    const dialog = page.locator(".term-quick-commands");
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("dcup");

    await addQuickCommand(page, {
      label: "Deploy",
      command: "docker-compose up -d --build",
      abbreviation: "dcup",
    });
    await expect(dialog.locator(".qc-item")).toHaveCount(1);
    await expect(dialog).toContainText("docker-compose up -d --build");
    await expect(dialog.locator(".qc-item .n-tag")).toContainText("dcup");

    // JUS-267: Add swaps the list for the editor (never appends it below the
    // list) and the footer keeps a visible action, so the click can never look
    // dead; Cancel returns to the list with Add visible again.
    await dialog.getByRole("button", { name: "Add command" }).click();
    await expect(dialog.locator(".qc-form")).toBeVisible();
    await expect(dialog.locator(".qc-list")).toHaveCount(0);
    await expect(
      dialog.getByRole("button", { name: "Save command" }),
    ).toBeInViewport();
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeVisible();
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog.locator(".qc-form")).toHaveCount(0);
    await expect(dialog.locator(".qc-item")).toHaveCount(1);
    await expect(
      dialog.getByRole("button", { name: "Add command" }),
    ).toBeVisible();

    // The Insert path sends the exact reviewed bytes, never Enter, and hands
    // focus back to the shell.
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

    // Edit keeps the same row and updates the label in place.
    await openQuickCommands(page);
    await dialog.getByRole("button", { name: "Edit Deploy" }).click();
    await dialog.locator(".qc-form input").first().fill("Deploy stack");
    await dialog.getByRole("button", { name: "Save command" }).click();
    await expect(dialog.locator(".qc-item")).toContainText("Deploy stack");

    // Delete asks first, then removes the row.
    await dialog.getByRole("button", { name: "Delete Deploy stack" }).click();
    await page
      .locator(".n-popconfirm")
      .getByRole("button", { name: "Delete", exact: true })
      .click();
    await expect(dialog.locator(".qc-item")).toHaveCount(0);
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

    // JUS-267 on a phone: Add opens the editor in place of the list and keeps
    // the Save and Cancel actions in view instead of appending a form below
    // the fold.
    await dialog.getByRole("button", { name: "Add command" }).click();
    await expect(dialog.locator(".qc-form")).toBeVisible();
    await expect(
      dialog.getByRole("button", { name: "Save command" }),
    ).toBeInViewport();
    await expect(dialog.getByRole("button", { name: "Cancel" })).toBeVisible();
    await dialog.getByRole("button", { name: "Cancel" }).click();
    await expect(dialog.getByRole("button", { name: "Add command" })).toBeVisible();

    // Tab cycles inside the dialog: the trap never lets focus escape.
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

    // Save the ticket's example through the real modal; it is browser-local
    // to this account.
    await openQuickCommands(page);
    await addQuickCommand(page, {
      label: "Docker Compose up",
      command: "docker-compose up -d --build",
      abbreviation: "dcup",
    });
    await page.locator(".term-quick-commands").getByRole("button", { name: "Done" }).click();

    // A real shell emits OSC 133 A/B at every prompt when its rc enables
    // shell integration. Emit the same marks from the live bash prompt so the
    // suggestion engine observes a genuine PTY byte stream.
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

    // Accepting replaces the typed abbreviation with the command and never
    // presses Enter: one frame, exactly delete ×4 + the reviewed text.
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

    // Enter was never sent until the user asks for it: no frame after typing
    // began (and no acceptance frame) carries CR.
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

/** expectBanner waits for the host banner and proves it is the committed logo. */
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
  // The duplicate-banner assertion needs the SETTLED buffer, not the first
  // frame that happens to contain the title.
  const text = await waitForScreenStable(page);
  expect(text).toContain(`server: ${serverName}`);

  // The committed asset is the single source of the banner artwork. Its
  // documented SHA-256 guards against an accidental edit and the rendered
  // lines prove the browser painted exactly that file (not a lookalike).
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

/** countOccurrences counts non-overlapping literal matches. */
function countOccurrences(haystack: string, needle: string): number {
  return haystack.split(needle).length - 1;
}
