import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h, ref } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

import BackupProvidersPanel from "@/features/backups/components/BackupProvidersPanel.vue";
import {
  backupMessages,
  isKeepLastValid,
  keepLastSchema,
} from "@/features/backups/schemas/backups";
import type { BackupSchedule } from "@/features/backups/api/backups";
import type { ResourceBackups } from "@/features/backups/composables/useResourceBackups";
import { useResourceBackups } from "@/features/backups/composables/useResourceBackups";
import { useBackupsStore } from "@/features/backups/stores/backups";
import { i18n, registerDiscoveredCatalogs } from "@/shared/i18n";

describe("backup retention form schema", () => {
  it("accepts 1 to 365 backups", () => {
    expect(keepLastSchema.safeParse(1).success).toBe(true);
    expect(keepLastSchema.safeParse(365).success).toBe(true);
  });

  it("rejects zero, more than a year and fractions", () => {
    expect(keepLastSchema.safeParse(0).success).toBe(false);
    expect(keepLastSchema.safeParse(366).success).toBe(false);
    expect(keepLastSchema.safeParse(1.5).success).toBe(false);
    expect(keepLastSchema.safeParse(null).success).toBe(false);
    expect(backupMessages.keepLastInvalid).toBe(
      "backups.schedules.keepLastInvalid",
    );
  });

  it("treats an unlimited policy as valid for an existing schedule", () => {
    expect(isKeepLastValid(null)).toBe(true);
    expect(isKeepLastValid(7)).toBe(true);
    expect(isKeepLastValid(0)).toBe(false);
  });
});

describe("editing a schedule's retention", () => {
  const legacySchedule: BackupSchedule = {
    id: "sched-1",
    resource_type: "database",
    resource_id: "db-1",
    cron: "0 2 * * *",
    enabled: true,
    next_run_at: "2026-10-30T02:00:00Z",
    max_run_seconds: 7200,
    keep_last: null,
    created_at: "2026-09-01T10:00:00Z",
    updated_at: "2026-09-01T10:00:00Z",
  };

  beforeEach(() => {
    setActivePinia(createPinia());
    registerDiscoveredCatalogs();
  });

  async function mountComposable(): Promise<{
    backups: ResourceBackups;
    unmount: () => void;
  }> {
    let backups: ResourceBackups | null = null;
    const Harness = defineComponent({
      setup() {
        backups = useResourceBackups(ref("database"), ref("db-1"), ref(false));
        return () => h("div");
      },
    });
    const wrapper = mount(
      defineComponent({
        render() {
          return h(NMessageProvider, null, { default: () => h(Harness) });
        },
      }),
      { global: { plugins: [i18n] } },
    );
    await flushPromises();
    if (backups === null) {
      throw new Error("the composable did not mount");
    }
    return { backups, unmount: () => wrapper.unmount() };
  }

  it("omits keep_last when a legacy unlimited schedule only changes its cron", async () => {
    const store = useBackupsStore();
    const edit = vi
      .spyOn(store, "editSchedule")
      .mockResolvedValue(legacySchedule);
    vi.spyOn(store, "fetchProtection").mockResolvedValue(undefined);
    const { backups, unmount } = await mountComposable();
    try {
      backups.openScheduleEdit(legacySchedule);
      backups.scheduleCron.value = "0 3 * * *";
      await backups.handleCreateSchedule();

      expect(edit).toHaveBeenCalledTimes(1);
      const payload = edit.mock.calls[0]?.[2];
      expect(payload).not.toHaveProperty("keep_last");
      expect(payload?.cron).toBe("0 3 * * *");
    } finally {
      unmount();
    }
  });

  it("sends the retention limit the owner entered", async () => {
    const store = useBackupsStore();
    const edit = vi
      .spyOn(store, "editSchedule")
      .mockResolvedValue({ ...legacySchedule, keep_last: 30 });
    vi.spyOn(store, "fetchProtection").mockResolvedValue(undefined);
    const { backups, unmount } = await mountComposable();
    try {
      backups.openScheduleEdit(legacySchedule);
      backups.scheduleKeepLast.value = 30;
      await backups.handleCreateSchedule();

      expect(edit).toHaveBeenCalledTimes(1);
      expect(edit.mock.calls[0]?.[2]?.keep_last).toBe(30);
    } finally {
      unmount();
    }
  });
});

describe("storage usage view", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    registerDiscoveredCatalogs();
  });

  it("shows artifact count, size and local free space", async () => {
    const store = useBackupsStore();
    store.providers = [
      {
        id: "p-1",
        name: "Local disk",
        kind: "local",
        builtin: true,
        has_credentials: false,
        backup_count: 3,
        total_size: 2 * 1024 ** 3,
        free_space: 10 * 1024 ** 3,
        total_space: 40 * 1024 ** 3,
        created_at: "2026-10-01T10:00:00Z",
        updated_at: "2026-10-01T10:00:00Z",
      },
    ];
    const wrapper = mount(
      defineComponent({
        render() {
          return h(NMessageProvider, null, {
            default: () => h(BackupProvidersPanel),
          });
        },
      }),
      { global: { plugins: [i18n] } },
    );
    await flushPromises();
    const text = wrapper.text();
    expect(text).toContain("3 backups · 2.00 GiB");
    expect(text).toContain("10.00 GiB free of 40.00 GiB");
  });
});
