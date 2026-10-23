import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, reactive, ref } from "vue";

const toolchainStore = reactive({
  serverId: "",
  tools: [] as Array<{ id: string; installed: boolean }>,
  activeJobs: [] as Array<{ tool_id: string; action: string }>,
  loading: false,
  failure: null as unknown,
  enqueue: vi.fn(async () => [] as string[]),
  openQueueModal: vi.fn(),
  closeQueueModal: vi.fn(),
  subscribe: vi.fn(async () => {}),
  refresh: vi.fn(async () => {}),
  disconnect: vi.fn(),
});

vi.mock("@/features/toolchain", () => ({
  useToolchainStore: () => toolchainStore,
  describeToolchainError: (error: unknown) => String(error),
}));

import {
  BUILD_PACK_TOOLS,
  useCreateAppWizard,
} from "@/features/applications/composables/useCreateAppWizard";
import { useServersStore } from "@/features/servers";
import { registerDiscoveredCatalogs } from "@/shared/i18n";

function mountWizard() {
  setActivePinia(createPinia());
  let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
  const Harness = defineComponent({
    setup() {
      wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
      return () => h("div");
    },
  });
  const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
  return { w: wiz!, wrapper };
}

function selectNode(w: ReturnType<typeof useCreateAppWizard>, id = "srv-1"): void {
  w.form.serverId = id;
  toolchainStore.serverId = id;
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  toolchainStore.serverId = "";
  toolchainStore.tools = [];
  toolchainStore.activeJobs = [];
  toolchainStore.loading = false;
  toolchainStore.failure = null;
  toolchainStore.enqueue.mockClear();
  toolchainStore.enqueue.mockResolvedValue([]);
  toolchainStore.openQueueModal.mockClear();
  toolchainStore.closeQueueModal.mockClear();
  toolchainStore.subscribe.mockClear();
  toolchainStore.subscribe.mockResolvedValue(undefined);
  toolchainStore.refresh.mockClear();
  toolchainStore.refresh.mockResolvedValue(undefined);
  toolchainStore.disconnect.mockClear();
});

describe("build-pack gating on the target node", () => {
  it("offers every pack while the node's tools are unknown", () => {
    const { w, wrapper } = mountWizard();
    w.form.sourceType = "git_public";
    expect(w.buildPackChoices.value.every((choice) => choice.selectable)).toBe(true);
    expect(w.buildPackValid.value).toBe(true);
    expect(w.noBuildEngine.value).toBe(false);
    wrapper.unmount();
  });

  it("disables a pack whose tool is missing and marks it installable", () => {
    const { w, wrapper } = mountWizard();
    w.form.sourceType = "git_public";
    selectNode(w);
    toolchainStore.tools = [
      { id: "docker", installed: true },
      { id: "buildkit", installed: true },
      { id: "railpack", installed: false },
      { id: "pack", installed: true },
    ];
    const railpack = w.buildPackChoices.value.find((choice) => choice.value === "railpack")!;
    expect(railpack.selectable).toBe(false);
    expect(railpack.installed).toBe(false);
    expect(railpack.missing).toBe("railpack");
    const buildpacks = w.buildPackChoices.value.find((choice) => choice.value === "buildpacks")!;
    expect(buildpacks.selectable).toBe(true);
    const auto = w.buildPackChoices.value.find((choice) => choice.value === "")!;
    expect(auto.selectable).toBe(true);
    wrapper.unmount();
  });

  it("enables a pack once its whole toolchain (deps included) is present", () => {
    const { w, wrapper } = mountWizard();
    w.form.sourceType = "git_public";
    selectNode(w);
    toolchainStore.tools = [
      { id: "docker", installed: true },
      { id: "buildkit", installed: true },
      { id: "railpack", installed: true },
    ];
    const railpack = w.buildPackChoices.value.find((choice) => choice.value === "railpack")!;
    expect(railpack.selectable).toBe(true);
    expect(railpack.missing).toBeNull();
    expect(w.noBuildEngine.value).toBe(false);
    wrapper.unmount();
  });

  it("treats a partially installed railpack chain as missing (buildkit dep)", () => {
    const { w, wrapper } = mountWizard();
    w.form.sourceType = "git_public";
    selectNode(w);
    toolchainStore.tools = [
      { id: "docker", installed: true },
      { id: "railpack", installed: true },
      { id: "buildkit", installed: false },
    ];
    const railpack = w.buildPackChoices.value.find((choice) => choice.value === "railpack")!;
    expect(railpack.selectable).toBe(false);
    expect(railpack.missing).toBe("buildkit");
    wrapper.unmount();
  });

  it("blocks auto and submit when no CLI engine is installed", () => {
    const { w, wrapper } = mountWizard();
    w.form.sourceType = "git_public";
    w.form.buildPack = "";
    selectNode(w);
    toolchainStore.tools = [
      { id: "docker", installed: true },
      { id: "buildkit", installed: false },
      { id: "railpack", installed: false },
      { id: "pack", installed: false },
    ];
    expect(w.noBuildEngine.value).toBe(true);
    const auto = w.buildPackChoices.value.find((choice) => choice.value === "")!;
    expect(auto.selectable).toBe(false);
    expect(w.buildPackValid.value).toBe(false);
    for (const value of ["dockerfile", "static"]) {
      expect(w.buildPackChoices.value.find((choice) => choice.value === value)!.selectable).toBe(true);
    }
    w.form.buildPack = "dockerfile";
    expect(w.buildPackValid.value).toBe(true);
    wrapper.unmount();
  });

  it("marks a pack installing while its install job runs", () => {
    const { w, wrapper } = mountWizard();
    w.form.sourceType = "git_public";
    selectNode(w);
    toolchainStore.tools = [
      { id: "docker", installed: true },
      { id: "buildkit", installed: false },
      { id: "railpack", installed: false },
    ];
    toolchainStore.activeJobs = [{ tool_id: "buildkit", action: "install" }];
    const railpack = w.buildPackChoices.value.find((choice) => choice.value === "railpack")!;
    expect(railpack.installing).toBe(true);
    wrapper.unmount();
  });
});

