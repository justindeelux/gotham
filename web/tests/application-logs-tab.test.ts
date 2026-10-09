import { NSelect } from "naive-ui";
import { beforeEach, describe, expect, it } from "vitest";
import { mount } from "@vue/test-utils";

import ApplicationLogsTab from "@/features/applications/components/ApplicationLogsTab.vue";
import en from "@/features/applications/locales/en";
import viCatalog from "@/features/applications/locales/vi";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  setLocale,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

/** props builds tab props with a selected node and deployment. */
function props(overrides: Record<string, unknown> = {}) {
  return {
    logServerId: "server-1",
    logDeploymentId: "deploy-1",
    serverOptions: [{ label: "node-a · 10.0.0.1", value: "server-1" }],
    deploymentOptions: [{ label: "deploy-1 · deploy · running", value: "deploy-1" }],
    activeDeploymentId: "",
    logTarget: null,
    effectiveLogServerId: "server-1",
    application: null,
    runtimeDeployment: null,
    ...overrides,
  };
}

/** mountTab renders the tab with heavy children stubbed out. */
function mountTab(tabProps: Record<string, unknown>) {
  return mount(ApplicationLogsTab, {
    props: tabProps as never,
    global: {
      plugins: [i18n],
      stubs: {
        DeployLogs: { template: "<div />" },
        DeploymentCommitCard: { template: "<div />" },
        LogViewer: { template: "<div />" },
      },
    },
  });
}

describe("ApplicationLogsTab selects", () => {
  it("binds values that match an option so the selects never render blank", () => {
    const wrapper = mountTab(props());
    const selects = wrapper.findAllComponents(NSelect);
    expect(selects.length).toBeGreaterThanOrEqual(2);
    const [node, deployment] = selects;
    expect(
      (props() as { serverOptions: Array<{ value: string }> }).serverOptions.some(
        (option) => option.value === node.props("value"),
      ),
    ).toBe(true);
    expect(
      (props() as { deploymentOptions: Array<{ value: string }> }).deploymentOptions.some(
        (option) => option.value === deployment.props("value"),
      ),
    ).toBe(true);
    wrapper.unmount();
  });

  it("shows placeholders when nothing is selected yet", () => {
    const wrapper = mountTab(
      props({
        logServerId: "",
        logDeploymentId: "",
        serverOptions: [],
        deploymentOptions: [],
        activeDeploymentId: "",
      }),
    );
    for (const select of wrapper.findAllComponents(NSelect)) {
      expect(String(select.props("placeholder") ?? "")).not.toBe("");
    }
    wrapper.unmount();
  });

  it("keeps the hint short with no channel pattern, in both locales", () => {
    for (const locale of ["en", "vi"] as const) {
      setLocale(locale);
      const wrapper = mountTab(props({ logServerId: "", logDeploymentId: "" }));
      expect(wrapper.text()).not.toContain("logs:");
      wrapper.unmount();
    }
    expect(en.logsTab.hint).not.toContain("{channel}");
    expect(checkCatalogParity(en, viCatalog)).toEqual([]);
  });
});
