/* global window:readonly */
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";

const {
  listBackups,
  listRestores,
  listSchedules,
  listProviders,
  getBackupProtection,
} = vi.hoisted(() => ({
  listBackups: vi.fn(async () => []),
  listRestores: vi.fn(async () => []),
  listSchedules: vi.fn(async () => []),
  listProviders: vi.fn(async () => []),
  getBackupProtection: vi.fn(async () => ({
    database_id: "db-1",
    status: "not_configured",
    schedules: [],
  })),
}));

vi.mock("@/features/backups/api/backups", async (importOriginal) => {
  const mod =
    await importOriginal<typeof import("@/features/backups/api/backups")>();
  return {
    ...mod,
    listBackups,
    listRestores,
    listSchedules,
    listProviders,
    getBackupProtection,
  };
});

vi.mock("naive-ui", async (importOriginal) => {
  const mod = await importOriginal<typeof import("naive-ui")>();
  return {
    ...mod,
    useMessage: () => ({
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      info: vi.fn(),
    }),
  };
});

import { NMessageProvider, NTabPane, NTabs } from "naive-ui";

import ResourceBackupsTab from "@/features/backups/components/ResourceBackupsTab.vue";
import { registerDiscoveredCatalogs, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

registerDiscoveredCatalogs();

beforeEach(() => {
  window.localStorage.clear();
  window.localStorage.setItem(
    "gotham.auth.session",
    JSON.stringify({
      user: {
        id: "u-1",
        email: "ada@gotham.dev",
        created_at: "2026-03-04T12:00:00Z",
        display_name: "Ada",
        has_password: true,
        role: "owner",
      },
      accessToken: "test-access",
      refreshToken: "test-refresh",
    }),
  );
  setActivePinia(createPinia());
  resetLocaleState();
  syncComposerLocale("en");
  listBackups.mockClear();
  listRestores.mockClear();
  listSchedules.mockClear();
  listProviders.mockClear();
  getBackupProtection.mockClear();
});

describe("resource backups tab mount", () => {
  it("loads every list once when the tab pane mounts active", async () => {
    const Harness = defineComponent({
      name: "BackupsTabHarness",
      setup() {
        const tab = ref("backups");
        return () =>
          h(NMessageProvider, null, {
            default: () =>
              h(
                NTabs,
                { value: tab.value },
                {
                  default: () =>
                    h(
                      NTabPane,
                      { name: "backups", tab: "Backups" },
                      {
                        default: () =>
                          h(ResourceBackupsTab, {
                            resourceType: "database",
                            resourceId: "db-1",
                            active: true,
                          }),
                      },
                    ),
                },
              ),
          });
      },
    });

    const wrapper = mount(Harness);
    try {
      await flushPromises();
      expect(listBackups).toHaveBeenCalledTimes(1);
      expect(listRestores).toHaveBeenCalledTimes(1);
      expect(listSchedules).toHaveBeenCalledTimes(1);
      expect(listProviders).toHaveBeenCalledTimes(1);
      expect(getBackupProtection).toHaveBeenCalledTimes(1);
      expect(listBackups).toHaveBeenCalledWith({
        resource: { resource_type: "database", resource_id: "db-1" },
      });
    } finally {
      wrapper.unmount();
    }
  });
});
