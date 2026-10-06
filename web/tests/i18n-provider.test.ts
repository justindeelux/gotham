// Mounted provider: Naive locale and HTML language follow the UI locale.
/* global document: readonly */
import { mount } from "@vue/test-utils";
import { dateEnGB, dateViVN, enGB, NConfigProvider, viVN } from "naive-ui";
import { beforeEach, describe, expect, it } from "vitest";
import { defineComponent, h } from "vue";

import {
  i18n,
  naiveDateLocale,
  naiveLocale,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";
import LanguageSelect from "@/shared/ui/LanguageSelect.vue";

const ProviderHarness = defineComponent({
  name: "ProviderHarness",
  setup() {
    return () =>
      h(NConfigProvider, {
        locale: naiveLocale.value,
        dateLocale: naiveDateLocale.value,
      });
  },
});

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

describe("mounted provider", () => {
  it("renders the English Naive locale and HTML language by default", () => {
    const wrapper = mount(ProviderHarness);
    const provider = wrapper.findComponent(NConfigProvider);
    expect(provider.props("locale")).toBe(enGB);
    expect(provider.props("dateLocale")).toBe(dateEnGB);
    expect(document.documentElement.lang).toBe("en");
    wrapper.unmount();
  });

  it("switches Naive locale and HTML language to Vietnamese", async () => {
    const wrapper = mount(ProviderHarness);
    setLocale("vi", null);
    await wrapper.vm.$nextTick();
    const provider = wrapper.findComponent(NConfigProvider);
    expect(provider.props("locale")).toBe(viVN);
    expect(provider.props("dateLocale")).toBe(dateViVN);
    expect(document.documentElement.lang).toBe("vi");
    wrapper.unmount();
  });
});

describe("LanguageSelect", () => {
  it("offers both autonyms and writes the choice through the setter", async () => {
    const wrapper = mount(LanguageSelect, { global: { plugins: [i18n] } });
    const select = wrapper.findComponent({ name: "Select" });
    const options = select.props("options") as Array<{
      value: string;
      label: string;
    }>;
    expect(options.map((option) => option.label)).toEqual([
      "English",
      "Tiếng Việt",
    ]);
    expect(wrapper.text()).toContain("English");
    setLocale("vi", null);
    await wrapper.vm.$nextTick();
    expect(document.documentElement.lang).toBe("vi");
    wrapper.unmount();
  });

  it("localizes the accessible name with the current locale", async () => {
    const wrapper = mount(LanguageSelect, { global: { plugins: [i18n] } });
    // Naive forwards extra attrs to the widget root only (Select.mjs renders
    // a plain div; internal Selection.mjs owns the focusable trigger and
    // exposes no label/role props), so assert both halves: the root carries
    // the localized widget name, and the real focusable element names itself
    // from its localized content.
    const focusable = () => wrapper.find('[tabindex="0"]');
    const rootName = () => wrapper.find(".n-select").attributes("aria-label");
    expect(rootName()).toBe("Language");
    expect(focusable().exists()).toBe(true);
    expect(focusable().text()).toBe("English");
    setLocale("vi", null);
    await wrapper.vm.$nextTick();
    expect(rootName()).toBe("Ngôn ngữ");
    expect(focusable().text()).toBe("Tiếng Việt");
    wrapper.unmount();
  });
});
