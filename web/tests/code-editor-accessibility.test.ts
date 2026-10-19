// JUS-237 fix: the embedded editor's accessible name, empty-document hint and
// invalid state follow the active locale and the caller's schema against a real
// CodeMirror view, without remounting the editor or losing the typed draft.
/* global document:readonly, HTMLElement:readonly */
import { EditorView } from "@codemirror/view";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { computed, defineComponent, h, nextTick, ref } from "vue";
import { useI18n } from "vue-i18n";

import CodeEditor from "@/features/files/components/CodeEditor.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const typed = "services:\n  web:\n    image: nginx:1.27-alpine\n";
const edited = `${typed}# edited after mount\n`;

beforeEach(() => {
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  document.body.innerHTML = "";
});

/** mountLocaleHarness wires CodeEditor to the applications compose strings. */
function mountLocaleHarness() {
  const Harness = defineComponent({
    setup() {
      const { t } = useI18n();
      const doc = ref(typed);
      const invalid = ref(false);
      const ariaLabel = computed(() => t("applications.compose.content"));
      const placeholder = computed(() => t("applications.compose.placeholder"));
      const error = computed(() => t("applications.compose.contentInvalid"));
      return () =>
        h(CodeEditor, {
          modelValue: doc.value,
          "onUpdate:modelValue": (value: string) => {
            doc.value = value;
          },
          preset: "yaml",
          ariaLabel: ariaLabel.value,
          placeholder: placeholder.value,
          invalid: invalid.value,
          error: error.value,
        });
    },
  });
  return mount(Harness, { global: { plugins: [i18n] } });
}

/** editorView resolves the real CodeMirror view behind the content element. */
function editorView(wrapper: ReturnType<typeof mountLocaleHarness>): EditorView {
  const content = wrapper.find(".cm-content");
  expect(content.exists()).toBe(true);
  const view = EditorView.findFromDOM(content.element as HTMLElement);
  if (!view) {
    throw new Error("CodeMirror view is not attached");
  }
  return view;
}

describe("CodeEditor accessibility (real CodeMirror)", () => {
  it("updates the aria label on a locale switch and keeps the typed draft", async () => {
    const wrapper = mountLocaleHarness();
    await vi.waitFor(() => {
      expect(wrapper.find(".cm-content").exists()).toBe(true);
    });

    const content = wrapper.find(".cm-content");
    expect(content.attributes("aria-label")).toBe("Compose file");

    // Type through the real view, as a user would, and let v-model land.
    const view = editorView(wrapper);
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: edited } });
    await nextTick();
    await flushPromises();

    syncComposerLocale("vi");
    await nextTick();
    await flushPromises();

    expect(wrapper.find(".cm-content").attributes("aria-label")).toBe("File compose");
    expect((wrapper.find(".cm-content").element as HTMLElement).textContent).toContain(
      "edited after mount",
    );
    expect(editorView(wrapper).state.doc.toString()).toBe(edited);
    wrapper.unmount();
  });

  it("updates the empty-document placeholder on a locale switch", async () => {
    const wrapper = mountLocaleHarness();
    await vi.waitFor(() => {
      expect(wrapper.find(".cm-content").exists()).toBe(true);
    });

    const view = editorView(wrapper);
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: "" } });
    await nextTick();
    await flushPromises();
    expect(wrapper.find(".cm-placeholder").text()).toBe("services:");

    syncComposerLocale("vi");
    await nextTick();
    await flushPromises();

    expect(wrapper.find(".cm-placeholder").text()).toBe(
      "# Dán tài liệu compose tại đây\nservices:",
    );
    wrapper.unmount();
  });

  it("marks the content element invalid and links the visible error text", async () => {
    const wrapper = mount(CodeEditor, {
      props: {
        modelValue: "",
        preset: "yaml",
        ariaLabel: "Compose file",
        invalid: true,
        error: "Enter a compose document with at least one service.",
      },
      global: { plugins: [i18n] },
    });
    await vi.waitFor(() => {
      expect(wrapper.find(".cm-content").exists()).toBe(true);
    });

    const error = wrapper.find(".code-editor-error");
    expect(error.text()).toBe("Enter a compose document with at least one service.");
    expect(wrapper.find(".cm-content").attributes("aria-invalid")).toBe("true");
    expect(wrapper.find(".cm-content").attributes("aria-describedby")).toBe(
      error.attributes("id"),
    );

    await wrapper.setProps({ invalid: false });
    expect(wrapper.find(".cm-content").attributes("aria-invalid")).toBeUndefined();
    expect(wrapper.find(".cm-content").attributes("aria-describedby")).toBeUndefined();
    expect(wrapper.find(".code-editor-error").exists()).toBe(false);
    wrapper.unmount();
  });
});
