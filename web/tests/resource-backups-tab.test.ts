import { flushPromises, mount } from "@vue/test-utils";
import { createPinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";

import ResourceBackupsTab from "@/features/backups/components/ResourceBackupsTab.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

const state = vi.hoisted(() => ({
  contents: [] as Array<{ name: string; kind: string; target?: string }>,
  backups: [] as Array<Record<string, unknown>>,
}));

vi.mock("naive-ui", async (importOriginal) => {
  const original = await importOriginal<typeof import("naive-ui")>();
  return {
    ...original,
    useMessage: () => ({ success: vi.fn(), error: vi.fn(), warning: vi.fn() }),
  };
});

vi.mock("@/features/auth", () => ({
  useAuthStore: () => ({ isOwner: true }),
}));

vi.mock("@/features/backups/api/backups", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@/features/backups/api/backups")>();
  return {
    ...original,
    listBackups: async () => state.backups,
    listRestores: async () => [],
    listSchedules: async () => [],
    getResourceBackupProtection: async () => ({
      resource_type: "application",
      resource_id: "app-1",
      status: "not_configured",
      schedules: [],
    }),
    listProviders: async () => [],
    getBackupContents: async () => state.contents,
  };
});

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  state.contents = [];
  state.backups = [];
});

function mountTab() {
  return mount(ResourceBackupsTab, {
    props: {
      resourceType: "application",
      resourceId: "app-1",
      resourceLabel: "shop",
      active: true,
    },
    global: {
      plugins: [createPinia(), i18n],
      stubs: {
        RouterLink: { template: "<a><slot /></a>" },
      },
    },
  });
}

describe("ResourceBackupsTab contents", () => {
  it("names what a run captures and offers the pause choice", async () => {
    state.contents = [{ name: "uploads", kind: "bind", target: "/data" }];
    const wrapper = mountTab();
    await flushPromises();

    const contents = wrapper.get('[data-testid="backup-contents"]');
    expect(contents.text()).toContain("What is included");
    expect(contents.text()).toContain("uploads");
    expect(contents.text()).toContain("/data");
    expect(contents.text()).toContain("Bind mount");
    expect(wrapper.text()).toContain(
      "Stop the resource while the archive is taken",
    );
  });

  it("offers the stop-resource option as a checkbox on schedules", async () => {
    const wrapper = mountTab();
    await flushPromises();

    const pause = wrapper.get(".schedule-pause");
    expect(pause.find(".n-checkbox").exists()).toBe(true);
    expect(pause.find(".n-switch").exists()).toBe(false);
    expect(pause.text()).toContain("Stop the resource during backup");
    expect(pause.text()).toContain("The backup is consistent");
    wrapper.unmount();
  });

  it("explains the cron expression while typing", async () => {
    const wrapper = mountTab();
    await flushPromises();
    const form = wrapper.get(".schedule-form__cron-group");
    const input = form.get("input");

    await input.setValue("0 2 * * *");
    await flushPromises();
    expect(form.text()).toContain("Every day at 02:00");
    expect(form.text()).not.toContain("(0 2 * * *)");
    expect(form.text()).not.toContain("Server local time");

    await input.setValue("0 2 * *");
    await flushPromises();
    expect(form.text()).toContain(
      "Enter a cron expression with 5 fields",
    );
    expect(form.text()).not.toContain("Every day at");
    wrapper.unmount();
  });

  it("labels a run that was taken without stopping", async () => {
    state.backups = [
      {
        id: "run-1",
        resource_type: "application",
        resource_id: "app-1",
        type: "manual",
        status: "completed",
        size: 1024,
        pause_during_backup: false,
        created_at: "2026-01-01T00:00:00Z",
      },
    ];
    const wrapper = mountTab();
    await flushPromises();

    expect(wrapper.get("#backup-runs").text()).toContain("Did not stop");
  });

  it("explains why an application has nothing to capture", async () => {
    const wrapper = mountTab();
    await flushPromises();

    expect(wrapper.text()).toContain(
      "This application has no persistent storage to back up. Add a storage mapping first.",
    );
  });
});
