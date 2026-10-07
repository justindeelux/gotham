// I18N-6: retained store banners re-derive in the new locale without refetch.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";
import { createPinia, setActivePinia } from "pinia";

import { useBackupsStore } from "@/features/databases/stores/backups";
import { useDatabaseBackups } from "@/features/databases/composables/useDatabaseBackups";
import {
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

declare global {
  // Counts mocked target test POSTs for the single-request assertion below.
  var __targetTestPosts: number | undefined;
}

vi.mock("@/features/databases/api/backups", async (importOriginal) => {
  const mod =
    await importOriginal<typeof import("@/features/databases/api/backups")>();
  return {
    ...mod,
    listBackups: async () => {
      throw { status: 401, message: "auth: expired" };
    },
    testTarget: async () => {
      globalThis.__targetTestPosts = (globalThis.__targetTestPosts ?? 0) + 1;
      throw { status: 502, message: "agent: unreachable" };
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

describe("retained target-test failure refreshes on switch", () => {
  it("derives the mocked 502 from raw cause once, with no resend", async () => {
    globalThis.__targetTestPosts = 0;
    let backups: ReturnType<typeof useDatabaseBackups> | null = null;
    const Harness = defineComponent({
      name: "TargetTestHarness",
      setup() {
        backups = useDatabaseBackups(ref("db-1"), ref("backups"));
        return () => h("div");
      },
    });
    const wrapper = mount(
      defineComponent({
        name: "TargetTestRoot",
        setup: () => () => h(NMessageProvider, null, { default: () => h(Harness) }),
      }),
    );
    try {
      await backups!.handleTestTarget("target-1");
      expect(globalThis.__targetTestPosts).toBe(1);
      expect(backups!.targetTestMessage("target-1")).toBe(
        "The node agent is unreachable or the job failed on the node. Check the node status and retry. (agent: unreachable)",
      );
      setLocale("vi", null);
      await nextTick();
      // Same retained failure, new locale, still exactly one POST.
      expect(backups!.targetTestMessage("target-1")).toBe(
        "Không kết nối được node agent hoặc tác vụ thất bại trên node. Kiểm tra trạng thái node rồi thử lại. (agent: unreachable)",
      );
      expect(globalThis.__targetTestPosts).toBe(1);
    } finally {
      wrapper.unmount();
    }
  });
});
