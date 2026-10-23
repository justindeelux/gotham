import { readFileSync } from "node:fs";

import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import BrandIcon from "@/features/add-resource/components/BrandIcon.vue";
import { brandFor, templateBrand } from "@/features/add-resource/utils/brands";
import { ENGINES } from "@/features/databases/utils/databaseEngines";

const tokensCss = readFileSync("src/shared/styles/tokens.css", "utf8");

const brandFiles = import.meta.glob(
  "../src/features/add-resource/assets/brands/*.svg",
  { eager: true },
);

function tokenHex(name: string): string {
  const match = new RegExp(`${name}:\\s*(#[0-9a-fA-F]{6})`).exec(tokensCss);
  if (!match) {
    throw new Error(`design token ${name} is missing`);
  }
  return match[1];
}

function luminance(hex: string): number {
  const value = Number.parseInt(hex.slice(1), 16);
  const channel = (raw: number): number => {
    const c = raw / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return (
    0.2126 * channel((value >> 16) & 0xff) +
    0.7152 * channel((value >> 8) & 0xff) +
    0.0722 * channel(value & 0xff)
  );
}

function contrastRatio(a: string, b: string): number {
  const first = luminance(a);
  const second = luminance(b);
  return (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05);
}

describe("BrandIcon", () => {
  it("renders the vendored glyph for a known slug", () => {
    const wrapper = mount(BrandIcon, {
      props: { logo: "github", letters: "GH", color: "#6e7681" },
    });
    const glyph = wrapper.find(".brand-icon__glyph");
    expect(glyph.exists()).toBe(true);
    expect(wrapper.find(".brand-icon__letters").exists()).toBe(false);
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

  it("keeps the GitHub frame readable on every dark surface", () => {
    const frame = brandFor("github_app", "GitHub").color;
    for (const token of ["--surface-warm", "--surface", "--bg"]) {
      expect(
        contrastRatio(frame, tokenHex(token)),
        `${frame} vs ${token}`,
      ).toBeGreaterThanOrEqual(1.5);
    }
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
