// Mounted provider: Naive locale and HTML language follow the UI locale.
/* global document: readonly, HTMLInputElement: readonly */
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
  const mountSelector = () =>
    mount(LanguageSelect, {
      global: { plugins: [i18n] },
      attachTo: document.body,
    });

  it("offers both autonyms and writes the choice through the setter", async () => {
    const wrapper = mountSelector();
    try {
      const labels = wrapper
        .findAll('input[type="radio"]')
        .map((input) => input.element.closest("label")?.textContent?.trim());
      expect(labels).toEqual(["English", "Tiếng Việt"]);
      expect(wrapper.text()).toContain("English");
      setLocale("vi", null);
      await wrapper.vm.$nextTick();
      expect(document.documentElement.lang).toBe("vi");
    } finally {
      wrapper.unmount();
    }
  });

  it("exposes a labeled radiogroup with native named checked radios", async () => {
    const wrapper = mountSelector();
    try {
      const group = wrapper.find('[role="radiogroup"]');
      expect(group.exists()).toBe(true);
      expect(group.attributes("aria-label")).toBe("Language");
      const radios = wrapper.findAll('input[type="radio"]');
      expect(radios).toHaveLength(2);
      const nameOf = (index: number): string =>
        radios[index].element.closest("label")?.textContent?.trim() ?? "";
      expect(nameOf(0)).toBe("English");
      expect(nameOf(1)).toBe("Tiếng Việt");
      const checked = (): string =>
        radios.find((radio) => (radio.element as HTMLInputElement).checked)?.attributes("value") ?? "";
      expect(checked()).toBe("en");
      setLocale("vi", null);
      await wrapper.vm.$nextTick();
      expect(group.attributes("aria-label")).toBe("Ngôn ngữ");
      expect(checked()).toBe("vi");
      (radios[0].element as HTMLInputElement).focus();
      expect(document.activeElement).toBe(radios[0].element);
    } finally {
      wrapper.unmount();
    }
  });
});
