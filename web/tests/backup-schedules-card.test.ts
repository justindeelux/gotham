/* global document: readonly */
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick, ref } from "vue";

import type {
  BackupSchedule,
  ResourceRef,
} from "@/features/backups/api/backups";
import BackupSchedulesCard from "@/features/backups/components/BackupSchedulesCard.vue";
import { resourceBackupsKey } from "@/features/backups/composables/useResourceBackups";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

registerDiscoveredCatalogs();

beforeEach(() => {
  resetLocaleState();
  syncComposerLocale("en");
});

function fakeBackups() {
  const schedules = ref<BackupSchedule[]>([]);
  return {
    backupsStore: {
      schedulesError: null,
      schedulesLoading: false,
      schedulesActing: false,
      schedulesOf: () => schedules.value,
    },
    resource: ref<ResourceRef>({
      resource_type: "application",
      resource_id: "app-1",
    }),
    providerLabel: vi.fn(() => "Local disk"),
    editingScheduleId: ref<string | null>(null),
    scheduleCron: ref(""),
    scheduleMaxRunMinutes: ref(120),
    scheduleKeepLast: ref<number | null>(7),
    scheduleProviderId: ref(""),
    scheduleProviderOptions: ref([]),
    scheduleEnabled: ref(true),
    schedulePauseDuringBackup: ref(true),
    handleToggleSchedule: vi.fn(),
    openScheduleEdit: vi.fn(),
    handleDeleteSchedule: vi.fn(),
    handleCreateSchedule: vi.fn(),
    resetScheduleForm: vi.fn(),
  };
}

function mountCard(backups: ReturnType<typeof fakeBackups>) {
  return mount(BackupSchedulesCard, {
    attachTo: document.body,
    global: {
      provide: { [resourceBackupsKey as symbol]: backups },
    },
  });
}

describe("backup schedule form", () => {
  it("labels the presets with sentences and fills the input on click", async () => {
    const backups = fakeBackups();
    const wrapper = mountCard(backups);
    try {
      const presets = wrapper.findAll(".schedule-form__presets button");
      expect(presets.map((button) => button.text())).toEqual([
        "Every day at 02:00",
        "Every 6 hours at :00",
        "Every Sunday at 03:00",
      ]);
      await presets[1]!.trigger("click");
      await nextTick();

      expect(backups.scheduleCron.value).toBe("0 */6 * * *");
      expect(presets[1]!.attributes("aria-pressed")).toBe("true");
      expect(wrapper.find(".schedule-form__cron-label").text()).toBe(
        "Every 6 hours at :00",
      );
    } finally {
      wrapper.unmount();
    }
  });

  it("shows the sentence beside the input without repeating the expression", async () => {
    const backups = fakeBackups();
    backups.scheduleCron.value = "0 5 * * *";
    const wrapper = mountCard(backups);
    try {
      const label = wrapper.find(".schedule-form__cron-label");
      expect(label.text()).toBe("Every day at 05:00");
      expect(label.text()).not.toContain("0 5 * * *");
      expect(label.text()).not.toContain("Server local time");
      expect(wrapper.text()).not.toContain("Server local time");
      expect(wrapper.find("input").element.value).toBe("0 5 * * *");
    } finally {
      wrapper.unmount();
    }
  });

  it("puts the error in the label and keeps Add schedule disabled", async () => {
    const backups = fakeBackups();
    backups.scheduleCron.value = "0 2 * *";
    const wrapper = mountCard(backups);
    try {
      const alert = wrapper.find('[role="alert"]');
      expect(alert.text()).toContain("Enter a cron expression");
      expect(wrapper.find(".schedule-form__cron-label").text()).toContain(
        "Enter a cron expression",
      );
      expect(wrapper.find(".n-input--error-status").exists()).toBe(true);
      expect(
        wrapper.find(".schedule-form__actions .n-button--disabled").exists(),
      ).toBe(true);
    } finally {
      wrapper.unmount();
    }
  });

  it("shows Save and Cancel while editing an existing schedule", async () => {
    const backups = fakeBackups();
    backups.editingScheduleId.value = "schedule-1";
    backups.scheduleCron.value = "0 2 * * *";
    backups.scheduleMaxRunMinutes.value = 90;
    const wrapper = mountCard(backups);
    try {
      const labels = wrapper.findAll("button").map((button) => button.text());
      expect(labels).toContain("Save schedule");
      expect(labels).toContain("Cancel");
      expect(labels).not.toContain("Add schedule");
      expect(
        wrapper
          .find(".schedule-form__presets button[aria-pressed='true']")
          .text(),
      ).toBe("Every day at 02:00");
    } finally {
      wrapper.unmount();
    }
  });
});
