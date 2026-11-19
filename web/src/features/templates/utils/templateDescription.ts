import catalog from "@/features/templates/locales/catalog.vi";
import type { TemplateSummary } from "@/features/templates/api/templates";
import { activeLocale } from "@/shared/i18n";

// English is the source text from the template API; only the add-resource
// chunk loads the Vietnamese catalog, so the app shell stays small.
export function templateDescription(template: TemplateSummary): string {
  if (activeLocale.value === "vi") {
    const translated = catalog[template.slug];
    if (translated) {
      return translated;
    }
  }
  return template.description;
}
