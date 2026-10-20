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
  terminalServer,
  typeReal,
  waitForCreate,
  watchTerminal,
} from "./terminal-support";

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

// A desktop viewport tall enough for the full 19-line logo: the banner is the
// full committed artwork above its minimum rows and a compact two-line form
// below it (both are by design). This suite pins the full form.
test.use({ storageState: storageStatePath, viewport: { width: 1366, height: 900 } });

const account = loadAccount();

/** requireTerminalNode skips when the real node is unavailable. */
async function requireTerminalNode(request: Parameters<typeof terminalServer>[0]) {
  test.skip(
    !(await isPlatformAdmin(request, account)),
    "the smoke account is not a platform operator (PLATFORM_ADMINS in the UI E2E workflow)",
  );
  const server = await terminalServer(request, account);
  test.skip(
    server === null,
    "no ready node agent is registered (the UI E2E workflow starts one)",
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

    const text = await expectBanner(page, server.name);
    expect(text).toContain("server: " + server.name);
    // Exactly one banner: the server line appears once, never repeated by a
    // second create or by a reconnect.
    expect(countOccurrences(text, "server: " + server.name)).toBe(1);
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

    await page.getByRole("button", { name: "Terminal theme" }).click();
    const light = page.getByRole("radio", { name: "Gotham Light" });
    await expect(light).toBeVisible();
    await light.check();
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

    // Save a command through the real UI; it is browser-local to this account.
    await page.getByRole("button", { name: "Quick commands" }).click();
    await page.getByRole("button", { name: "Add command" }).click();
    await page.getByPlaceholder("Deploy API").fill("E2E quick command");
    await page
      .getByPlaceholder("docker compose up -d")
      .fill("echo QUICK-$((6*7))");
    await page.getByPlaceholder("dcu").fill("dcu");
    await page.getByRole("button", { name: "Save command" }).click();
    await page.keyboard.press("Escape");

    // A real shell emits OSC 133 A/B at every prompt when its rc enables
    // shell integration. Emit the same marks from the live bash prompt so the
    // suggestion engine observes a genuine PTY byte stream.
    await runCommand(context, page, "printf '\\033]133;A\\007\\033]133;B\\007'");
    await page.waitForTimeout(500);
    await focusTerminal(page);
    await typeReal(context, page, "dcu");

    const option = page.getByRole("option");
    await expect(option).toBeVisible();
    await expect(option).toContainText("E2E quick command");
    await expect(option).toContainText("echo QUICK-$((6*7))");
    await option.click();
    // Accepting replaces the abbreviation without running it.
    await expect
      .poll(async () => (await screenText(page)).includes("echo QUICK-$((6*7))"))
      .toBe(true);
    await pressEnter(page);
    await expect
      .poll(async () => (await screenText(page)).includes("QUICK-42"))
      .toBe(true);
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
  const text = await (async () => {
    let current = "";
    await expect
      .poll(
        async () => {
          current = await screenText(page).catch(() => "");
          return current.includes(`server: ${serverName}`);
        },
        { timeout: 25_000, intervals: [100, 250, 500] },
      )
      .toBe(true);
    return current;
  })();

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
