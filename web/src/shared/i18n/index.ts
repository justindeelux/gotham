/**
 * Shared i18n runtime: one global vue-i18n composer (Composition API,
 * English fallback), synchronous feature-catalog discovery, Naive UI
 * locale mapping, and the validation presentation path.
 *
 * Sibling feature workers add `features/<module>/locales/{en,vi}.ts`
 * exporting a default message dict; initI18n discovers and registers each
 * file under its module namespace without touching this aggregator.
 */
import type { NDateLocale, NLocale } from "naive-ui";
import { dateEnGB, dateViVN, enGB, viVN } from "naive-ui";
import { computed } from "vue";
import type { App } from "vue";
import { createI18n } from "vue-i18n";

import { registerNamespace } from "./catalog";
import {
  activeLocale,
  applyDocumentLanguage,
  fallbackLocale,
  getLocaleStorage,
  onLocaleChange,
  resolveInitialLocale,
  startLocaleSync,
} from "./locale";
import type { Locale } from "./locale";
import en from "./locales/en";
import vi from "./locales/vi";

export type { Locale };
export {
  activeLocale,
  applyDocumentLanguage,
  fallbackLocale,
  getLocaleStorage,
  handleStorageEvent,
  isSupportedLocale,
  localeStorageKey,
  localeTag,
  onLocaleChange,
  readStoredLocale,
  resetLocaleState,
  resolveInitialLocale,
  setLocale,
} from "./locale";
export { staleChunkCopy } from "./staleFallback";

/** Feature catalog modules discovered synchronously at startup. */
interface FeatureCatalogModule {
  default: Record<string, unknown>;
}

/**
 * loadFeatureCatalogs registers every discovered feature locale file under
 * its module namespace (`features/<module>/locales/en.ts` -> `module`).
 * A namespace already present keeps its first registration, so the shared
 * `common`/`validation`/`language`/`time` roots cannot be shadowed.
 */
export function loadFeatureCatalogs(
  tree: Record<string, unknown>,
  modules: Record<string, FeatureCatalogModule>,
): void {
  for (const [path, module] of Object.entries(modules)) {
    const namespace = /features\/([^/]+)\/locales\/[a-z]+\.ts$/.exec(path)?.[1];
    if (!namespace || !module?.default || namespace in tree) {
      continue;
    }
    registerNamespace(tree, namespace, module.default);
  }
}

const enMessages = { ...en };
const viMessages: typeof enMessages = { ...vi };

/**
 * discoverFeatureCatalogs returns the Vite-discovered feature locale files.
 * It runs inside initI18n (never at module top level) so Node harnesses that
 * bundle this module with plain esbuild — which has no import.meta.glob —
 * can import the composer and message helpers without executing discovery.
 * Literal patterns keep this statically analyzable for Vite; sibling workers
 * add files only.
 */
function discoverFeatureCatalogs(): {
  en: Record<string, FeatureCatalogModule>;
  vi: Record<string, FeatureCatalogModule>;
} {
  return {
    en: import.meta.glob<FeatureCatalogModule>(
      "@/features/*/locales/en.ts",
      { eager: true },
    ) as Record<string, FeatureCatalogModule>,
    vi: import.meta.glob<FeatureCatalogModule>(
      "@/features/*/locales/vi.ts",
      { eager: true },
    ) as Record<string, FeatureCatalogModule>,
  };
}

/**
 * registerDiscoveredCatalogs merges every feature locale file into the
 * composer under its module namespace. Missing-side files fall back to
 * English through the global fallbackLocale.
 */
export function registerDiscoveredCatalogs(): void {
  const discovered = discoverFeatureCatalogs();
  const enTree: Record<string, unknown> = {};
  const viTree: Record<string, unknown> = {};
  loadFeatureCatalogs(enTree, discovered.en);
  loadFeatureCatalogs(viTree, discovered.vi);
  i18n.global.mergeLocaleMessage("en", enTree);
  i18n.global.mergeLocaleMessage("vi", viTree);
}

/** i18n is the single global composer; components use explicit useI18n. */
export const i18n = createI18n({
  legacy: false,
  locale: activeLocale.value,
  fallbackLocale,
  messages: { en: enMessages, vi: viMessages },
});

/** syncComposerLocale moves the composer onto an activated locale. */
export function syncComposerLocale(locale: Locale): void {
  if (i18n.global.locale.value !== locale) {
    i18n.global.locale.value = locale;
  }
}

onLocaleChange(syncComposerLocale);

/**
 * initI18n registers feature catalogs, applies the stored preference before
 * first mount and starts cross-tab synchronization. Called once from main.ts.
 * Catalogs still load synchronously at startup, so fallback and stale-chunk
 * recovery never depend on a lazy chunk.
 */
export function initI18n(): void {
  registerDiscoveredCatalogs();
  const initial = resolveInitialLocale(getLocaleStorage());
  activeLocale.value = initial;
  syncComposerLocale(initial);
  applyDocumentLanguage(initial);
  startLocaleSync();
}

/**
 * installI18n registers the composer on the app. Called once from main.ts
 * before mount; initI18n applies the stored preference first.
 */
export function installI18n(app: App): void {
  initI18n();
  syncComposerLocale(activeLocale.value);
  app.use(i18n);
}

/** naiveLocale is the Naive UI component locale for the active language. */
export const naiveLocale = computed<NLocale>(() =>
  activeLocale.value === "vi" ? viVN : enGB,
);

/** naiveDateLocale is the Naive UI date locale for the active language. */
export const naiveDateLocale = computed<NDateLocale>(() =>
  activeLocale.value === "vi" ? dateViVN : dateEnGB,
);

/**
 * Known zod/Naive default English strings with curated localized fallbacks.
 * Only these exact strings map; everything else passes through untouched so
 * legacy feature messages and provider diagnostics keep byte-identical text.
 */
const defaultMessageKeys: Array<[string, string]> = [
  ["Invalid value", "validation.invalid"],
  ["Required", "validation.required"],
];

/**
 * resolveValidationMessage maps one stored schema message to display text
 * at invocation time (so a language switch refreshes visible feedback):
 * a registered message key resolves through the composer with English
 * fallback, a known zod default maps to its curated fallback, and any
 * legacy English or provider-supplied string passes through unchanged.
 */
export function resolveValidationMessage(message: string): string {
  const composer = i18n.global;
  if (typeof message === "string" && message.includes(".") && composer.te(message)) {
    return String(composer.t(message));
  }
  for (const [fallback, key] of defaultMessageKeys) {
    if (message === fallback) {
      return String(composer.t(key));
    }
  }
  return message;
}
