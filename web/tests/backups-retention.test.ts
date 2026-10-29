import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h } from "vue";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";

import BackupProvidersPanel from "@/features/backups/components/BackupProvidersPanel.vue";
import {
  backupMessages,
  isKeepLastValid,
  keepLastSchema,
} from "@/features/backups/schemas/backups";
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
