import { ref } from "vue";

export const supportedLocales = ["en", "vi"] as const;

export type Locale = (typeof supportedLocales)[number];

export const localeStorageKey = "gotham-locale";

export const fallbackLocale: Locale = "en";

export function isSupportedLocale(value: unknown): value is Locale {
  return value === "en" || value === "vi";
}

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

export function resolveInitialLocale(storage: Storage | null): Locale {
  return readStoredLocale(storage) ?? fallbackLocale;
}

export function localeTag(locale?: Locale | null): string {
  const active = locale ?? activeLocale.value;
  return active === "vi" ? "vi-VN" : "en-GB";
}

export const activeLocale = ref<Locale>(fallbackLocale);

export type LocaleChangeListener = (_next: Locale) => void;

const localeChangeListeners = new Set<LocaleChangeListener>();

export function onLocaleChange(listener: LocaleChangeListener): () => void {
  localeChangeListeners.add(listener);
  return () => {
    localeChangeListeners.delete(listener);
  };
}

function notifyLocaleChange(locale: Locale): void {
  for (const listener of localeChangeListeners) {
    listener(locale);
  }
}

let localeSyncStarted = false;

export function applyDocumentLanguage(locale: Locale): void {
  if (typeof document !== "undefined") {
    document.documentElement.lang = locale;
  }
}

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
    }
  }
  activeLocale.value = locale;
  applyDocumentLanguage(locale);
  notifyLocaleChange(locale);
  return true;
}

export function getLocaleStorage(): Storage | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

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

function localeStorageListener(event: StorageEvent): void {
  handleStorageEvent({
    key: event.key,
    newValue: event.newValue,
    storageArea: event.storageArea ?? undefined,
  });
}

export function startLocaleSync(): void {
  if (localeSyncStarted || typeof window === "undefined") {
    return;
  }
  localeSyncStarted = true;
  window.addEventListener("storage", localeStorageListener);
}

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
