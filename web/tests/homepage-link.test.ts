import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { createI18n } from "vue-i18n";

import HomepageLink from "@/shared/components/HomepageLink.vue";
import en from "@/shared/i18n/locales/en";
import vi from "@/shared/i18n/locales/vi";

function mountLink(locale: "en" | "vi", props: { href?: string; name?: string }) {
  const i18n = createI18n({ legacy: false, locale, messages: { en, vi } });
  return mount(HomepageLink, { props, global: { plugins: [i18n] } });
}

describe("HomepageLink", () => {
  it("opens the homepage in a new tab", () => {
    const wrapper = mountLink("en", { href: "https://n8n.io", name: "n8n" });
    const link = wrapper.get("a");
    expect(link.attributes("href")).toBe("https://n8n.io");
    expect(link.attributes("target")).toBe("_blank");
    expect(link.attributes("rel")).toBe("noopener noreferrer");
    expect(link.text()).toContain("Open website");
    expect(link.attributes("aria-label")).toBe("Open website: n8n");
  });

  it("is hidden without a homepage", () => {
    const wrapper = mountLink("en", {});
    expect(wrapper.find("a").exists()).toBe(false);
  });

  it("renders the Vietnamese label", () => {
    const wrapper = mountLink("vi", { href: "https://redis.io", name: "Redis" });
    expect(wrapper.get("a").text()).toContain("Mở trang web");
  });
});
