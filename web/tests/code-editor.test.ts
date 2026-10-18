// Reusable embedded CodeEditor (JUS-230): preset grammars, the shared
// settings table, save emission and clean teardown. The session factory is
// mocked so the test asserts the component's lifecycle without depending on
// CodeMirror's DOM measurement in jsdom.
/* global document:readonly, localStorage:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import CodeEditor from "@/features/files/components/CodeEditor.vue";
import { CODE_EDITOR_PRESETS } from "@/features/files/editor/presets";
import { DEFAULT_EDITOR_SETTINGS } from "@/features/files/schemas/editorSettings";
import { useEditorSettingsStore } from "@/features/files/stores/editorSettings";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const mocks = vi.hoisted(() => {
  interface FakeSession {
    getDoc: ReturnType<typeof vi.fn>;
    setDoc: ReturnType<typeof vi.fn>;
    setLanguage: ReturnType<typeof vi.fn>;
    setSettings: ReturnType<typeof vi.fn>;
    setReadOnly: ReturnType<typeof vi.fn>;
    focus: ReturnType<typeof vi.fn>;
    requestMeasure: ReturnType<typeof vi.fn>;
    openSearch: ReturnType<typeof vi.fn>;
    openReplace: ReturnType<typeof vi.fn>;
    goToLine: ReturnType<typeof vi.fn>;
    destroy: ReturnType<typeof vi.fn>;
  }

  /** fakeSession tracks the doc so getDoc mirrors a live CodeMirror view. */
  function fakeSession(doc: string): FakeSession {
    let current = doc;
    return {
      getDoc: vi.fn(() => current),
      setDoc: vi.fn((next: string) => {
        current = next;
      }),
      setLanguage: vi.fn(),
      setSettings: vi.fn(),
      setReadOnly: vi.fn(),
      focus: vi.fn(),
      requestMeasure: vi.fn(),
      openSearch: vi.fn(),
      openReplace: vi.fn(),
      goToLine: vi.fn(),
      destroy: vi.fn(),
    };
  }

  const sessions: FakeSession[] = [];
  const createEditorSession = vi.fn(async (options: { doc: string }) => {
    const session = fakeSession(options.doc);
    sessions.push(session);
    return session;
  });

  return { sessions, createEditorSession, fakeSession };
});

vi.mock("@/features/files/composables/useCodeMirror", () => ({
  createEditorSession: mocks.createEditorSession,
}));

beforeEach(() => {
  setActivePinia(createPinia());
  localStorage.clear();
  resetLocaleState();
  syncComposerLocale("en");
  mocks.createEditorSession.mockClear();
  mocks.sessions.length = 0;
});

afterEach(() => {
  document.body.innerHTML = "";
});

/** mountEditor mounts the component and waits for the lazy session build. */
async function mountEditor(props: Record<string, unknown> = {}) {
  const wrapper = mount(CodeEditor, {
    props: { modelValue: "services: {}\n", ...props },
    global: { plugins: [i18n] },
  });
  await flushPromises();
  return wrapper;
}

/** lastOptions is the option bag the component handed to the session factory. */
function lastOptions(): Record<string, unknown> {
  const call = mocks.createEditorSession.mock.calls.at(-1);
  if (!call) {
    throw new Error("createEditorSession was not called");
  }
  return call[0] as Record<string, unknown>;
}

describe("CodeEditor presets", () => {
  it("maps every config preset to a shipped grammar", () => {
    expect(CODE_EDITOR_PRESETS).toEqual({
      yaml: "yaml",
      dockerfile: "dockerfile",
      shell: "shell",
      env: "ini",
      nginx: "nginx",
    });
  });
});

describe("CodeEditor", () => {
  it("builds one session with the preset language and shared settings", async () => {
    const wrapper = await mountEditor({ preset: "yaml" });

    expect(mocks.sessions).toHaveLength(1);
    const options = lastOptions();
    expect(options.language).toBe("yaml");
    expect(options.settings).toMatchObject({
      theme: DEFAULT_EDITOR_SETTINGS.theme,
      tabSize: DEFAULT_EDITOR_SETTINGS.tabSize,
      fontFamily: DEFAULT_EDITOR_SETTINGS.fontFamily,
      autocomplete: DEFAULT_EDITOR_SETTINGS.autocomplete,
    });

    wrapper.unmount();
  });

  it("lets an explicit language win over the preset", async () => {
    const wrapper = await mountEditor({ preset: "shell", language: "nginx" });
    expect(lastOptions().language).toBe("nginx");
    wrapper.unmount();
  });

  it("defaults the aria label through i18n and keeps a caller override", async () => {
    const wrapper = await mountEditor();
    expect(lastOptions().ariaLabel).toBe("Code editor");
    wrapper.unmount();

    syncComposerLocale("vi");
    const vietnamese = await mountEditor();
    expect(lastOptions().ariaLabel).toBe("Trình soạn thảo mã");
    vietnamese.unmount();

    syncComposerLocale("en");
    const labeled = await mountEditor({ ariaLabel: "Compose YAML" });
    expect(lastOptions().ariaLabel).toBe("Compose YAML");
    labeled.unmount();
  });

  it("applies the wrap override on top of the stored settings", async () => {
    const wrapper = await mountEditor({ wrap: true });
    expect(lastOptions().settings).toMatchObject({ wrap: true });

    // A settings change reconfigures the live session and keeps the override.
    useEditorSettingsStore().update({ theme: "light" });
    await flushPromises();
    const applied = mocks.sessions[0].setSettings.mock.calls.at(-1)?.[0];
    expect(applied).toMatchObject({ theme: "light", wrap: true });

    wrapper.unmount();
  });

  it("emits save with the current document and no network call", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    const wrapper = await mountEditor({ modelValue: "a: 1\n" });
    const options = lastOptions();

    (options.onSave as () => void)();

    expect(wrapper.emitted("save")).toEqual([["a: 1\n"]]);
    expect(fetchSpy).not.toHaveBeenCalled();
    fetchSpy.mockRestore();
    wrapper.unmount();
  });

  it("forwards edits through v-model and pulls parent updates back", async () => {
    const wrapper = await mountEditor();
    const options = lastOptions();

    (options.onChange as (_doc: string) => void)("a: 2\n");
    expect(wrapper.emitted("update:modelValue")).toEqual([["a: 2\n"]]);

    await wrapper.setProps({ modelValue: "from: parent\n" });
    expect(mocks.sessions[0].setDoc).toHaveBeenCalledWith("from: parent\n");
    wrapper.unmount();
  });

  it("destroys the mounted session on unmount", async () => {
    const wrapper = await mountEditor();
    const session = mocks.sessions[0];

    wrapper.unmount();

    expect(session.destroy).toHaveBeenCalledTimes(1);
  });

  it("destroys a session that resolves after the component unmounted", async () => {
    let resolveSession: ((_session: unknown) => void) | null = null;
    mocks.createEditorSession.mockImplementationOnce(
      () => new Promise((resolve) => {
        resolveSession = resolve;
      }),
    );

    const wrapper = mount(CodeEditor, {
      props: { modelValue: "" },
      global: { plugins: [i18n] },
    });
    wrapper.unmount();

    const session = mocks.fakeSession("");
    resolveSession?.(session);
    await flushPromises();

    expect(session.destroy).toHaveBeenCalledTimes(1);
  });
});
