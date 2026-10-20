// JUS-249 UI: the quick-command editor and the abbreviation suggestion strip.
// The runtime is a faithful fake that records every accept/insert call, so the
// tests assert the action target and that no path presses Enter on its own.
/* global document:readonly, HTMLElement:readonly, navigator:readonly, window:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";

/** runtimeFakes holds every fake runtime the store creates. */
const runtimeFakes = vi.hoisted(() => {
  class FakeRuntime {
    options: Record<string, unknown>;
    attached: HTMLElement | null = null;
    disposed = false;
    focused = 0;
    accepted: Array<string | undefined> = [];
    inserted: string[] = [];
    composing: boolean[] = [];
    dismissed = 0;

    constructor(options: Record<string, unknown>) {
      this.options = options;
      instances.push(this);
    }

    async connect(): Promise<void> {
      (this.options.onState as (_patch: Record<string, unknown>) => void)({
        status: "connected",
        shell: "/bin/bash",
        serviceUser: "gotham-agent",
      });
    }

    attach(container: HTMLElement): void {
      this.attached = container;
      const host = document.createElement("div");
      host.className = "term-host";
      container.appendChild(host);
    }

    focus(): void {
      this.focused += 1;
    }

    fit(): void {
      // no layout in jsdom
    }

    dispose(): void {
      this.disposed = true;
    }

    acceptSuggestion(commandId?: string): { ok: boolean } {
      this.accepted.push(commandId);
      return { ok: true };
    }

    insertCommand(text: string): { ok: boolean } {
      this.inserted.push(text);
      return { ok: true };
    }

    dismissSuggestion(): void {
      this.dismissed += 1;
    }

    setComposing(composing: boolean): void {
      this.composing.push(composing);
    }

    emitSuggestion(state: Record<string, unknown>): void {
      (this.options.onSuggestion as (_state: Record<string, unknown>) => void)(
        state,
      );
    }
  }
  const instances: FakeRuntime[] = [];
  return { FakeRuntime, instances };
});

/** apiFakes is the HTTP seam the store uses. */
const apiFakes = vi.hoisted(() => ({
  create: vi.fn(),
  close: vi.fn(),
}));

vi.mock("@/features/terminal/transport/terminalRuntime", () => ({
  TerminalRuntime: runtimeFakes.FakeRuntime,
}));

vi.mock("@/features/terminal/api/terminal", () => ({
  createTerminalSession: apiFakes.create,
  closeTerminalSession: apiFakes.close,
  currentTerminalToken: () => "jwt-token",
  terminalFailure: () => ({ code: "generic", message: "failed" }),
  terminalWebSocketUrl: (path: string) => `ws://test${path}`,
}));

const { useTerminalStore } = await import("@/features/terminal/stores/terminal");
const { useQuickCommandsStore } = await import(
  "@/features/terminal/stores/quickCommands"
);
const { default: TerminalSurface } = await import(
  "../src/features/terminal/components/TerminalSurface.vue"
);
const { default: QuickCommandPanel } = await import(
  "../src/features/terminal/components/QuickCommandPanel.vue"
);

/** suggestionState builds one runtime suggestion snapshot. */
function suggestionState(overrides: Record<string, unknown> = {}) {
  return {
    phase: "input",
    detected: true,
    alternateScreen: false,
    composing: false,
    dismissed: false,
    suggestion: {
      abbreviation: "gs",
      commandId: "c1",
      label: "Git status",
      command: "git status",
    },
    ...overrides,
  };
}

let sessionSeq = 0;

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  document.body.innerHTML = "";
  window.localStorage.clear();
  setActivePinia(createPinia());
  runtimeFakes.instances.length = 0;
  sessionSeq = 0;
  apiFakes.create.mockReset();
  apiFakes.close.mockReset();
  apiFakes.close.mockResolvedValue(undefined);
  apiFakes.create.mockImplementation(async (serverId: string) => {
    sessionSeq += 1;
    return {
      id: `session-${sessionSeq}`,
      server_id: serverId,
      target: "host",
      root: "volumes",
      cwd: "",
      cols: 120,
      rows: 32,
      expires_at: "2026-10-20T12:01:00Z",
      websocket_path: `/api/v1/servers/${serverId}/terminal/sessions/session-${sessionSeq}/ws`,
      subprotocol: "gotham.terminal.v1",
    };
  });
});

/** mountSurface opens one page session and mounts the shared surface. */
async function mountSurface() {
  const terminal = useTerminalStore();
  const tab = await terminal.openPageSession({ serverId: "server-1", serverName: "prod" });
  const wrapper = mount(TerminalSurface, {
    props: { tabId: tab.id, variant: "page" },
    attachTo: document.body,
    global: { plugins: [i18n] },
  });
  await flushPromises();
  const runtime = runtimeFakes.instances[0]!;
  return { wrapper, runtime, tab, terminal };
}

