import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";

import type {
  BackupProtectionSchedule,
  BackupProtectionSummary,
} from "@/features/backups/api/backups";
import BackupProtectionCard from "@/features/backups/components/BackupProtectionCard.vue";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

registerDiscoveredCatalogs();

function schedule(
  overrides: Partial<BackupProtectionSchedule> = {},
): BackupProtectionSchedule {
  return {
    schedule_id: "schedule-1",
    cron: "0 2 * * *",
    target_id: "target-1",
    target_name: "offsite",
    status: "protected",
    last_success_at: "2026-10-27T02:05:00Z",
    last_backup_id: "backup-1",
    expected_slot_at: "2026-10-27T02:00:00Z",
    overdue_seconds: 0,
    max_run_seconds: 7200,
    ...overrides,
  };
}

function mountCard(
  summary: BackupProtectionSummary | null,
  loadedBackupIds: string[] = ["backup-1"],
) {
  return mount(BackupProtectionCard, {
    props: {
      summary,
      targetLabel: (targetId?: string) =>
        targetId ? "offsite" : "local disk",
      targetHref: (targetId: string) => `#backup-target-${targetId}`,
      runHref: (backupId?: string) =>
        !backupId
          ? "#backup-runs"
          : loadedBackupIds.includes(backupId)
            ? `#backup-run-${backupId}`
            : "#backup-runs",
    },
  });
}

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

describe("backup protection card", () => {
  it("shows a healthy schedule and links its success and target", () => {
    const wrapper = mountCard({
      resource_type: "database",
      resource_id: "db-1",
      database_id: "db-1",
      status: "protected",
      schedules: [schedule()],
    });
    try {
      expect(wrapper.text()).toContain("Protected");
      expect(wrapper.find('a[href="#backup-run-backup-1"]').exists()).toBe(true);
      expect(wrapper.find('a[href="#backup-target-target-1"]').exists()).toBe(
        true,
      );
    } finally {
      wrapper.unmount();
    }
  });

  it("falls back to the runs list when the success is outside the page", () => {
    const wrapper = mountCard(
      {
        resource_type: "database",
        resource_id: "db-1",
        database_id: "db-1",
        status: "protected",
        schedules: [schedule()],
      },
      [],
    );
    try {
      expect(wrapper.find('a[href="#backup-run-backup-1"]').exists()).toBe(
        false,
      );
      expect(wrapper.find('a[href="#backup-runs"]').exists()).toBe(true);
    } finally {
      wrapper.unmount();
    }
  });

  it("shows overdue elapsed time", () => {
    const wrapper = mountCard({
      resource_type: "database",
      resource_id: "db-1",
      database_id: "db-1",
      status: "overdue",
      schedules: [
        schedule({
          status: "overdue",
          last_success_at: undefined,
          last_backup_id: undefined,
          overdue_seconds: 7200,
        }),
      ],
    });
    try {
      expect(wrapper.text()).toContain("Overdue");
      expect(wrapper.text()).toContain("Overdue for 2 h");
    } finally {
      wrapper.unmount();
    }
  });

  it("shows a stuck run and links the running backup", () => {
    const wrapper = mountCard(
      {
        resource_type: "database",
        resource_id: "db-1",
        database_id: "db-1",
        status: "stuck",
        schedules: [
          schedule({
            status: "stuck",
            running_since: "2026-10-27T02:00:00Z",
            running_backup_id: "backup-running",
            overdue_seconds: 3600,
          }),
        ],
      },
      ["backup-1", "backup-running"],
    );
    try {
      expect(wrapper.text()).toContain("Stuck");
      expect(
        wrapper.find('a[href="#backup-run-backup-running"]').exists(),
      ).toBe(true);
    } finally {
      wrapper.unmount();
    }
  });

  it("renders an application or service summary like a database", () => {
    for (const [resourceType, resourceId] of [
      ["application", "app-1"],
      ["service", "svc-1"],
    ] as const) {
      const wrapper = mountCard({
        resource_type: resourceType,
        resource_id: resourceId,
        status: "protected",
        schedules: [schedule()],
      });
      try {
        expect(wrapper.text()).toContain("Protected");
      } finally {
        wrapper.unmount();
      }
    }
  });

  it("shows not configured without a schedule", () => {
    const wrapper = mountCard({
      resource_type: "database",
      resource_id: "db-1",
      database_id: "db-1",
      status: "not_configured",
      schedules: [],
    });
    try {
      expect(wrapper.text()).toContain("Backup not configured");
    } finally {
      wrapper.unmount();
    }
  });
});
