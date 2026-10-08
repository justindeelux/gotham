/**
 * cardMark derives the card mark from the template name. The API `icon` key
 * is carried as `data-icon` for traceability but not rendered as a glyph: the
 * app's stroke icon set has no brand glyphs, and the mockup's own gallery uses
 * the letter mark (`.tpl-mark`). Shared by the template gallery and the
 * Add-resource picker so one template shows the same mark on both pages.
 */
export function cardMark(name: string): string {
  const first = name.trim()[0];
  return first ? first.toUpperCase() : "?";
}
