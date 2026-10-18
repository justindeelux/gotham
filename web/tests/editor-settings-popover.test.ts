// Editor settings popover (JUS-219 fix round): the controls must stay bound to
// the live settings object after an update, keyboard users must land inside the
// panel and get focus back on Escape, and a row's text must not double-toggle a
// switch.
/* global document:readonly, KeyboardEvent:readonly, HTMLElement:readonly, localStorage:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import EditorSettingsPopover from "@/features/files/components/EditorSettingsPopover.vue";
// Side-effect import: merges the lazy editor catalogs under `files.editor`.
import "@/features/files/locales/editorMessages";
import { useEditorSettingsStore } from "@/features/files/stores/editorSettings";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

beforeEach(() => {
  setActivePinia(createPinia());
  localStorage.clear();
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  document.body.innerHTML = "";
});

/** mountPopover mounts the component with its teleported panel on the body. */
function mountPopover() {
  return mount(EditorSettingsPopover, {
    attachTo: document.body,
    global: { plugins: [i18n] },
  });
}

/** openPopover clicks the trigger and waits for the panel to render. */
async function openPopover(wrapper: ReturnType<typeof mountPopover>): Promise<void> {
  await wrapper.find("button").trigger("click");
  await flushPromises();
}

/** rowFor finds the settings row whose text starts with the given label. */
function rowFor(label: string): HTMLElement {
  const rows = [...document.querySelectorAll<HTMLElement>(".ed-settings-row")];
  const row = rows.find((item) => item.textContent?.trim().startsWith(label));
  if (!row) {
    throw new Error(`settings row not found: ${label}`);
  }
  return row;
}

describe("EditorSettingsPopover", () => {
  it("keeps controls in sync after a change", async () => {
    const wrapper = mountPopover();
    await openPopover(wrapper);

    const control = rowFor("Line numbers").querySelector<HTMLElement>('[role="switch"]');
    expect(control?.getAttribute("aria-checked")).toBe("true");

    control?.click();
    await flushPromises();

    expect(useEditorSettingsStore().settings.lineNumbers).toBe(false);
    expect(
      rowFor("Line numbers")
        .querySelector('[role="switch"]')
        ?.getAttribute("aria-checked"),
    ).toBe("false");
    wrapper.unmount();
  });

  it("moves focus into the panel and returns it on Escape", async () => {
    const wrapper = mountPopover();
    await openPopover(wrapper);

    const panel = document.querySelector<HTMLElement>(".ed-settings");
    expect(panel).not.toBeNull();
    expect(panel?.contains(document.activeElement)).toBe(true);

    const active = document.activeElement;
    expect(active).toBeInstanceOf(HTMLElement);
    active?.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }),
    );
    await flushPromises();

    expect(document.querySelector(".ed-settings")).toBeNull();
    expect(document.activeElement).toBe(wrapper.find("button").element);
    wrapper.unmount();
  });

  it("does not toggle a switch when the row text is clicked", async () => {
    const wrapper = mountPopover();
    await openPopover(wrapper);

    rowFor("Line numbers").querySelector("span")?.click();
    await flushPromises();

    expect(useEditorSettingsStore().settings.lineNumbers).toBe(true);
    wrapper.unmount();
  });
});
