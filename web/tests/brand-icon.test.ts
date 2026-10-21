import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import BrandIcon from "@/features/add-resource/components/BrandIcon.vue";
import { brandFor, templateBrand } from "@/features/add-resource/utils/brands";
import { ENGINES } from "@/features/databases/utils/databaseEngines";

/** Every vendored glyph, keyed by its assets/brands file name. */
const brandFiles = import.meta.glob(
  "../src/features/add-resource/assets/brands/*.svg",
  { eager: true },
);

describe("BrandIcon", () => {
  it("renders the vendored glyph for a known slug", () => {
    const wrapper = mount(BrandIcon, {
      props: { logo: "github", letters: "GH", color: "#24292f" },
    });
    const glyph = wrapper.find(".brand-icon__glyph");
    expect(glyph.exists()).toBe(true);
    expect(wrapper.find(".brand-icon__letters").exists()).toBe(false);
    // The URL rides one CSS variable the scoped mask rules consume.
    expect(glyph.attributes("style")).toContain("--brand-icon-logo");
  });

  it("falls back to letters when the slug has no vendored file", () => {
    const wrapper = mount(BrandIcon, {
      props: { logo: "future-brand", letters: "FB", color: "#5865f2" },
    });
    expect(wrapper.find(".brand-icon__glyph").exists()).toBe(false);
    const letters = wrapper.find(".brand-icon__letters");
    expect(letters.exists()).toBe(true);
    expect(letters.text()).toBe("FB");
  });

  it("is decorative for assistive technology", () => {
    const wrapper = mount(BrandIcon, {
      props: { logo: "git", letters: "G", color: "#f05032" },
    });
    expect(wrapper.attributes("aria-hidden")).toBe("true");
    expect(wrapper.classes()).toContain("brand-icon--md");
  });

  it("ships a vendored file for every mapped slug", () => {
    const keys = [
      "git_public",
      "git_private",
      "github_app",
      "gitlab_app",
      "dockerfile",
      "image",
      "compose",
      ...ENGINES.map((engine) => engine.value),
    ];
    const logos = keys.map((key) => brandFor(key, "Resource").logo);
    for (const icon of ["wordpress", "nextcloud", "n8n", "uptime-kuma"]) {
      logos.push(templateBrand(icon, icon).logo);
    }
    expect(logos.every((logo) => logo !== null)).toBe(true);
    for (const logo of logos) {
      expect(
        brandFiles[`../src/features/add-resource/assets/brands/${logo}.svg`],
        `missing vendored glyph for ${logo}`,
      ).toBeDefined();
    }
  });
});
