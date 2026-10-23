/* global document:readonly, KeyboardEvent:readonly */
import { mount } from "@vue/test-utils";
import { flushPromises } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import EditorSearchBar from "@/features/files/components/EditorSearchBar.vue";
import "@/features/files/locales/editorMessages";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

afterEach(() => {
  document.body.innerHTML = "";
});

function mountBar(overrides: Record<string, unknown> = {}) {
  return mount(EditorSearchBar, {
    attachTo: document.body,
    props: {
      show: true,
      expanded: false,
      canReplace: true,
      query: "one",
      replacement: "",
      caseSensitive: false,
      wholeWord: false,
      regexp: false,
      patternError: null,
      total: 12,
      capped: false,
      current: 3,
      focusReplace: false,
      ...overrides,
    },
    global: { plugins: [i18n] },
  });
}

function findButton(wrapper: ReturnType<typeof mountBar>, name: string) {
  const button = wrapper.find(`button[aria-label="${name}"]`);
  expect(button.exists(), name).toBe(true);
  return button;
}

describe("EditorSearchBar", () => {
  it("shows the match counter in a live region and focuses the query", async () => {
    const wrapper = mountBar();
    await flushPromises();

    const counter = wrapper.find(".ed-find-count");
    expect(counter.attributes("aria-live")).toBe("polite");
    expect(counter.text()).toBe("3 of 12");
    expect(document.activeElement).toBe(wrapper.findAll("input")[0]?.element);
    wrapper.unmount();
  });

  it("says No results when the query matches nothing", async () => {
    const wrapper = mountBar({ total: 0, current: 0 });
    expect(wrapper.find(".ed-find-count").text()).toBe("No results");
    wrapper.unmount();
  });

  it("degrades to a capped counter instead of listing a huge count", async () => {
    const wrapper = mountBar({ total: 1000, capped: true });
    expect(wrapper.find(".ed-find-count").text()).toBe("1000+ results");
    wrapper.unmount();
  });

  it("walks the matches with Enter and reverses with Shift+Enter", async () => {
    const wrapper = mountBar();
    const input = wrapper.findAll("input")[0]!;

    await input.trigger("keydown", { key: "Enter" });
    await input.trigger("keydown", { key: "Enter", shiftKey: true });

    expect(wrapper.emitted("next")).toHaveLength(1);
    expect(wrapper.emitted("previous")).toHaveLength(1);
    wrapper.unmount();
  });

  it("closes on Escape without letting the window see the key", async () => {
    const windowKeydown = vi.fn();
    document.addEventListener("keydown", windowKeydown);
    const wrapper = mountBar();
    const event = new KeyboardEvent("keydown", {
      key: "Escape",
      bubbles: true,
      cancelable: true,
    });

    wrapper.findAll("input")[0]!.element.dispatchEvent(event);
    await flushPromises();

    expect(wrapper.emitted("close")).toHaveLength(1);
    expect(event.defaultPrevented).toBe(true);
    expect(windowKeydown).not.toHaveBeenCalled();
    document.removeEventListener("keydown", windowKeydown);
    wrapper.unmount();
  });

  it("reports every toggle as pressed and emits its change", async () => {
    const wrapper = mountBar({ caseSensitive: true });
    expect(findButton(wrapper, "Match case").attributes("aria-pressed")).toBe("true");
    expect(findButton(wrapper, "Whole word").attributes("aria-pressed")).toBe("false");
    expect(findButton(wrapper, "Regular expression").attributes("aria-pressed")).toBe(
      "false",
    );

    await findButton(wrapper, "Whole word").trigger("click");
    expect(wrapper.emitted("toggle:word")).toHaveLength(1);
    wrapper.unmount();
  });

  it("shows an inline alert for an invalid regex", () => {
    const wrapper = mountBar({
      query: "(",
      regexp: true,
      patternError: "syntax",
    });

    const alert = wrapper.find('[role="alert"]');
    expect(alert.text()).toBe("Invalid regular expression");
    const input = wrapper.findAll("input")[0]!;
    expect(input.attributes("aria-invalid")).toBe("true");
    expect(input.attributes("aria-describedby")).toBe(alert.attributes("id"));
    wrapper.unmount();
  });

  it("refuses a catastrophic pattern with its own inline alert", () => {
    const wrapper = mountBar({
      query: "(a+)+$",
      regexp: true,
      patternError: "complexity",
    });

    expect(wrapper.find('[role="alert"]').text()).toBe(
      "Pattern too complex to search safely",
    );
    expect(wrapper.findAll("input")[0]!.attributes("aria-invalid")).toBe("true");
    wrapper.unmount();
  });

  it("disables replace on a read-only tab but keeps find usable", async () => {
    const wrapper = mountBar({ expanded: true, canReplace: false });
    const inputs = wrapper.findAll("input");
    expect(inputs).toHaveLength(2);
    expect(inputs[1]!.attributes("disabled")).toBeDefined();

    const replace = wrapper.findAll("button").filter((button) => button.text() === "Replace");
    const replaceAll = wrapper
      .findAll("button")
      .filter((button) => button.text() === "Replace all");
    expect(replace[0]!.attributes("disabled")).toBeDefined();
    expect(replaceAll[0]!.attributes("disabled")).toBeDefined();
    wrapper.unmount();
  });

  it("focuses the replacement field when replacing was requested", async () => {
    const wrapper = mountBar({ expanded: true, focusReplace: true });
    await flushPromises();
    expect(document.activeElement).toBe(wrapper.findAll("input")[1]?.element);
    wrapper.unmount();
  });

  it("replaces one match on Enter and all of them with Mod+Enter", async () => {
    const wrapper = mountBar({ expanded: true });
    const replace = wrapper.findAll("input")[1]!;

    await replace.trigger("keydown", { key: "Enter" });
    await replace.trigger("keydown", { key: "Enter", metaKey: true });

    expect(wrapper.emitted("replace")).toHaveLength(1);
    expect(wrapper.emitted("replace-all")).toHaveLength(1);
    wrapper.unmount();
  });
});
