import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it } from "vitest";

import CodeEditor from "@/features/files/components/CodeEditor.vue";
import ComposeEditor from "@/features/services/components/ComposeEditor.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

function mountEditor(props: Record<string, unknown>) {
  return mount(ComposeEditor, {
    props,
    global: { plugins: [i18n] },
  });
}

describe("ComposeEditor", () => {
  it("renders the stored document through the read-only YAML editor", () => {
    const wrapper = mountEditor({ modelValue: "services:\n  web:\n    image: nginx\n" });
    const editor = wrapper.findComponent(CodeEditor);

    expect(wrapper.find("pre").exists()).toBe(false);
    expect(editor.props("preset")).toBe("yaml");
    expect(editor.props("readOnly")).toBe(true);
    expect(editor.props("wrap")).toBe(true);
    expect(editor.props("modelValue")).toBe("services:\n  web:\n    image: nginx\n");
  });

  it("shows the empty label as the placeholder", () => {
    const wrapper = mountEditor({ modelValue: "" });

    expect(wrapper.findComponent(CodeEditor).props("placeholder")).toBe(
      "No compose document stored.",
    );
  });

  it("keeps the editing view writable", () => {
    const wrapper = mountEditor({ modelValue: "services: {}\n", editing: true });

    expect(wrapper.findComponent(CodeEditor).props("readOnly")).toBe(false);
  });
});
