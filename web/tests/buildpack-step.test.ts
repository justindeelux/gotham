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
const markBuildPackTouched = vi.fn();

interface StepOptions {
  choices?: BuildPackChoice[];
  loading?: boolean;
  error?: boolean;
  noEngine?: boolean;
  buildPack?: string;
  scan?: {
    loading?: boolean;
    ran?: boolean;
    failed?: boolean;
    detectedPack?: string;
    markerFile?: string;
    composeFiles?: string[];
    composeFile?: string;
    composeServices?: string[];
    directories?: string[];
  };
}

function mountStep(options: StepOptions = {}) {
  const form = reactive({
    buildPack: options.buildPack ?? "",
    serverId: "srv-1",
    workingDir: "",
  });
  const state = {
    form,
    isCompose: computed(() => false),
    isComposePack: computed(() => form.buildPack === "docker_compose"),
    isDockerfile: computed(() => false),
    nodeToolsLoading: computed(() => options.loading ?? false),
    nodeToolsError: computed(() => options.error ?? false),
    noBuildEngine: computed(() => options.noEngine ?? false),
    buildPackChoices: computed(() => options.choices ?? []),
    scan: {
      loading: options.scan?.loading ?? false,
      ran: options.scan?.ran ?? false,
      failed: options.scan?.failed ?? false,
      detectedPack: options.scan?.detectedPack ?? "",
      markerFile: options.scan?.markerFile ?? "",
      composeFiles: options.scan?.composeFiles ?? [],
      composeFile: options.scan?.composeFile ?? "",
      composeServices: options.scan?.composeServices ?? [],
    },
    scanComposeFileOptions: computed(() =>
      (options.scan?.composeFiles ?? []).map((name) => ({ label: name, value: name })),
    ),
    scanDirectoryOptions: computed(() => [
      { label: "Repository root", value: "" },
      ...(options.scan?.directories ?? []).map((dir) => ({ label: dir, value: dir })),
    ]),
    scannedComposeServiceOptions: computed(() =>
      (options.scan?.composeServices ?? []).map((name) => ({ label: name, value: name })),
    ),
    installBuildPack,
    retryNodeTools,
    subscribeNodeTools,
    markBuildPackTouched,
  };
  const Harness = defineComponent({
    setup() {
      provideCreateWizard(state as never);
      return () => h(WizardBuildPackStep);
    },
  });
  return mount(Harness, { global: { plugins: [i18n] } });
}

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

describe("repository scan hints", () => {
  it("shows the scan state without blocking the pack choice", () => {
    const loading = mountStep({ scan: { loading: true } });
    expect(loading.text()).toContain("Scanning the repository root");
    expect(loading.find(".n-radio-group").exists()).toBe(true);
    loading.unmount();

    const found = mountStep({ scan: { ran: true, markerFile: "docker-compose.yml" } });
    expect(found.text()).toContain("Found docker-compose.yml in the repository root");
    found.unmount();

    const empty = mountStep({ scan: { ran: true } });
    expect(empty.text()).toContain("No build markers were found");
    empty.unmount();

    const failed = mountStep({ scan: { failed: true } });
    expect(failed.text()).toContain("Couldn't scan the repository");
    expect(failed.text()).toContain("not blocked");
    failed.unmount();
  });

  it("offers the scanned folders as working directories", () => {
    const wrapper = mountStep({
      scan: { ran: true, markerFile: "package.json", directories: ["apps", "apps/web"] },
    });
    const select = wrapper.find(".app-working-dir");
    expect(select.exists()).toBe(true);
    const options = select.findComponent({ name: "Select" }).props("options") as Array<{
      value: string;
    }>;
    expect(options.map((option) => option.value)).toEqual(["", "apps", "apps/web"]);
    expect(wrapper.text()).toContain("Repository root");
    wrapper.unmount();
  });

  it("uses the scanned compose files and services as selectors", () => {
    const wrapper = mountStep({
      buildPack: "docker_compose",
      scan: {
        ran: true,
        markerFile: "docker-compose.yml",
        composeFiles: ["docker-compose.yml", "compose.yaml"],
        composeFile: "docker-compose.yml",
        composeServices: ["web", "worker"],
      },
    });
    const fields = wrapper.find(".app-compose-pack-fields");
    expect(fields.findAll(".n-select")).toHaveLength(2);
    expect(fields.text()).toContain("Services read from docker-compose.yml.");
    wrapper.unmount();
  });

  it("keeps the free-text fields when the scan returned no compose data", () => {
    const wrapper = mountStep({ buildPack: "docker_compose", scan: { ran: true } });
    const fields = wrapper.find(".app-compose-pack-fields");
    expect(fields.findAll(".n-select")).toHaveLength(0);
    expect(fields.findAll("input")).toHaveLength(2);
    wrapper.unmount();
  });
});
