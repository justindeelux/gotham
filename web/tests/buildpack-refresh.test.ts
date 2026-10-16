import { flushPromises, mount } from "@vue/test-utils";
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";

import WizardBuildPackStep from "@/features/applications/components/WizardBuildPackStep.vue";
import {
  provideCreateWizard,
  useCreateAppWizard,
} from "@/features/applications/composables/useCreateAppWizard";
import { useServersStore } from "@/features/servers";
import { listJobs, listTools } from "@/features/toolchain/api/toolchain";
import type { ToolchainTool } from "@/features/toolchain/api/toolchain";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";
import { useWebSocket } from "@/shared/composables/useWebSocket";
import { i18n, registerDiscoveredCatalogs } from "@/shared/i18n";

vi.mock("@/shared/composables/useWebSocket", () => ({ useWebSocket: vi.fn() }));
vi.mock("@/features/toolchain/api/toolchain", async (importOriginal) => ({
  ...await importOriginal<typeof import("@/features/toolchain/api/toolchain")>(),
  listTools: vi.fn(),
  listJobs: vi.fn(),
}));

const socket = { subscribe: vi.fn(), unsubscribe: vi.fn(), close: vi.fn() };
const wrappers: Array<ReturnType<typeof mount>> = [];

function catalog(railpackInstalled = true): ToolchainTool[] {
  return ["docker", "buildkit", "railpack", "pack"].map((id) => ({
    id,
    name: id,
    installed: id === "pack" ? false : id === "railpack" ? railpackInstalled : true,
    installable: true,
    uninstallable: true,
    needs_root: true,
    version_ok: true,
  }));
}

async function mountWizard() {
  let wizard!: ReturnType<typeof useCreateAppWizard>;
  const Harness = defineComponent({
    setup() {
      wizard = useCreateAppWizard(ref(true), (() => undefined) as never);
      wizard.form.sourceType = "git_public";
      wizard.form.serverId = "srv-1";
      wizard.form.buildPack = "railpack";
      wizard.step.value = 2;
      provideCreateWizard(wizard);
      return () => wizard.step.value === 2 ? h(WizardBuildPackStep) : h("div");
    },
  });
  const wrapper = mount({
    render: () => h(NMessageProvider, null, { default: () => h(Harness) }),
  }, { global: { plugins: [i18n] } });
  wrappers.push(wrapper);
  await flushPromises();
  return { wizard, wrapper };
}

function expectMissingEngine(wizard: ReturnType<typeof useCreateAppWizard>) {
  for (const value of ["railpack", ""]) {
    expect(wizard.buildPackChoices.value.find((pack) => pack.value === value)?.selectable)
      .toBe(false);
  }
  expect(wizard.buildPackValid.value).toBe(false);
  expect(wizard.canContinue.value).toBe(false);
}

beforeEach(() => {
  vi.clearAllMocks();
  setActivePinia(createPinia());
  registerDiscoveredCatalogs();
  useServersStore().servers = [{ id: "srv-1", status: "ready" } as never];
  vi.mocked(listTools).mockResolvedValue(catalog());
  vi.mocked(listJobs).mockResolvedValue([]);
  vi.mocked(useWebSocket).mockReturnValue(socket as unknown as ReturnType<typeof useWebSocket>);
});

afterEach(() => {
  for (const wrapper of wrappers.splice(0)) {
    wrapper.unmount();
  }
  useToolchainStore().reset();
});

describe("build-pack catalog refresh", () => {
  it("refetches on Back/Continue and Back from the next step without duplicating subscriptions", async () => {
    const { wizard } = await mountWizard();
    expect(wizard.buildPackValid.value).toBe(true);
    wizard.prevStep();
    await flushPromises();
    vi.mocked(listTools).mockResolvedValue(catalog(false));
    wizard.nextStep();
    await flushPromises();
    expect(listTools).toHaveBeenCalledTimes(2);
    expectMissingEngine(wizard);

    wizard.step.value = 3;
    await flushPromises();
    wizard.prevStep();
    await flushPromises();
    expect(listTools).toHaveBeenCalledTimes(3);
    expect(useWebSocket).toHaveBeenCalledTimes(1);
    expect(socket.subscribe).toHaveBeenCalledTimes(1);
  });

  it("refetches on focus and removes the listener when the step leaves or unmounts", async () => {
    const { wizard, wrapper } = await mountWizard();
    vi.mocked(listTools).mockResolvedValue(catalog(false));
    globalThis.window.dispatchEvent(new globalThis.Event("focus"));
    await flushPromises();
    expect(listTools).toHaveBeenCalledTimes(2);
    expectMissingEngine(wizard);
    wizard.prevStep();
    await flushPromises();
    globalThis.window.dispatchEvent(new globalThis.Event("focus"));
    await flushPromises();
    expect(listTools).toHaveBeenCalledTimes(2);

    wizard.nextStep();
    await flushPromises();
    globalThis.window.dispatchEvent(new globalThis.Event("focus"));
    await flushPromises();
    expect(listTools).toHaveBeenCalledTimes(4);
    expect(useWebSocket).toHaveBeenCalledTimes(1);
    expect(socket.subscribe).toHaveBeenCalledTimes(1);
    wrapper.unmount();
    globalThis.window.dispatchEvent(new globalThis.Event("focus"));
    await flushPromises();
    expect(listTools).toHaveBeenCalledTimes(4);
  });

  it("shows refresh failures and recovers on a later focus", async () => {
    const { wizard } = await mountWizard();
    vi.mocked(listTools).mockRejectedValueOnce(new Error("agent unreachable"));
    globalThis.window.dispatchEvent(new globalThis.Event("focus"));
    await flushPromises();
    expect(wizard.nodeToolsError.value).toBe(true);
    globalThis.window.dispatchEvent(new globalThis.Event("focus"));
    await flushPromises();
    expect(wizard.nodeToolsError.value).toBe(false);
    expect(wizard.buildPackValid.value).toBe(true);
  });

  it("re-establishes one subscription after the wizard closes and reopens", async () => {
    const { wizard } = await mountWizard();
    wizard.closeWizard();
    await flushPromises();
    expect(socket.unsubscribe).toHaveBeenCalledTimes(1);
    expect(socket.close).toHaveBeenCalledTimes(1);
    wizard.form.serverId = "srv-1";
    wizard.step.value = 2;
    await flushPromises();
    expect(listTools).toHaveBeenCalledTimes(2);
    expect(useWebSocket).toHaveBeenCalledTimes(2);
    expect(socket.subscribe).toHaveBeenCalledTimes(2);
  });
});
