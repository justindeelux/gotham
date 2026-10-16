// JUS-179 build-pack step UI: the disabled option, its reason, the Install
// button (with a contextual accessible name), the loading/error notes and the
// no-engine CTA. The wizard state is stubbed so the step renders deterministically.
import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";
import { computed, defineComponent, h, reactive } from "vue";

import WizardBuildPackStep from "@/features/applications/components/WizardBuildPackStep.vue";
import { provideCreateWizard } from "@/features/applications/composables/useCreateAppWizard";
import type { BuildPackChoice } from "@/features/applications/composables/useCreateAppWizard";
import { i18n, registerDiscoveredCatalogs } from "@/shared/i18n";

registerDiscoveredCatalogs();

const installBuildPack = vi.fn();
const retryNodeTools = vi.fn();
const subscribeNodeTools = vi.fn();

interface StepOptions {
  choices?: BuildPackChoice[];
  loading?: boolean;
  error?: boolean;
  noEngine?: boolean;
}

/** mountStep renders the step against a stubbed wizard state. */
function mountStep(options: StepOptions = {}) {
  const form = reactive({ buildPack: "", serverId: "srv-1" });
  const state = {
    form,
    isComposePaste: computed(() => false),
    isDockerfile: computed(() => false),
    nodeToolsLoading: computed(() => options.loading ?? false),
    nodeToolsError: computed(() => options.error ?? false),
    noBuildEngine: computed(() => options.noEngine ?? false),
    buildPackChoices: computed(() => options.choices ?? []),
    installBuildPack,
    retryNodeTools,
    subscribeNodeTools,
  };
  const Harness = defineComponent({
    setup() {
      provideCreateWizard(state as never);
      return () => h(WizardBuildPackStep);
    },
  });
  return mount(Harness, { global: { plugins: [i18n] } });
}

/** railpackChoice is a missing Railpack row the Install button targets. */
function railpackChoice(overrides: Partial<BuildPackChoice> = {}): BuildPackChoice {
  return {
    value: "railpack",
    label: "Railpack",
    hint: "Language auto-detection without a Dockerfile.",
    tools: ["docker", "buildkit", "railpack"],
    installed: false,
    selectable: false,
    missing: "railpack",
    installing: false,
    ...overrides,
  };
}

describe("build-pack step rendering", () => {
  it("disables a missing pack, shows the reason and an Install button", async () => {
    installBuildPack.mockClear();
    const wrapper = mountStep({ choices: [railpackChoice()] });
    expect(subscribeNodeTools).toHaveBeenCalled();
    const row = wrapper.find(".pack-row");
    expect(row.find(".n-radio--disabled").exists()).toBe(true);
    expect(row.text()).toContain("railpack is not installed");
    const install = row.find("button");
    expect(install.text()).toBe("Install");
    expect(install.attributes("aria-label")).toBe("Install Railpack");
    await install.trigger("click");
    expect(installBuildPack).toHaveBeenCalledWith("railpack");
    wrapper.unmount();
  });

  it("shows the install spinner text and disables the button while installing", () => {
    const wrapper = mountStep({ choices: [railpackChoice({ installing: true })] });
    const install = wrapper.find("button");
    expect(install.attributes("disabled")).toBeDefined();
    expect(wrapper.find(".pack-row").text()).toContain("Installing railpack");
    wrapper.unmount();
  });

  it("notes a pending tool check and offers a retry on failure", async () => {
    const loading = mountStep({ loading: true });
    expect(loading.text()).toContain("Checking the build tools");
    expect(loading.findAll("button")).toHaveLength(0);
    loading.unmount();

    retryNodeTools.mockClear();
    const failed = mountStep({ error: true });
    const retry = failed.findAll("button").find((button) => button.text() === "Retry")!;
    expect(retry).toBeTruthy();
    await retry.trigger("click");
    expect(retryNodeTools).toHaveBeenCalled();
    failed.unmount();
  });

  it("offers to install Railpack when no CLI engine is installed", async () => {
    installBuildPack.mockClear();
    const wrapper = mountStep({ noEngine: true });
    const cta = wrapper.findAll("button").find((button) => button.text() === "Install Railpack")!;
    expect(cta).toBeTruthy();
    await cta.trigger("click");
    expect(installBuildPack).toHaveBeenCalledWith("railpack");
    wrapper.unmount();
  });
});
