// I18N-6: retained store banners re-derive in the new locale without refetch.
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";

import { useBackupsStore } from "@/features/databases/stores/backups";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

vi.mock("@/features/databases/api/backups", async (importOriginal) => {
  const mod =
    await importOriginal<typeof import("@/features/databases/api/backups")>();
  return {
    ...mod,
    listBackups: async () => {
      throw { status: 401, message: "auth: expired" };
    },
  };
});

registerDiscoveredCatalogs();

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
  setActivePinia(createPinia());
});

describe("retained backup banners refresh on switch", () => {
  it("re-derives the stored failure in the new locale without refetch", async () => {
    const store = useBackupsStore();
    await expect(store.fetchBackups("db-1")).rejects.toEqual({
      status: 401,
      message: "auth: expired",
    });
    expect(store.backupsError).toBe("Session expired. Please sign in again.");
    setLocale("vi", null);
    await nextTick();
    expect(store.backupsError).toBe(
      "Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.",
    );
  });
});
