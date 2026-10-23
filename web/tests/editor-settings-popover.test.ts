/* global document:readonly, KeyboardEvent:readonly, HTMLElement:readonly, localStorage:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import EditorSettingsPopover from "@/features/files/components/EditorSettingsPopover.vue";
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

function mountPopover() {
  return mount(EditorSettingsPopover, {
    attachTo: document.body,
    global: { plugins: [i18n] },
  });
}

async function openPopover(wrapper: ReturnType<typeof mountPopover>): Promise<void> {
  await wrapper.find("button").trigger("click");
  await flushPromises();
}

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