describe("terminal suggestion UI", () => {
  it("renders a keyboard-operable suggestion and accepts on click", async () => {
    const { wrapper, runtime } = await mountSurface();
    runtime.emitSuggestion(suggestionState());
    await flushPromises();

    const option = wrapper.find(".term-suggest-option");
    expect(option.exists()).toBe(true);
    expect(option.text()).toContain("gs");
    expect(option.text()).toContain("Git status");
    expect(option.attributes("role")).toBe("option");

    await option.trigger("click");
    expect(runtime.accepted).toHaveLength(1);
    expect(runtime.inserted).toEqual([]);
    expect(runtime.focused).toBeGreaterThan(0);
    wrapper.unmount();
  });

  it("Ctrl+Space focuses the option, Enter accepts, Escape dismisses", async () => {
    const { wrapper, runtime } = await mountSurface();
    runtime.emitSuggestion(suggestionState());
    await flushPromises();

    await wrapper.find(".term-surface").trigger("keydown", {
      key: " ",
      code: "Space",
      ctrlKey: true,
    });
    await flushPromises();
    const option = wrapper.find(".term-suggest-option");
    expect(document.activeElement).toBe(option.element);

    await option.trigger("keydown", { key: "Enter" });
    expect(runtime.accepted).toHaveLength(1);

    runtime.emitSuggestion(suggestionState());
    await flushPromises();
    await wrapper.find(".term-suggest-option").trigger("keydown", { key: "Escape" });
    expect(runtime.dismissed).toBeGreaterThan(0);
    wrapper.unmount();
  });

  it("stops suggestions during IME composition", async () => {
    const { wrapper, runtime } = await mountSurface();
    await wrapper.find(".term-surface").trigger("compositionstart");
    expect(runtime.composing).toEqual([true]);
    await wrapper.find(".term-surface").trigger("compositionend");
    expect(runtime.composing).toEqual([true, false]);
    wrapper.unmount();
  });

  it("never shows the setup hint when prompt marks were seen", async () => {
    const { wrapper, runtime } = await mountSurface();
    runtime.emitSuggestion(suggestionState({ detected: true, suggestion: null }));
    await flushPromises();
    // The panel is teleported; open it and assert the hint is absent.
    await wrapper.find(".term-commands-btn").trigger("click");
    await flushPromises();
    expect(document.body.textContent).not.toContain(
      i18n.global.t("terminal.quickCommands.hintTitle"),
    );
    wrapper.unmount();
  });
});

describe("quick command panel", () => {
  /** mountPanel creates one session, seeds commands, and mounts the panel. */
  async function mountPanel() {
    const terminal = useTerminalStore();
    const commands = useQuickCommandsStore();
    // The panel is normally opened by a live tab; create one so insert has a
    // real target.
    const tab = await terminal.openPageSession({ serverId: "server-1", serverName: "prod" });
    const wrapper = mount(QuickCommandPanel, {
      props: {
        tabId: tab.id,
        shell: "/bin/bash",
        integrationDetected: false,
        alternateScreen: false,
      },
      attachTo: document.body,
      global: { plugins: [i18n] },
    });
    await flushPromises();
    return { wrapper, commands, runtime: runtimeFakes.instances[0]! };
  }

  it("adds, edits and inserts a command without executing it", async () => {
    const { wrapper, commands, runtime } = await mountPanel();

    await wrapper.find(".qc-add").trigger("click");
    await flushPromises();
    const label = wrapper.find(".qc-form input");
    await label.setValue("Deploy");
    const textarea = wrapper.find(".qc-form textarea");
    await textarea.setValue("docker compose up -d");
    const abbreviation = wrapper.findAll(".qc-form input")[1]!;
    await abbreviation.setValue("dcu");
    await wrapper.find(".qc-form").trigger("submit");
    await flushPromises();

    expect(commands.commands).toHaveLength(1);
    expect(commands.commands[0]).toMatchObject({
      label: "Deploy",
      command: "docker compose up -d",
      abbreviation: "dcu",
    });

    await wrapper.find(".qc-insert").trigger("click");
    expect(runtime.inserted).toEqual(["docker compose up -d"]);
    expect(wrapper.emitted("inserted")?.at(-1)).toEqual(["Deploy"]);

    // Edit reopens the same command and updates it in place.
    await wrapper.findAll(".qc-actions button")[1]!.trigger("click");
    await flushPromises();
    await wrapper.find(".qc-form input").setValue("Deploy prod");
    await wrapper.find(".qc-form").trigger("submit");
    await flushPromises();
    expect(commands.commands[0]!.label).toBe("Deploy prod");

    wrapper.unmount();
  });

  it("rejects a duplicate abbreviation and keeps the old list", async () => {
    const { wrapper, commands } = await mountPanel();
    commands.add({ label: "Status", command: "git status", abbreviation: "gs" });

    await wrapper.find(".qc-add").trigger("click");
    await flushPromises();
    await wrapper.find(".qc-form input").setValue("Other");
    await wrapper.find(".qc-form textarea").setValue("git stash");
    const abbreviation = wrapper.findAll(".qc-form input")[1]!;
    await abbreviation.setValue("GS");
    await wrapper.find(".qc-form").trigger("submit");
    await flushPromises();

    // Naive shows the zod message; the store is untouched.
    expect(commands.commands).toHaveLength(1);
    expect(document.body.textContent).toContain(
      i18n.global.t("terminal.quickCommands.validation.abbreviationDuplicate"),
    );
    wrapper.unmount();
  });

  it("refuses unsafe multiline insertion in a shell without bracketed paste", async () => {
    const { wrapper, commands, runtime } = await mountPanel();
    commands.add({ label: "Multi", command: "echo one\necho two", abbreviation: "mm" });
    await flushPromises();
    runtime.insertCommand = () => ({ ok: false, failure: "multiline-no-paste" });

    await wrapper.find(".qc-insert").trigger("click");
    expect(document.body.textContent).toContain(
      i18n.global.t("terminal.quickCommands.insertMultiline"),
    );
    wrapper.unmount();
  });

  it("shows the copyable snippet only when no prompt marks were seen", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const { wrapper } = await mountPanel();
    expect(document.body.textContent).toContain(
      i18n.global.t("terminal.quickCommands.hintTitle"),
    );
    expect(wrapper.find(".qc-snippet").text()).toContain("]133;B");
    await wrapper.find(".qc-copy").trigger("click");
    await flushPromises();
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining("]133;B"));
    wrapper.unmount();
  });
});
