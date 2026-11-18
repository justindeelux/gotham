import { readFileSync } from "node:fs";

import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

import BrandIcon from "@/features/add-resource/components/BrandIcon.vue";
import { createAssetResolver } from "@/features/add-resource/utils/brandAssets";
import { brandFor, templateBrand } from "@/features/add-resource/utils/brands";
import { ENGINES } from "@/features/databases/utils/databaseEngines";

const tokensCss = readFileSync("src/shared/styles/tokens.css", "utf8");

const glyphFiles = import.meta.glob(
  "../src/features/add-resource/assets/brands/*.svg",
  { eager: true },
);

const iconFiles = import.meta.glob(
  "../src/features/add-resource/assets/icons/**/*.{svg,png,webp,jpeg,jpg,ico}",
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

async function mountIcon(
  props: { letters: string; color: string; logo?: string | null },
  expected: string,
) {
  const wrapper = mount(BrandIcon, { props });
  await vi.waitFor(() => expect(wrapper.find(expected).exists()).toBe(true));
  return wrapper;
}

describe("BrandIcon", () => {
  it("renders the vendored glyph for a source mark", async () => {
    const wrapper = await mountIcon(
      brandFor("github_app", "GitHub"),
      ".brand-icon__glyph",
    );
    const glyph = wrapper.find(".brand-icon__glyph");
    expect(glyph.exists()).toBe(true);
    expect(wrapper.find(".brand-icon__image").exists()).toBe(false);
    expect(glyph.attributes("style")).toContain("--brand-icon-logo");
  });

  it("renders the bundled catalog icon for a template slug", async () => {
    const wrapper = await mountIcon(
      templateBrand("nextcloud", "Nextcloud"),
      ".brand-icon__image",
    );
    const image = wrapper.find(".brand-icon__image");
    expect(image.exists()).toBe(true);
    expect(image.attributes("src")).toContain("nextcloud");
    expect(wrapper.find(".brand-icon__glyph").exists()).toBe(false);
    expect(wrapper.classes()).toContain("brand-icon--image");
  });

  it("prefers the service icon over a source glyph with the same name", async () => {
    const template = await mountIcon(
      templateBrand("gitlab", "GitLab"),
      ".brand-icon__image",
    );
    expect(template.find(".brand-icon__image").exists()).toBe(true);
    expect(template.find(".brand-icon__glyph").exists()).toBe(false);
    expect(template.classes()).toContain("brand-icon--image");

    const source = await mountIcon(brandFor("gitlab_app", "GitLab"), ".brand-icon__glyph");
    expect(source.find(".brand-icon__glyph").exists()).toBe(true);
    expect(source.find(".brand-icon__image").exists()).toBe(false);
  });

  it("evicts a failed import and falls back to the default icon", async () => {
    let attempts = 0;
    const icons = new Map<string, () => Promise<string>>([
      ["default", async () => "default.webp"],
      [
        "flaky",
        async () => {
          attempts += 1;
          if (attempts === 1) {
            throw new Error("stale chunk");
          }
          return "flaky.svg";
        },
      ],
    ]);
    const { assetFor } = createAssetResolver(icons, new Map());

    await expect(assetFor("flaky", false)).resolves.toEqual({
      url: "default.webp",
      tinted: false,
    });
    await expect(assetFor("flaky", false)).resolves.toEqual({
      url: "flaky.svg",
      tinted: false,
    });
    expect(attempts).toBe(2);
  });

  it("shows the default icon when the slug has no vendored file", async () => {
    const wrapper = await mountIcon(
      { logo: "future-brand", letters: "FB", color: "#5865f2" },
      ".brand-icon__image",
    );
    const image = wrapper.find(".brand-icon__image");
    expect(image.exists()).toBe(true);
    expect(image.attributes("src")).toContain("default");
    expect(wrapper.find(".brand-icon__letters").exists()).toBe(false);
  });

  it("falls back to letters when there is no logo key", async () => {
    const wrapper = await mountIcon(
      { logo: null, letters: "FB", color: "#5865f2" },
      ".brand-icon__letters",
    );
    expect(wrapper.find(".brand-icon__image").exists()).toBe(false);
    expect(wrapper.find(".brand-icon__glyph").exists()).toBe(false);
    const letters = wrapper.find(".brand-icon__letters");
    expect(letters.exists()).toBe(true);
    expect(letters.text()).toBe("FB");
  });

  it("is decorative for assistive technology", async () => {
    const wrapper = await mountIcon(brandFor("git_public", "Repository"), ".brand-icon__glyph");
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

  it("ships a vendored glyph for every source and an icon for every engine", () => {
    const glyphKeys = [
      "git_public",
      "git_private",
      "github_app",
      "gitlab_app",
      "dockerfile",
      "image",
      "compose",
    ];
    for (const key of glyphKeys) {
      const logo = brandFor(key, "Resource").logo;
      expect(
        glyphFiles[`../src/features/add-resource/assets/brands/${logo}.svg`],
        `missing vendored glyph for ${logo}`,
      ).toBeDefined();
    }
    for (const engine of ENGINES) {
      const logo = brandFor(engine.value, engine.label).logo;
      expect(
        Object.keys(iconFiles).some((path) =>
          new RegExp(`/icons/databases/${logo}\\.[a-z]+$`).test(path),
        ),
        `missing catalog icon for ${logo}`,
      ).toBe(true);
    }
    for (const slug of ["wordpress", "nextcloud", "n8n", "uptime-kuma"]) {
      expect(
        Object.keys(iconFiles).some((path) =>
          new RegExp(`/icons/services/${slug}\\.[a-z]+$`).test(path),
        ),
        `missing catalog icon for ${slug}`,
      ).toBe(true);
    }
  });
});
