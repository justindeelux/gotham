// JUS-249 UI: the quick-command editor and the abbreviation suggestion strip.
// The runtime is a faithful fake that records every accept/insert call, so the
// tests assert the action target and that no path presses Enter on its own.
/* global document:readonly, HTMLElement:readonly, KeyboardEvent:readonly, Event:readonly, HTMLButtonElement:readonly, HTMLInputElement:readonly, HTMLTextAreaElement:readonly, window:readonly */
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
const { default: QuickCommandsModal } = await import(
  "../src/features/terminal/components/QuickCommandsModal.vue"
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

  it("Ctrl+Shift+Space focuses the option, Enter accepts, Escape dismisses", async () => {
    const { wrapper, runtime } = await mountSurface();
    runtime.emitSuggestion(suggestionState());
    await flushPromises();

    await wrapper.find(".term-surface").trigger("keydown", {
      key: " ",
      code: "Space",
      ctrlKey: true,
      shiftKey: true,
    });
    await flushPromises();
    const option = wrapper.find(".term-suggest-option");
    expect(document.activeElement).toBe(option.element);

    await option.trigger("keydown", { key: "Enter" });
    expect(runtime.accepted).toHaveLength(1);

    runtime.emitSuggestion(suggestionState());
    await flushPromises();
    await wrapper.find(".term-surface").trigger("keydown", {
      key: " ",
      code: "Space",
      ctrlKey: true,
      shiftKey: true,
    });
    await flushPromises();
    await wrapper.find(".term-suggest-option").trigger("keydown", { key: "Escape" });
    expect(runtime.dismissed).toBeGreaterThan(0);
    wrapper.unmount();
  });

  it("leaves Esc and Ctrl+Space to the terminal while the list is not focused", async () => {
    const { wrapper, runtime } = await mountSurface();
    runtime.emitSuggestion(suggestionState());
    await flushPromises();

    const surface = wrapper.find(".term-surface");
    const escape = new KeyboardEvent("keydown", {
      key: "Escape",
      bubbles: true,
      cancelable: true,
    });
    surface.element.dispatchEvent(escape);
    expect(escape.defaultPrevented).toBe(false);

    const ctrlSpace = new KeyboardEvent("keydown", {
      key: " ",
      code: "Space",
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    });
    surface.element.dispatchEvent(ctrlSpace);
    expect(ctrlSpace.defaultPrevented).toBe(false);
    // The fake runtime owns the engine; the UI did not dismiss on its own.
    expect(runtime.dismissed).toBe(0);
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

  it("opens the quick-command manager as a modal from the meta bar", async () => {
    const { wrapper } = await mountSurface();
    expect(document.body.textContent).not.toContain(
      i18n.global.t("terminal.quickCommands.title"),
    );

    await wrapper.find(".term-commands-btn").trigger("click");
    await flushPromises();

    // The modal is teleported to body and lists the empty state with the
    // example that explains the feature. This assertion is the mutation
    // target for "the button must open the modal".
    expect(document.body.textContent).toContain(
      i18n.global.t("terminal.quickCommands.title"),
    );
    expect(document.body.textContent).toContain(
      i18n.global.t("terminal.quickCommands.emptyExample", { shortcut: "dcup" }),
    );
    expect(document.querySelector(".term-quick-commands")).not.toBeNull();
    wrapper.unmount();
  });

  it("returns focus to the terminal when the modal closes (after-leave)", async () => {
    const { wrapper, runtime } = await mountSurface();
    const before = runtime.focused;
    await wrapper.find(".term-commands-btn").trigger("click");
    await flushPromises();

    const done = [...document.querySelectorAll<HTMLButtonElement>("button")].find(
      (element) =>
        element.textContent?.includes(i18n.global.t("terminal.quickCommands.done")),
    );
    expect(done).toBeDefined();
    done!.click();
    await flushPromises();

    // The hide went through the modal; jsdom has no CSS transition, so replay
    // the `closed` event NModal emits on after-leave in a real browser.
    const modal = wrapper.findComponent(QuickCommandsModal);
    expect(modal.props("show")).toBe(false);
    modal.vm.$emit("closed");
    await flushPromises();

    expect(runtime.focused).toBeGreaterThan(before);
    wrapper.unmount();
  });

  it("does not flash the no-marks hint before the grace period", async () => {
    const { wrapper, runtime } = await mountSurface();
    useQuickCommandsStore().add({
      label: "Git status",
      command: "git status",
      abbreviation: "gs",
    });
    runtime.emitSuggestion(
      suggestionState({ detected: false, suggestion: null }),
    );
    await flushPromises();

    // Right after connect a healthy shell may still be printing its prompt.
    expect(document.body.textContent).not.toContain(
      i18n.global.t("terminal.suggestions.noPromptMarks"),
    );

    // A shell that never reports marks shows the one-line hint after the
    // grace period; there is no documentation snippet.
    await vi.waitFor(
      () => {
        expect(document.body.textContent).toContain(
          i18n.global.t("terminal.suggestions.noPromptMarks"),
        );
      },
      { timeout: 4_000, interval: 50 },
    );
    expect(document.querySelector(".qc-snippet")).toBeNull();
    wrapper.unmount();
  });
});

describe("quick command modal", () => {
  /** mountModal creates one session and mounts the manager modal open. */
  async function mountModal() {
    const terminal = useTerminalStore();
    const commands = useQuickCommandsStore();
    // The modal is normally opened by a live tab; create one so insert has a
    // real target.
    const tab = await terminal.openPageSession({ serverId: "server-1", serverName: "prod" });
    const wrapper = mount(QuickCommandsModal, {
      props: { show: true, tabId: tab.id },
      attachTo: document.body,
      global: { plugins: [i18n] },
    });
    await flushPromises();
    return { wrapper, commands, runtime: runtimeFakes.instances[0]! };
  }

  /** click finds one button by its visible text and clicks it. */
  function click(text: string): void {
    const button = [...document.querySelectorAll("button")].find((element) =>
      element.textContent?.includes(text),
    );
    expect(button, `button ${text}`).toBeDefined();
    button!.click();
  }

  /** fill writes a value into a modal field. */
  async function fill(selector: string, value: string): Promise<void> {
    const input = document.querySelector<HTMLInputElement | HTMLTextAreaElement>(
      selector,
    );
    expect(input, selector).not.toBeNull();
    input!.value = value;
    input!.dispatchEvent(new Event("input", { bubbles: true }));
    await flushPromises();
  }

  /** modalIsHidden reads the teleported card's v-show state. */
  function modalIsHidden(): boolean {
    const root = document.querySelector<HTMLElement>(".term-quick-commands");
    return root === null || root.style.display === "none";
  }

  it("adds, edits and inserts a command without executing it", async () => {
    const { wrapper, commands, runtime } = await mountModal();

    click("Add command");
    await flushPromises();
    await fill(".qc-form input", "Deploy");
    await fill(".qc-form textarea", "docker compose up -d");
    // The abbreviation input is the second text input in the form.
    const inputs = document.querySelectorAll(".qc-form input");
    expect(inputs.length).toBeGreaterThanOrEqual(2);
    const abbreviation = inputs[1] as HTMLInputElement;
    abbreviation.value = "dcu";
    abbreviation.dispatchEvent(new Event("input", { bubbles: true }));
    click("Save command");
    await flushPromises();

    expect(commands.commands).toHaveLength(1);
    expect(commands.commands[0]).toMatchObject({
      label: "Deploy",
      command: "docker compose up -d",
      abbreviation: "dcu",
    });

    click("Insert");
    // Insertion is the exact reviewed text: acceptance never appends Enter.
    expect(runtime.inserted).toEqual(["docker compose up -d"]);
    expect(runtime.inserted[0]).not.toContain("\r");
    expect(wrapper.emitted("inserted")?.at(-1)).toEqual(["Deploy"]);
    expect(wrapper.emitted("update:show")?.at(-1)).toEqual([false]);

    // Edit reopens the same command and updates it in place.
    const edit = document.querySelector<HTMLButtonElement>(
      'button[aria-label="Edit Deploy"]',
    );
    expect(edit).not.toBeNull();
    edit!.click();
    await flushPromises();
    await fill(".qc-form input", "Deploy prod");
    click("Save command");
    await flushPromises();
    expect(commands.commands[0]!.label).toBe("Deploy prod");

    wrapper.unmount();
  });

  it("rejects a duplicate abbreviation and keeps the old list", async () => {
    const { wrapper, commands } = await mountModal();
    commands.add({ label: "Status", command: "git status", abbreviation: "gs" });

    click("Add command");
    await flushPromises();
    await fill(".qc-form input", "Other");
    await fill(".qc-form textarea", "git stash");
    const abbreviation = document.querySelectorAll(".qc-form input")[1] as HTMLInputElement;
    abbreviation.value = "GS";
    abbreviation.dispatchEvent(new Event("input", { bubbles: true }));
    click("Save command");
    await flushPromises();

    // Naive shows the zod message; the store is untouched.
    expect(commands.commands).toHaveLength(1);
    expect(document.body.textContent).toContain(
      i18n.global.t("terminal.quickCommands.validation.abbreviationDuplicate"),
    );
    wrapper.unmount();
  });

  it("refuses unsafe multiline insertion in a shell without bracketed paste", async () => {
    const { wrapper, commands, runtime } = await mountModal();
    commands.add({ label: "Multi", command: "echo one\necho two", abbreviation: "mm" });
    await flushPromises();
    runtime.insertCommand = () => ({ ok: false, failure: "multiline-no-paste" });

    click("Insert");
    await flushPromises();
    expect(document.body.textContent).toContain(
      i18n.global.t("terminal.quickCommands.insertMultiline"),
    );
    expect(wrapper.emitted("update:show")).toBeUndefined();
    wrapper.unmount();
  });

  it("swaps the list for the editor and keeps a footer action in every state", async () => {
    const { wrapper, commands } = await mountModal();
    for (let index = 0; index < 12; index += 1) {
      commands.add({
        label: `Command ${index + 1}`,
        command: `echo ${index + 1}`,
      });
    }
    await flushPromises();
    expect(document.querySelectorAll(".qc-item")).toHaveLength(12);

    click("Add command");
    await flushPromises();

    // JUS-267: the editor replaces the list instead of being appended below
    // it, so it is always the first thing in the scrollable body. The footer
    // keeps a visible Cancel action while the Add button is gone.
    expect(document.querySelector(".qc-form")).not.toBeNull();
    expect(document.querySelector(".qc-list")).toBeNull();
    expect(document.querySelector(".qc-add")).toBeNull();
    expect(document.querySelector(".qc-form-actions")).not.toBeNull();
    const cancel = document.querySelector<HTMLButtonElement>(".qc-cancel");
    expect(cancel).not.toBeNull();
    expect(cancel!.textContent).toContain(
      i18n.global.t("terminal.quickCommands.cancel"),
    );

    cancel!.click();
    await flushPromises();
    expect(document.querySelector(".qc-form")).toBeNull();
    expect(document.querySelectorAll(".qc-item")).toHaveLength(12);
    expect(document.querySelector(".qc-add")).not.toBeNull();
    wrapper.unmount();
  });

  it("explains the feature with the dcup example when empty", async () => {
    const { wrapper } = await mountModal();
    expect(document.body.textContent).toContain(
      i18n.global.t("terminal.quickCommands.emptyExample", { shortcut: "dcup" }),
    );
    wrapper.unmount();
  });

  it("deletes a command after confirmation", async () => {
    const { wrapper, commands } = await mountModal();
    commands.add({ label: "Status", command: "git status", abbreviation: "gs" });
    await flushPromises();

    const remove = document.querySelector<HTMLButtonElement>(
      'button[aria-label="Delete Status"]',
    );
    expect(remove).not.toBeNull();
    remove!.click();
    await flushPromises();
    click("Delete");
    await flushPromises();

    expect(commands.commands).toHaveLength(0);
    wrapper.unmount();
  });

  it("closes the form on the first Escape and the modal on the second", async () => {
    const { wrapper } = await mountSurface();
    await wrapper.find(".term-commands-btn").trigger("click");
    await flushPromises();

    click("Add command");
    await flushPromises();
    await fill(".qc-form input", "Discarded draft");

    const label = document.querySelector<HTMLInputElement>(".qc-form input");
    expect(label).not.toBeNull();
    label!.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Escape",
        bubbles: true,
        cancelable: true,
      }),
    );
    await flushPromises();
    // The draft is discarded, not saved, and the modal stays open.
    expect(document.querySelector(".qc-form")).toBeNull();
    expect(useQuickCommandsStore().commands).toHaveLength(0);
    expect(modalIsHidden()).toBe(false);

    // The second Escape (now no form) closes the modal.
    document.querySelector<HTMLButtonElement>(".qc-add")!.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Escape",
        bubbles: true,
        cancelable: true,
      }),
    );
    await flushPromises();
    await flushPromises();
    expect(modalIsHidden()).toBe(true);
    wrapper.unmount();
  });
});
