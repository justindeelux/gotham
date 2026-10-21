import { cardMark } from "@/features/templates/utils/templateMark";

/**
 * Brand identity for resource avatars (JUS-274). Every mark is a vendored
 * Simple Icons monochrome glyph (see `assets/brands`) on the brand hue, so one
 * mapping drives the Add-resource picker, application detail and the
 * environment list. Unknown keys keep the deterministic initials fallback
 * instead of rendering a broken image. Inline assets only (no CDN).
 */

export interface BrandMark {
  /** Vendored Simple Icons slug, null when the key has no brand glyph. */
  logo: string | null;
  /** Letter fallback; always set so an unknown key never renders blank. */
  letters: string;
  /** Avatar background hue. The glyph itself stays white. */
  color: string;
}

const BRAND_ACCENT = "#5865f2";

/** Frame sizes of a resource avatar; the edge lengths are design tokens. */
export type BrandIconSize = "sm" | "md" | "lg";

/** Template slug (the API `icon` key) to its brand glyph and hue. */
const TEMPLATE_BRANDS: Record<string, { logo: string; color: string }> = {
  wordpress: { logo: "wordpress", color: "#21759b" },
  nextcloud: { logo: "nextcloud", color: "#0082c9" },
  n8n: { logo: "n8n", color: "#ea4b71" },
  "uptime-kuma": { logo: "uptimekuma", color: "#369b6b" },
};

/**
 * Source values and database engines. The Docker source variants share the
 * Docker glyph; their labels keep them distinguishable in the picker.
 */
const MARKS: Record<string, { logo: string | null; color: string }> = {
  git_public: { logo: "git", color: "#f05032" },
  git_private: { logo: "git", color: "#f05032" },
  // GitHub's own dark hue (#24292f) is 1.06:1 against --surface, so the frame
  // disappears on the app's dark surfaces; this lighter GitHub slate reads
  // (3.0:1) while the glyph stays white.
  github_app: { logo: "github", color: "#6e7681" },
  gitlab_app: { logo: "gitlab", color: "#fc6d26" },
  dockerfile: { logo: "docker", color: "#2496ed" },
  image: { logo: "docker", color: "#2496ed" },
  compose: { logo: "docker", color: "#2496ed" },
  postgres: { logo: "postgresql", color: "#4169e1" },
  mysql: { logo: "mysql", color: "#4479a1" },
  mariadb: { logo: "mariadb", color: "#c0765a" },
  mongodb: { logo: "mongodb", color: "#47a248" },
  redis: { logo: "redis", color: "#ff4438" },
};

/** initialsOf derives a one- or two-letter mark from a display name. */
export function initialsOf(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) {
    return "?";
  }
  if (words.length === 1) {
    return words[0].slice(0, 2).toUpperCase();
  }
  return (words[0][0] + words[1][0]).toUpperCase();
}

/**
 * templateBrand resolves the mark for one template catalog entry: its brand
 * glyph on the brand hue, or the shared gallery initials for unknown slugs.
 */
export function templateBrand(icon: string, name: string): BrandMark {
  const brand = TEMPLATE_BRANDS[icon];
  return {
    logo: brand?.logo ?? null,
    letters: cardMark(name),
    color: brand?.color ?? BRAND_ACCENT,
  };
}

/**
 * brandFor resolves the mark for a source value, an engine value or the
 * legacy application card, falling back to initials on the accent hue for
 * unknown keys.
 */
export function brandFor(key: string, name: string): BrandMark {
  const brand = MARKS[key];
  return {
    logo: brand?.logo ?? null,
    letters: initialsOf(name),
    color: brand?.color ?? BRAND_ACCENT,
  };
}

/**
 * applicationBrand resolves an application's identity from its canonical
 * source metadata. A legacy row without `source_type` derives the type from
 * its provider, exactly like the control plane's NormalizeSourceType.
 */
export function applicationBrand(
  sourceType: string,
  provider: string,
  name: string,
): BrandMark {
  if (sourceType !== "") {
    return brandFor(sourceType, name);
  }
  if (provider === "github") {
    return brandFor("github_app", name);
  }
  if (provider === "gitlab") {
    return brandFor("gitlab_app", name);
  }
  return brandFor("git_public", name);
}
