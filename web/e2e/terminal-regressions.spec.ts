import { expect, test } from "./fixtures";
import { loadAccount, storageStatePath } from "./support";
import {
  agentPid,
  authHeaders,
  fileManagerURL,
  focusTerminal,
  isPlatformAdmin,
  pickServer,
  processRunning,
  ptmxCount,
  runCommand,
  screenText,
  serverTemporaryRoot,
  terminalServer,
  waitForCreate,
  waitForProcessGone,
  watchTerminal,
  installSession,
  loginFresh,
} from "./terminal-support";

/**
 * JUS-251 regressions for the real bugs the live browser proof found.
 *
 * Each scenario drives the real control plane, the real node agent and a real
 * PTY; a fake that accepted whatever the UI sent is exactly what missed these
 * defects. The `sleep`/fd scenario is Linux/macOS native: it inspects the
 * agent process this run started (GOTHAM_E2E_AGENT_PID), never a developer's
 * long-lived agent.
 */

test.use({ storageState: storageStatePath });

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

test.describe("terminal regressions", () => {
  test("JUS-253: a page host shell with no root and no cwd runs on the real backend", async ({
    page,
    context,
    request,
  }) => {
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);

    await page.goto("/terminal");
    await pickServer(page, server.name);
    const create = await waitForCreate(traffic);

    // The exact payload that used to be rejected: no root, no cwd, no empty
    // strings either. The real backend must answer 201 and the PTY must work.
    expect(create.status).toBe(201);
    expect(create.body).toEqual({
      target: "host",
      cols: expect.any(Number),
      rows: expect.any(Number),
    });

    await expect
      .poll(async () => (await screenText(page)).includes("server: " + server.name))
      .toBe(true);
    const marker = "NO-ROOT-" + create.sessionId.slice(0, 8);
    await runCommand(context, page, `echo ${marker}; pwd`);
    await expect
      .poll(async () => (await screenText(page)).includes(marker))
      .toBe(true);
    // A default directory was chosen by the agent, never by the browser.
    const text = await screenText(page);
    expect(text).not.toContain("cwd");
  });

  test("JUS-248/JUS-249: File manager sends the real cwd and a double click creates one window session", async ({
    page,
    context,
    request,
  }) => {
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);

    await page.goto(fileManagerURL(server.id));
    await expect(page.locator(".files-page, main")).toBeVisible();
    // Walk into the platform's temp directory; its relative path is the cwd
    // the browser must send and the absolute path is what the PTY must show.
    const relative = serverTemporaryRoot(server.os);
    for (const segment of relative.split("/")) {
      await page.getByRole("cell", { name: segment, exact: true }).dblclick();
    }
    await expect(page.getByRole("button", { name: "Open terminal here" })).toBeVisible();
    await page.waitForTimeout(500);
    expect(traffic.createCount()).toBe(0);

    // The guard is a double-click: two clicks before the first await settles.
    await page
      .getByRole("button", { name: "Open terminal here" })
      .dblclick({ delay: 40 });
    const create = await waitForCreate(traffic);
    expect(create.status).toBe(201);
    expect(create.body).toMatchObject({
      target: "host",
      root: expect.any(String),
      cwd: relative,
    });
    expect(String(create.body?.root)).not.toBe("");
    expect(traffic.createCount()).toBe(1);

    // The session is in the floating window and starts in that folder.
    await expect(page.locator(".term-window")).toBeVisible();
    await expect
      .poll(() => screenText(page, ".term-window").then((text) => text.includes("/" + relative)))
      .toBe(true);
    const marker = "CWD-" + create.sessionId.slice(0, 8);
    await runCommand(context, page, `pwd; echo ${marker}`, ".term-window");
    await expect
      .poll(() => screenText(page, ".term-window").then((text) => text.includes(marker)))
      .toBe(true);
    expect(traffic.createCount()).toBe(1);
  });

  test("JUS-254: closing a host shell kills its background job and releases the PTY descriptor", async ({
    page,
    context,
    request,
  }) => {
    test.setTimeout(90_000);
    const server = await requireTerminalNode(request);
    const traffic = watchTerminal(page);
    const pid = agentPid();
    const baseline = pid === null ? null : ptmxCount(pid);

    await page.goto("/terminal");
    await pickServer(page, server.name);
    await waitForCreate(traffic);
    await expect
      .poll(async () => (await screenText(page)).includes("server: " + server.name))
      .toBe(true);

    // A job-control background job in its own process group. `sleep 7617` is
    // distinctive enough that no unrelated process can match this assertion.
    await focusTerminal(page);
    await runCommand(context, page, "sleep 7617 &");
    await expect
      .poll(() => processRunning("sleep 7617"))
      .toBe(true);

    // Close from the pane, confirm the dialog.
    await page.getByRole("button", { name: "Close terminal" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Close terminal" }).click();
    await expect
      .poll(() => traffic.sockets.every((socket) => socket.closed))
      .toBe(true);

    expect(
      await waitForProcessGone("sleep 7617"),
      "the session's background sleep survived close (JUS-254)",
    ).toBe(true);

    // Descriptor release: only asserted when the workflow tells us which
    // agent pid it started. The macOS fallback keeps the check useful on a
    // developer machine; a missing inspection tool skips only this part.
    if (pid !== null && baseline !== null) {
      let after = baseline;
      await expect
        .poll(async () => {
          after = ptmxCount(pid) ?? baseline;
          return after;
        }, { timeout: 20_000 })
        .toBe(baseline);
    }
  });

  test("JUS-255: the floating window is on top of the page notice at every breakpoint", async ({
    page,
    request,
  }) => {
    test.setTimeout(120_000);
    const server = await requireTerminalNode(request);
    for (const [width, height] of [
      [1366, 800],
      [1280, 720],
      [480, 800],
      [390, 800],
    ]) {
      const traffic = watchTerminal(page);
      await page.setViewportSize({ width, height });
      await page.goto("/terminal");
      // A fresh session per viewport keeps the geometry deterministic (the
      // window is positioned relative to the viewport on first open).
      await pickServer(page, server.name);
      await waitForCreate(traffic);
      await expect
        .poll(async () => (await screenText(page)).includes("server: " + server.name), {
          timeout: 25_000,
        })
        .toBe(true);
      await page.getByRole("button", { name: "Open in window" }).first().click();
      await expect(page.locator(".term-window")).toBeVisible();
      // The page pane now carries the moved notice. It must never paint above
      // the window: probe the window's centre, corners and status bar.
      await expect(page.locator(".term-pane")).toContainText(
        /open in the window|Minimize the window/i,
      );
      const probes = await page.evaluate(() => {
        const win = document.querySelector<HTMLElement>(".term-window");
        if (win === null) {
          return null;
        }
        const rect = win.getBoundingClientRect();
        const points: Record<string, [number, number]> = {
          center: [rect.left + rect.width / 2, rect.top + rect.height / 2],
          topLeft: [rect.left + 8, rect.top + 8],
          topRight: [rect.right - 8, rect.top + 8],
          bottomLeft: [rect.left + 8, rect.bottom - 8],
          bottomRight: [rect.right - 8, rect.bottom - 8],
          statusBar: [rect.left + 40, rect.bottom - 14],
        };
        const hits: Record<string, boolean> = {};
        for (const [name, [x, y]] of Object.entries(points)) {
          const cx = Math.min(Math.max(x, 1), window.innerWidth - 2);
          const cy = Math.min(Math.max(y, 1), window.innerHeight - 2);
          const top = document.elementFromPoint(cx, cy);
          hits[name] = top !== null && win.contains(top);
        }
        return hits;
      });
      expect(probes, `${width}x${height}: no terminal window`).not.toBeNull();
      for (const [point, hit] of Object.entries(probes!)) {
        expect(hit, `${width}x${height}: ${point} is covered by the page`).toBe(true);
      }
      // Close through the dialog so the next viewport starts empty.
      await page.getByRole("button", { name: "Close terminal window" }).click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: /^(Close all|Close terminal)$/ })
        .click();
      await expect(page.locator(".term-window")).not.toBeVisible();
    }
  });

  test("an unauthenticated create request is refused with no session", async ({
    request,
  }) => {
    await requireTerminalNode(request);
    const servers = await request.get("/api/v1/servers", {
      headers: authHeaders(account),
    });
    const body = (await servers.json()) as { servers: Array<{ id: string }> };
    const serverId = body.servers[0]!.id;
    const anonymous = await request.post(
      `/api/v1/servers/${serverId}/terminal/sessions`,
      { data: { target: "host", cols: 80, rows: 24 } },
    );
    expect(anonymous.status()).toBe(401);
  });
});
