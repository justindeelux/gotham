/**
 * Brand marks for the Add-resource picker. Inline SVG only (no CDN): each
 * mark is a rounded square in the brand hue with a short letter mark, in
 * the same visual language as the `.tpl-mark` avatars in the design
 * mockups. Unknown keys fall back to the accent hue with initials derived
 * from the display name, so a new template or engine needs no UI change.
 */

export interface BrandMark {
  letters: string;
  color: string;
}

const BRAND_ACCENT = "#5865f2";

const MARKS: Record<string, BrandMark> = {
  application: { letters: "</>", color: "#5865f2" },
  wordpress: { letters: "W", color: "#21759b" },
  nextcloud: { letters: "NC", color: "#0082c9" },
  n8n: { letters: "n8", color: "#ea4b71" },
  "uptime-kuma": { letters: "UK", color: "#3f9e4d" },
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
 * brandFor resolves the mark for a template icon key or engine value,
 * falling back to initials on the accent hue for unknown keys.
 */
export function brandFor(key: string, name: string): BrandMark {
  return MARKS[key] ?? { letters: initialsOf(name), color: BRAND_ACCENT };
}
