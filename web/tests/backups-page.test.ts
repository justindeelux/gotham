/* global document: readonly */
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick, ref } from "vue";

import type { BackupRun, StorageProviderKind } from "@/features/backups/api/backups";
import BackupProviderDialog from "@/features/backups/components/BackupProviderDialog.vue";
import en from "@/features/backups/locales/en";
import viMessages from "@/features/backups/locales/vi";
import type { ProviderCatalog } from "@/features/backups/composables/useProviderCatalog";
import { providerCatalogKey } from "@/features/backups/composables/useProviderCatalog";
import {
  CRON_PRESETS,
  describeCron,
  matchesBackupFilters,
  runDuration,
} from "@/features/backups/utils/backupStatus";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";
import { checkCatalogParity } from "@/shared/i18n/catalog";

registerDiscoveredCatalogs();

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

function run(overrides: Partial<BackupRun> = {}): BackupRun {
  return {
    id: "backup-1",
    resource_type: "database",
    resource_id: "db-1",
    type: "manual",
    status: "completed",
    size: 1024,
    pause_during_backup: true,
    created_at: "2026-10-01T12:00:00.000Z",
    finished_at: "2026-10-01T12:02:30.000Z",
    ...overrides,
  };
}

describe("backups catalog", () => {
  it("has en/vi parity", () => {
    expect(
      checkCatalogParity(
        en as unknown as Record<string, unknown>,
        viMessages as unknown as Record<string, unknown>,
      ),
    ).toEqual([]);
  });
});

describe("backup filters", () => {
  it("matches resource type and status together", () => {
    expect(matchesBackupFilters(run(), "all", "all")).toBe(true);
    expect(matchesBackupFilters(run(), "database", "completed")).toBe(true);
    expect(matchesBackupFilters(run(), "application", "all")).toBe(false);
    expect(matchesBackupFilters(run({ status: "failed" }), "all", "completed")).toBe(
      false,
    );
  });
});

describe("cron in plain language", () => {
  it("names every preset", () => {
    expect(describeCron(CRON_PRESETS[0])).toBe("Every day at 02:00");
    expect(describeCron(CRON_PRESETS[1])).toBe("Every 6 hours");
    expect(describeCron(CRON_PRESETS[2])).toBe("Every Sunday at 03:00");
  });

  it("keeps an unknown expression visible", () => {
    expect(describeCron("*/5 * * * *")).toBe(
      "Custom schedule (*/5 * * * *)",
    );
  });

  it("translates to Vietnamese", () => {
    setLocale("vi", null);
    expect(describeCron(CRON_PRESETS[1])).toBe("Mỗi 6 giờ");
  });
});

describe("run duration", () => {
  it("uses the finished timestamp and reports a live run", () => {
    expect(runDuration(run().created_at, run().finished_at)).toBe("2 min 30 s");
    expect(runDuration(run().created_at, undefined)).toBe("in progress");
    expect(
      runDuration("2026-10-01T12:00:00.000Z", "2026-10-01T12:00:05.000Z"),
    ).toBe("5 s");
  });
});

function providerCatalog(overrides: Partial<ProviderCatalog> = {}): ProviderCatalog {
  return {
    backupsStore: { providersActing: false },
    dialogOpen: ref(true),
    editingId: ref(null),
    name: ref(""),
    kind: ref<StorageProviderKind>("s3"),
    endpoint: ref(""),
    region: ref(""),
    bucket: ref(""),
    prefix: ref(""),
    accessKey: ref(""),
    secretKey: ref(""),
    checks: ref({}),
    resetForm: vi.fn(),
    openCreate: vi.fn(),
    openEdit: vi.fn(),
    close: vi.fn(),
    save: vi.fn(),
    remove: vi.fn(),
    test: vi.fn(),
    checkMessage: vi.fn(() => ""),
    ...overrides,
  } as unknown as ProviderCatalog;
}

describe("storage provider dialog", () => {
  it("shows the S3 fields and the write-only key note", async () => {
    const catalog = providerCatalog();
    const wrapper = mount(BackupProviderDialog, {
      attachTo: document.body,
      global: {
        provide: { [providerCatalogKey as symbol]: catalog },
      },
    });
    try {
      await nextTick();
      const text = document.body.textContent ?? "";
      expect(text).toContain("Add storage provider");
      expect(text).toContain("Endpoint");
      expect(text).toContain("Bucket");
      expect(text).toContain("Secret key");
      expect(text).toContain("never shown back");
    } finally {
      wrapper.unmount();
    }
  });

  it("hides the S3 fields for the local disk kind", async () => {
    const catalog = providerCatalog({ kind: ref<StorageProviderKind>("local") });
    const wrapper = mount(BackupProviderDialog, {
      attachTo: document.body,
      global: {
        provide: { [providerCatalogKey as symbol]: catalog },
      },
    });
    try {
      await nextTick();
      const text = document.body.textContent ?? "";
      expect(text).not.toContain("Endpoint");
      expect(text).toContain("Local disk keeps artifacts");
    } finally {
      wrapper.unmount();
    }
  });
});
