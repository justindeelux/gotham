import { ref } from "vue";

/** Locales the SPA can render. English is the default and the fallback. */
export const supportedLocales = ["en", "vi"] as const;

/** Locale is one of the two supported UI languages. */
export type Locale = (typeof supportedLocales)[number];

/** localStorage key holding the explicit language choice. */
export const localeStorageKey = "gotham-locale";

/** Fallback locale used for absent, corrupt and unsupported stored values. */
export const fallbackLocale: Locale = "en";

/** isSupportedLocale narrows an unknown stored value to a valid Locale. */
export function isSupportedLocale(value: unknown): value is Locale {
  return value === "en" || value === "vi";
}

/** readStoredLocale returns the persisted choice, or null when absent. */
export function readStoredLocale(storage: Storage | null): Locale | null {
  if (!storage) {
    return null;
  }
  try {
    const raw = storage.getItem(localeStorageKey);
    return isSupportedLocale(raw) ? raw : null;
  } catch {
    return null;
  }
}

/** resolveInitialLocale reads the stored choice, falling back to English. */
export function resolveInitialLocale(storage: Storage | null): Locale {
  return readStoredLocale(storage) ?? fallbackLocale;
}

/** localeTag maps a UI locale onto its Intl display tag. */
export function localeTag(locale?: Locale | null): string {
  const active = locale ?? activeLocale.value;
  return active === "vi" ? "vi-VN" : "en-GB";
}

/** activeLocale is the in-memory choice; storage failures keep it usable. */
export const activeLocale = ref<Locale>(fallbackLocale);

/** LocaleChangeListener runs on every activated locale change. */
export type LocaleChangeListener = (_next: Locale) => void;

/** localeChangeListeners receive setLocale/tab-sync activations. */
const localeChangeListeners = new Set<LocaleChangeListener>();

/** onLocaleChange subscribes to locale activations; returns unsubscribe. */
export function onLocaleChange(listener: LocaleChangeListener): () => void {
  localeChangeListeners.add(listener);
  return () => {
    localeChangeListeners.delete(listener);
  };
}

/** notifyLocaleChange fans an activation out to subscribers. */
function notifyLocaleChange(locale: Locale): void {
  for (const listener of localeChangeListeners) {
    listener(locale);
  }
}

/** localeSyncStarted guards the single global storage listener. */
let localeSyncStarted = false;

/** applyDocumentLanguage mirrors the locale onto <html lang> (en/vi, LTR). */
export function applyDocumentLanguage(locale: Locale): void {
  if (typeof document !== "undefined") {
    document.documentElement.lang = locale;
  }
}

/**
 * setLocale validates, persists (best-effort) and activates a locale.
 * Unsupported input is ignored. Storage failures keep the in-memory choice.
 */
export function setLocale(locale: unknown, storage?: Storage | null): boolean {
  if (!isSupportedLocale(locale)) {
    return false;
  }
  const store =
    storage === undefined ? getLocaleStorage() : storage;
  if (store) {
    try {
      store.setItem(localeStorageKey, locale);
    } catch {
      // Blocked storage (private mode, quota): keep the in-memory choice.
    }
  }
  activeLocale.value = locale;
  applyDocumentLanguage(locale);
  notifyLocaleChange(locale);
  return true;
}

/** getLocaleStorage returns localStorage when reachable, else null. */
export function getLocaleStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

/**
 * handleStorageEvent applies another tab's valid change: removal (including
 * a full clear(), which arrives with key null) means English, invalid values
 * are ignored. Events from a different storage area are ignored. Never
 * writes back, so no loops.
 */
export function handleStorageEvent(event: {
  key: string | null;
  newValue: string | null;
  storageArea?: Storage | null;
}): void {
  if (event.key !== localeStorageKey && event.key !== null) {
    return;
  }
  if (
    event.storageArea !== undefined &&
    event.storageArea !== null &&
    event.storageArea !== getLocaleStorage()
  ) {
    return;
  }
  if (event.newValue === null) {
    activeLocale.value = fallbackLocale;
    applyDocumentLanguage(fallbackLocale);
    notifyLocaleChange(fallbackLocale);
    return;
  }
  if (isSupportedLocale(event.newValue)) {
    activeLocale.value = event.newValue;
    applyDocumentLanguage(event.newValue);
    notifyLocaleChange(event.newValue);
  }
}

/** localeStorageListener is the named cross-tab handler (removable). */
function localeStorageListener(event: StorageEvent): void {
  handleStorageEvent({
    key: event.key,
    newValue: event.newValue,
    storageArea: event.storageArea ?? undefined,
  });
}

/** startLocaleSync registers the cross-tab listener exactly once. */
export function startLocaleSync(): void {
  if (localeSyncStarted || typeof window === "undefined") {
    return;
  }
  localeSyncStarted = true;
  window.addEventListener("storage", localeStorageListener);
}

/** resetLocaleState restores module state for unit tests only. */
export function resetLocaleState(): void {
  activeLocale.value = fallbackLocale;
  if (typeof window !== "undefined") {
    window.removeEventListener("storage", localeStorageListener);
  }
  localeSyncStarted = false;
  if (typeof document !== "undefined") {
    document.documentElement.lang = fallbackLocale;
  }
}
