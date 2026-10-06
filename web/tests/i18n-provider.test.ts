// Mounted provider: Naive locale and HTML language follow the UI locale.
/* global document: readonly */
import { mount } from "@vue/test-utils";
import { dateEnGB, dateViVN, enGB, NConfigProvider, viVN } from "naive-ui";
import { beforeEach, describe, expect, it } from "vitest";
import { defineComponent, h } from "vue";

import {
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
    const wrapper = mount(LanguageSelect);
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
});
