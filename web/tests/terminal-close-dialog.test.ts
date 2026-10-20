// JUS-245: every terminating path routes through one confirmation dialog.
// Cancel preserves the sessions; confirm releases exactly the listed tabs.
/* global document:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import { useTerminalStore } from "@/features/terminal/stores/terminal";
import type { TerminalTab } from "@/features/terminal/stores/terminal";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";

const { default: TerminalCloseDialog } = await import(
  "../src/features/terminal/components/TerminalCloseDialog.vue"
);

/** makeTab builds one minimal tab for the dialog. */
function makeTab(id: string, cwd: string): TerminalTab {
  return {
    id,
    serverId: "server-1",
    serverName: "prod-01",
    target: "host",
    root: "volumes",
    cwd,
    title: `prod-01 · ${cwd}`,
    mode: "window",
    sessionId: `session-${id}`,
    status: "connected",
    cols: 120,
    rows: 32,
    shell: "/bin/bash",
    serviceUser: "gotham-agent",
    exitCode: null,
    exitSignal: "",
    errorCode: null,
    errorMessage: "",
    createdAt: 0,
  };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  document.body.innerHTML = "";
  setActivePinia(createPinia());
});

describe("TerminalCloseDialog", () => {
  it("confirms a single tab and keeps it on cancel", async () => {
    const terminal = useTerminalStore();
    terminal.tabs = [makeTab("a", "app")];
    terminal.requestClose(["a"]);
    const dialog = mount(TerminalCloseDialog, {
      attachTo: document.body,
      global: { plugins: [i18n] },
    });
    await flushPromises();

    expect(document.body.textContent).toContain("Close this terminal?");
    expect(document.body.textContent).toContain("prod-01 · app");
    expect(document.body.textContent).toContain("Keep terminals");

    const cancel = [...document.querySelectorAll("button")].find((button) =>
      button.textContent?.includes("Keep terminals"),
    );
    expect(cancel).toBeDefined();
    cancel!.click();
    await flushPromises();
    expect(terminal.closeRequest).toBeNull();
    expect(terminal.tabs).toHaveLength(1);
    dialog.unmount();
  });

  it("lists a multi-tab window and closes only after confirm", async () => {
    const terminal = useTerminalStore();
    terminal.tabs = [makeTab("a", "app"), makeTab("b", "data")];
    terminal.requestClose(["a", "b"]);
    const dialog = mount(TerminalCloseDialog, {
      attachTo: document.body,
      global: { plugins: [i18n] },
    });
    await flushPromises();

    expect(document.body.textContent).toContain("Close all terminals?");
    expect(document.body.textContent).toContain("prod-01 · data");
    const confirm = [...document.querySelectorAll("button")].find((button) =>
      button.textContent?.includes("Close all"),
    );
    expect(confirm).toBeDefined();
    confirm!.click();
    await flushPromises();
    expect(terminal.tabs).toHaveLength(0);
    expect(terminal.closeRequest).toBeNull();
    dialog.unmount();
  });
});
