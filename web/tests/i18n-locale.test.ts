import { beforeEach, describe, expect, it, vi } from "vitest";

/* global Storage: readonly, document: readonly */

import {
  activeLocale,
  handleStorageEvent,
  isSupportedLocale,
  localeStorageKey,
  readStoredLocale,
  resetLocaleState,
  resolveInitialLocale,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

/** memoryStorage builds an isolated Storage for locale tests. */
function memoryStorage(initial?: Record<string, string>): Storage {
  const data = new Map(Object.entries(initial ?? {}));
  return {
    get length() {
      return data.size;
    },
    clear: () => data.clear(),
    getItem: (key: string) => data.get(key) ?? null,
    key: (index: number) => [...data.keys()][index] ?? null,
    removeItem: (key: string) => {
      data.delete(key);
    },
    setItem: (key: string, value: string) => {
      data.set(key, value);
    },
  };
}

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

describe("isSupportedLocale", () => {
  it("accepts only en and vi", () => {
    expect(isSupportedLocale("en")).toBe(true);
    expect(isSupportedLocale("vi")).toBe(true);
    expect(isSupportedLocale("fr")).toBe(false);
    expect(isSupportedLocale("")).toBe(false);
    expect(isSupportedLocale(null)).toBe(false);
  });
});

describe("resolveInitialLocale", () => {
  it("falls back to English when storage is absent", () => {
    expect(resolveInitialLocale(null)).toBe("en");
    expect(resolveInitialLocale(memoryStorage())).toBe("en");
  });

  it("falls back to English for corrupt and unsupported values", () => {
    expect(
      resolveInitialLocale(memoryStorage({ [localeStorageKey]: "fr" })),
    ).toBe("en");
    expect(
      resolveInitialLocale(memoryStorage({ [localeStorageKey]: "" })),
    ).toBe("en");
    expect(
      resolveInitialLocale(memoryStorage({ [localeStorageKey]: "{bad" })),
    ).toBe("en");
  });

  it("reads a stored Vietnamese choice", () => {
    expect(
      resolveInitialLocale(memoryStorage({ [localeStorageKey]: "vi" })),
    ).toBe("vi");
  });

  it("returns null on blocked storage reads", () => {
    const blocked: Storage = memoryStorage();
    vi.spyOn(blocked, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(readStoredLocale(blocked)).toBeNull();
    expect(resolveInitialLocale(blocked)).toBe("en");
  });
});

describe("setLocale", () => {
  it("persists, activates and mirrors document language", () => {
    const storage = memoryStorage();
    expect(setLocale("vi", storage)).toBe(true);
    expect(storage.getItem(localeStorageKey)).toBe("vi");
    expect(activeLocale.value).toBe("vi");
    expect(document.documentElement.lang).toBe("vi");
  });

  it("rejects unsupported input without changing state", () => {
    const storage = memoryStorage();
    expect(setLocale("fr", storage)).toBe(false);
    expect(setLocale("", storage)).toBe(false);
    expect(setLocale(null, storage)).toBe(false);
    expect(storage.getItem(localeStorageKey)).toBeNull();
    expect(activeLocale.value).toBe("en");
  });

  it("keeps a usable in-memory choice when storage is blocked", () => {
    const blocked: Storage = memoryStorage();
    vi.spyOn(blocked, "setItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(setLocale("vi", blocked)).toBe(true);
    expect(activeLocale.value).toBe("vi");
    expect(document.documentElement.lang).toBe("vi");
  });
});

describe("handleStorageEvent (another tab)", () => {
  it("applies the other tab's valid change", () => {
    handleStorageEvent({ key: localeStorageKey, newValue: "vi" });
    expect(activeLocale.value).toBe("vi");
    expect(document.documentElement.lang).toBe("vi");
  });

  it("treats removal as English", () => {
    setLocale("vi", memoryStorage());
    handleStorageEvent({ key: localeStorageKey, newValue: null });
    expect(activeLocale.value).toBe("en");
    expect(document.documentElement.lang).toBe("en");
  });

  it("ignores invalid values and unrelated keys", () => {
    setLocale("vi", memoryStorage());
    handleStorageEvent({ key: localeStorageKey, newValue: "fr" });
    expect(activeLocale.value).toBe("vi");
    handleStorageEvent({ key: "other-key", newValue: "en" });
    expect(activeLocale.value).toBe("vi");
  });
});
