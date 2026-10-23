import type { NDateLocale, NLocale } from "naive-ui";
import { dateEnGB, dateViVN, enGB, viVN } from "naive-ui";
import { computed } from "vue";
import type { App } from "vue";
import { createI18n } from "vue-i18n";

import { registerNamespace } from "./catalog";
import {
  activeLocale,
  fallbackLocale,
  getLocaleStorage,
  onLocaleChange,
  resolveInitialLocale,
  setLocale,
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
  startLocaleSync,
} from "./locale";
export { staleChunkCopy } from "./staleFallback";

interface FeatureCatalogModule {
  default: Record<string, unknown>;
}

export function loadFeatureCatalogs(
  tree: Record<string, unknown>,
  modules: Record<string, FeatureCatalogModule>,
  reserved: string[] = [],
): void {
  for (const [path, module] of Object.entries(modules)) {
    const namespace = /features\/([^/]+)\/locales\/[a-z]+\.ts$/.exec(path)?.[1];
    if (!namespace || !module?.default) {
      continue;
    }
    if (reserved.includes(namespace)) {
      throw new Error(`reserved i18n namespace: ${namespace}`);
    }
    registerNamespace(tree, namespace, module.default);
  }
}

const enMessages = { ...en };
const viMessages: typeof enMessages = { ...vi };

const reservedRoots = [...Object.keys(en), ...Object.keys(vi)];

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

export function registerDiscoveredCatalogs(): void {
  const discovered = discoverFeatureCatalogs();
  const enTree: Record<string, unknown> = {};
  const viTree: Record<string, unknown> = {};
  loadFeatureCatalogs(enTree, discovered.en, reservedRoots);
  loadFeatureCatalogs(viTree, discovered.vi, reservedRoots);
  i18n.global.mergeLocaleMessage("en", enTree);
  i18n.global.mergeLocaleMessage("vi", viTree);
}

export const i18n = createI18n({
  legacy: false,
  locale: activeLocale.value,
  fallbackLocale,
  messages: { en: enMessages, vi: viMessages },
});

export function syncComposerLocale(locale: Locale): void {
  if (i18n.global.locale.value !== locale) {
    i18n.global.locale.value = locale;
  }
}

onLocaleChange(syncComposerLocale);

export function initI18n(): void {
  registerDiscoveredCatalogs();
  setLocale(resolveInitialLocale(getLocaleStorage()), getLocaleStorage());
  startLocaleSync();
}

export function installI18n(app: App): void {
  initI18n();
  syncComposerLocale(activeLocale.value);
  app.use(i18n);
}

export const naiveLocale = computed<NLocale>(() =>
  activeLocale.value === "vi" ? viVN : enGB,
);

export const naiveDateLocale = computed<NDateLocale>(() =>
  activeLocale.value === "vi" ? dateViVN : dateEnGB,
);

const defaultMessageKeys: Array<[string, string]> = [
  ["Invalid value", "validation.invalid"],
  ["Required", "validation.required"],
];

export type ValidationMessageParams = Record<string, string | number>;

export function resolveValidationMessage(
  message: string,
  params?: ValidationMessageParams,
): string {
  const composer = i18n.global;
  if (composer.te(message)) {
    return String(composer.t(message, params ?? {}));
  }
  for (const [fallback, key] of defaultMessageKeys) {
    if (message === fallback) {
      return String(composer.t(key));
    }
  }
  return message;
}
