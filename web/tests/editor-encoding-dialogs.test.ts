// Encoding dialogs (JUS-222): the save-encoding picker emits the chosen
// charset, and the unsupported-characters prompt lists them and offers the
// UTF-8 retry. Teleport is stubbed so the Naive modal stays in the wrapper.
/* global HTMLInputElement:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import EditorEncodingDialog from "@/features/files/components/EditorEncodingDialog.vue";
import EditorEncodingUnsupportedDialog from "@/features/files/components/EditorEncodingUnsupportedDialog.vue";
// Side-effect import: merges the lazy editor catalogs under `files.editor`.
import "@/features/files/locales/editorMessages";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

registerDiscoveredCatalogs();
resetLocaleState();
syncComposerLocale("en");

/** buttonWith finds the button whose label contains `text`. */
function buttonWith(
  wrapper: ReturnType<typeof mount>,
  text: string,
): ReturnType<typeof wrapper.findAll>[number] {
  const button = wrapper
    .findAll("button")
    .find((item) => item.text().includes(text));
  if (!button) {
    throw new Error(`button ${text} not found`);
  }
  return button;
}

describe("encoding picker dialog", () => {
  it("lists every charset and emits the picked one", async () => {
    const wrapper = mount(EditorEncodingDialog, {
      props: { show: true, encoding: "gbk" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const labels = wrapper.findAll(".n-radio__label").map((item) => item.text());
    expect(labels).toEqual(["ASCII", "UTF-8", "GBK", "GB2312", "BIG5"]);

    const radios = wrapper.findAll<HTMLInputElement>('input[type="radio"]');
    const utf8 = radios.find((input) => input.element.value === "utf-8")!;
    await utf8.setValue(true);
    await flushPromises();

    expect(wrapper.emitted("select")).toEqual([["utf-8"]]);
    expect(wrapper.emitted("update:show")).toEqual([[false]]);
  });

  it("closes without emitting a change on cancel", async () => {
    const wrapper = mount(EditorEncodingDialog, {
      props: { show: true, encoding: "gbk" },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    await buttonWith(wrapper, "Cancel").trigger("click");
    await flushPromises();

    expect(wrapper.emitted("select")).toBeUndefined();
    expect(wrapper.emitted("update:show")).toEqual([[false]]);
  });
});

describe("unsupported characters dialog", () => {
  it("names the encoding and lists the offending characters", async () => {
    const wrapper = mount(EditorEncodingUnsupportedDialog, {
      props: {
        show: true,
        name: "app.conf",
        encoding: "gb2312",
        characters: ["😀", "é"],
      },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    const text = wrapper.text();
    expect(text).toContain("GB2312");
    expect(text).toContain("app.conf");
    expect(wrapper.findAll(".enc-chars li").map((item) => item.text())).toEqual([
      "😀",
      "é",
    ]);

    await buttonWith(wrapper, "Switch to UTF-8").trigger("click");
    expect(wrapper.emitted("useUtf8")).toHaveLength(1);
  });

  it("omits the character list when only the server detected the refusal", async () => {
    const wrapper = mount(EditorEncodingUnsupportedDialog, {
      props: {
        show: true,
        name: "app.conf",
        encoding: "gbk",
        characters: [],
      },
      global: { plugins: [i18n], stubs: { teleport: true } },
    });
    await flushPromises();

    expect(wrapper.find(".enc-chars").exists()).toBe(false);

    await buttonWith(wrapper, "Cancel").trigger("click");
    expect(wrapper.emitted("useUtf8")).toBeUndefined();
    expect(wrapper.emitted("update:show")).toEqual([[false]]);
  });
});
