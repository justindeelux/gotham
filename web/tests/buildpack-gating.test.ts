// JUS-179 build-pack gating: the picker only offers build packs whose toolchain
// is installed on the target node, offers an inline Install that enqueues the
// engine's deps through the toolchain store and opens the JUS-178 modal, and
// re-enables automatically as the store reloads the catalog. The toolchain
// store is mocked so the states are driven directly.
import { mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, reactive, ref } from "vue";

const toolchainStore = reactive({
  serverId: "",
  tools: [] as Array<{ id: string; installed: boolean }>,
  activeJobs: [] as Array<{ tool_id: string; action: string }>,
  enqueue: vi.fn(async () => [] as string[]),
  openQueueModal: vi.fn(),
  subscribe: vi.fn(async () => {}),
});

vi.mock("@/features/toolchain", () => ({
  useToolchainStore: () => toolchainStore,
  describeToolchainError: (error: unknown) => String(error),
}));

import {
  BUILD_PACK_TOOLS,
  useCreateAppWizard,
} from "@/features/applications/composables/useCreateAppWizard";
import { registerDiscoveredCatalogs } from "@/shared/i18n";

/** mountWizard exposes the raw wizard state without rendering a step. */
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

/** selectNode points the wizard and the (mocked) store at one node. */
function selectNode(w: ReturnType<typeof useCreateAppWizard>, id = "srv-1"): void {
  w.form.serverId = id;
  toolchainStore.serverId = id;
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  toolchainStore.serverId = "";
  toolchainStore.tools = [];
  toolchainStore.activeJobs = [];
  toolchainStore.enqueue.mockClear();
  toolchainStore.enqueue.mockResolvedValue([]);
  toolchainStore.openQueueModal.mockClear();
  toolchainStore.subscribe.mockClear();
  toolchainStore.subscribe.mockResolvedValue(undefined);
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
    // Auto stays selectable because Buildpacks is installed.
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
    // Buildpacks is missing, so no CLI engine at all is not the case here.
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
    // Dockerfile and static stay available.
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
  it("points the toolchain store at the wizard's node once", () => {
    const { w, wrapper } = mountWizard();
    w.form.serverId = "srv-1";
    w.subscribeNodeTools();
    expect(toolchainStore.subscribe).toHaveBeenCalledWith("srv-1");
    toolchainStore.serverId = "srv-1";
    w.subscribeNodeTools();
    expect(toolchainStore.subscribe).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });
});