describe("installBuildPack", () => {
  it("enqueues the engine's tools deps first and opens the progress modal", async () => {
    const { w, wrapper } = mountWizard();
    w.form.sourceType = "git_public";
    selectNode(w);
    await w.installBuildPack("railpack");
    expect(toolchainStore.enqueue).toHaveBeenCalledWith(BUILD_PACK_TOOLS.railpack, "install");
    expect(BUILD_PACK_TOOLS.railpack).toEqual(["docker", "buildkit", "railpack"]);
    expect(toolchainStore.openQueueModal).toHaveBeenCalled();
    wrapper.unmount();
  });

  it("does nothing with no node selected", async () => {
    const { w, wrapper } = mountWizard();
    w.form.serverId = "";
    await w.installBuildPack("buildpacks");
    expect(toolchainStore.enqueue).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});

describe("subscribeNodeTools", () => {
  it("subscribes once for a ready node", () => {
    const { w, wrapper } = mountWizard();
    useServersStore().servers = [
      { id: "srv-1", name: "node-1", ip: "127.0.0.1", status: "ready" } as never,
    ];
    w.form.serverId = "srv-1";
    w.subscribeNodeTools();
    expect(toolchainStore.subscribe).toHaveBeenCalledWith("srv-1");
    toolchainStore.serverId = "srv-1";
    w.subscribeNodeTools();
    expect(toolchainStore.subscribe).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("does not fetch tools for a non-ready node (no live agent)", () => {
    const { w, wrapper } = mountWizard();
    useServersStore().servers = [
      { id: "srv-1", name: "node-1", ip: "127.0.0.1", status: "pending" } as never,
    ];
    w.form.serverId = "srv-1";
    w.subscribeNodeTools();
    expect(toolchainStore.subscribe).not.toHaveBeenCalled();
    wrapper.unmount();
  });
});

describe("node tool fetch state", () => {
  it("exposes loading and error without disabling every pack, and retries", async () => {
    const { w, wrapper } = mountWizard();
    w.form.sourceType = "git_public";
    selectNode(w);
    toolchainStore.loading = true;
    expect(w.nodeToolsLoading.value).toBe(true);
    expect(w.nodeToolsError.value).toBe(false);
    toolchainStore.loading = false;
    toolchainStore.failure = new Error("agent unreachable");
    expect(w.nodeToolsLoading.value).toBe(false);
    expect(w.nodeToolsError.value).toBe(true);
    expect(w.buildPackChoices.value.every((choice) => choice.selectable)).toBe(true);
    await w.retryNodeTools();
    expect(toolchainStore.refresh).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("does not report loading or error for another node", () => {
    const { w, wrapper } = mountWizard();
    w.form.serverId = "srv-a";
    toolchainStore.serverId = "srv-b";
    toolchainStore.loading = true;
    toolchainStore.failure = new Error("boom");
    expect(w.nodeToolsLoading.value).toBe(false);
    expect(w.nodeToolsError.value).toBe(false);
    wrapper.unmount();
  });
});

describe("toolchain socket lifecycle", () => {
  it("closes the modal, disconnects and forgets the node on wizard close", () => {
    const { w, wrapper } = mountWizard();
    w.form.serverId = "srv-1";
    toolchainStore.serverId = "srv-1";
    w.closeWizard();
    expect(toolchainStore.closeQueueModal).toHaveBeenCalled();
    expect(toolchainStore.disconnect).toHaveBeenCalled();
    expect(toolchainStore.serverId).toBe("");
    wrapper.unmount();
  });
});
