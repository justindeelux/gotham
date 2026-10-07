// Mounted provider: Naive locale and HTML language follow the UI locale.
/* global document: readonly, localStorage: readonly, HTMLElement: readonly */
import { mount } from "@vue/test-utils";
import { dateEnGB, dateViVN, enGB, NConfigProvider, NDropdown, viVN } from "naive-ui";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h } from "vue";

import {
  i18n,
  localeStorageKey,
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
      await wrapper.get("button").trigger("click");
      await vi.waitFor(() => {
        expect(document.querySelectorAll('[role="menuitemradio"]')).toHaveLength(2);
      });
      const items = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitemradio"]'));
      expect(items.map((item) => item.textContent?.trim())).toEqual([
        i18n.global.t("language.names.en"),
        i18n.global.t("language.names.vi"),
      ]);
      expect(items[0].getAttribute("aria-checked")).toBe("true");
      items[1].click();
      await wrapper.vm.$nextTick();
      expect(document.documentElement.lang).toBe("vi");
      expect(localStorage.getItem(localeStorageKey)).toBe("vi");
      expect(wrapper.findComponent(NDropdown).props("value")).toBe("vi");
      expect(wrapper.get("button").attributes("aria-expanded")).toBe("false");
      await wrapper.get("button").trigger("click");
      await vi.waitFor(() => {
        expect(document.querySelector('[role="menuitemradio"][aria-checked="true"]')?.textContent)
          .toBe(i18n.global.t("language.names.vi"));
      });
    } finally {
      wrapper.unmount();
      localStorage.removeItem(localeStorageKey);
    }
  });

  it("exposes a labeled, focusable icon button and follows external locale changes", async () => {
    const wrapper = mountSelector();
    try {
      const button = wrapper.get("button");
      expect(button.attributes("aria-label")).toBe("Language");
      expect(button.attributes("aria-haspopup")).toBe("menu");
      expect(button.attributes("aria-expanded")).toBe("false");
      expect(button.find("svg").exists()).toBe(true);
      expect(wrapper.findComponent(NDropdown).props("value")).toBe("en");
      setLocale("vi", null);
      await wrapper.vm.$nextTick();
      expect(button.attributes("aria-label")).toBe(i18n.global.t("language.label"));
      expect(wrapper.findComponent(NDropdown).props("value")).toBe("vi");
      (button.element as HTMLElement).focus();
      expect(document.activeElement).toBe(button.element);
    } finally {
      wrapper.unmount();
    }
  });
});
