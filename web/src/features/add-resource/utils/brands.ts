import { cardMark } from "@/features/templates/utils/templateMark";

/**
 * Brand colors for the Add-resource picker. Inline SVG only (no CDN): each
 * mark is a rounded square in the brand hue with a letter mark, in the same
 * visual language as the `.tpl-mark` avatars in the design mockups.
 * Template letters come from the shared `cardMark` so one template shows
 * the same mark in the gallery and on this picker; unknown keys fall back
 * to the accent hue, so a new template or engine needs no UI change.
 */

export interface BrandMark {
  letters: string;
  color: string;
}

const BRAND_ACCENT = "#5865f2";

/** Template slug (the API `icon` key) to brand hue. */
const TEMPLATE_COLORS: Record<string, string> = {
  wordpress: "#21759b",
  nextcloud: "#0082c9",
  n8n: "#ea4b71",
  "uptime-kuma": "#3f9e4d",
};

const MARKS: Record<string, BrandMark> = {
  application: { letters: "</>", color: "#5865f2" },
  postgres: { letters: "PG", color: "#336791" },
  mysql: { letters: "MY", color: "#00758f" },
  mariadb: { letters: "MD", color: "#c0765a" },
  mongodb: { letters: "MG", color: "#47a248" },
  redis: { letters: "RD", color: "#dc382d" },
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
 * templateBrand resolves the mark for one template catalog entry: shared
 * gallery letters on the brand hue.
 */
export function templateBrand(icon: string, name: string): BrandMark {
  return { letters: cardMark(name), color: TEMPLATE_COLORS[icon] ?? BRAND_ACCENT };
}

/**
 * brandFor resolves the mark for an engine value or the application card,
 * falling back to initials on the accent hue for unknown keys.
 */
export function brandFor(key: string, name: string): BrandMark {
  return MARKS[key] ?? { letters: initialsOf(name), color: BRAND_ACCENT };
}
