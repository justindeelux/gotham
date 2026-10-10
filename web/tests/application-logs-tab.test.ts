import { NMessageProvider, NSelect } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { createMemoryHistory, createRouter } from "vue-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick } from "vue";
import { flushPromises, mount } from "@vue/test-utils";

import ApplicationLogsTab from "@/features/applications/components/ApplicationLogsTab.vue";
import { useApplicationDetail } from "@/features/applications/composables/useApplicationDetail";
import type {
  Application,
  Deployment,
} from "@/features/applications/api/applications";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import type { Server } from "@/features/servers/api/servers";
import { useServersStore } from "@/features/servers/stores/servers";
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

interface LogProbe {
  apps: Application[];
  deployments: Record<string, Deployment[]>;
  servers: Server[];
  serversError?: unknown;
}

/** logProbe holds the canned API answers driving the detail composable. */
function logProbe(): LogProbe {
  const scope = globalThis as Record<string, unknown>;
  scope.__logProbe ??= { apps: [], deployments: {}, servers: [] };
  return scope.__logProbe as LogProbe;
}

vi.mock("@/features/applications/api/applications", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@/features/applications/api/applications")>();
  return {
    ...original,
    getApplication: async (id: string) =>
      [...logProbe().apps].find((item) => item.id === id) ?? logProbe().apps[0],
    listDeployments: async (id: string) => [...(logProbe().deployments[id] ?? [])],
  };
});

vi.mock("@/features/servers/api/servers", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@/features/servers/api/servers")>();
  return {
    ...original,
    listServers: async () => {
      if (logProbe().serversError !== undefined && logProbe().serversError !== null) {
        throw logProbe().serversError;
      }
      return [...logProbe().servers];
    },
  };
});

vi.mock("@/features/applications/api/previews", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@/features/applications/api/previews")>();
  return { ...original, listPreviews: async () => [] };
});

vi.mock("@/features/projects/api/variables", async (importOriginal) => {
  const original =
    await importOriginal<typeof import("@/features/projects/api/variables")>();
  return {
    ...original,
    getProjectVariables: async () => [],
    getEnvironmentVariables: async () => [],
  };
});

/** probe holds the canned API answers driving the detail composable. */
const probe: LogProbe = logProbe();

/** dep builds a deployment row with only id/state varying. */
function dep(id: string, state: Deployment["state"]): Deployment {
  return {
    id,
    application_id: "app-1",
    kind: "deploy",
    state,
    image_tag: "",
    registry_image: "",
    digest: "",
    error: "",
    attempt: 1,
    container_id: "",
    rollback_from: "",
    started_at: null,
    finished_at: null,
    created_at: "",
    updated_at: "",
  };
}

/** app builds an application row with only id/node varying. */
function app(id: string, serverId: string | null): Application {
  return {
    id,
    name: id,
    environment_id: "env-1",
    environment_name: "env",
    project_id: "proj-1",
    project_name: "proj",
    provider: "dockerfile",
    repo: "",
    clone_url: "",
    source_type: "dockerfile",
    github_app_id: "",
    branch: "main",
    build_pack: "",
    image_ref: "",
    has_registry_credential: false,
    base_domain: "",
    base_domain_disabled: false,
    port: 3000,
    host_port: 0,
    server_id: serverId,
    server_name: "",
    created_at: "",
    updated_at: "",
  };
}

/** srv builds a node row with only id varying. */
function srv(id: string): Server {
  return {
    id,
    name: id,
    ip: "10.0.0.1",
    port: 22,
    ssh_user: "root",
    ssh_key_id: null,
    has_password: false,
    status: "connected",
    node_id: null,
    os: null,
    docker_version: null,
    arch: null,
    total_mem: null,
    total_disk: null,
    cpu_usage: null,
    mem_usage: null,
    disk_usage: null,
    container_count: null,
    last_seen: null,
    created_at: "",
    updated_at: "",
  };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  probe.apps = [];
  probe.deployments = {};
  probe.servers = [];
  probe.serversError = null;
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
  it("renders exactly the displayed picks the parent computed", () => {
    const tabProps = props();
    const wrapper = mountTab(tabProps);
    const selects = wrapper.findAllComponents(NSelect);
    expect(selects).toHaveLength(2);
    expect(selects[0].props("value")).toBe(tabProps.logServerId);
    expect(selects[1].props("value")).toBe(tabProps.logDeploymentId);
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

  it("lays the commit card on the picks row with the selects stacked", () => {
    const wrapper = mountTab(props());
    const head = wrapper.find(".deploy-log-head");
    expect(head.exists()).toBe(true);
    expect(head.find(".deploy-log-picks").exists()).toBe(true);
    expect(head.findAllComponents(NSelect)).toHaveLength(2);
    expect(head.find(".deploy-log-commit").exists()).toBe(true);
    wrapper.unmount();
  });

  it("keeps the hint short with no channel pattern, in both locales", () => {
    for (const locale of ["en", "vi"] as const) {
      setLocale(locale);
      const wrapper = mountTab(props({ logServerId: "", logDeploymentId: "" }));
      expect(wrapper.text()).not.toContain("logs:");
      expect(wrapper.text()).toContain(
        String(i18n.global.t("applications.logsTab.hint")),
      );
      wrapper.unmount();
    }
    expect(en.logsTab.hint).not.toContain("{channel}");
    expect(en.logsTab.hint).not.toContain("logs:");
    expect(checkCatalogParity(en, viCatalog)).toEqual([]);
  });
});

/** mountDetail mounts the real composable behind a message provider. */
async function mountDetail(appId: string) {
  setActivePinia(createPinia());
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        name: "application-detail",
        path: "/p/:projectId/e/:environmentId/app/:id",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push({
    name: "application-detail",
    params: { projectId: "proj-1", environmentId: "env-1", id: appId },
  });
  const Host = defineComponent({
    setup() {
      const detail = useApplicationDetail();
      (globalThis as Record<string, unknown>).__detail = detail;
      return () => h("div");
    },
  });
  const shell = defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(Host) });
    },
  });
  const wrapper = mount(shell, { global: { plugins: [router, i18n] } });
  await flushPromises();
  const detail = (globalThis as Record<string, unknown>).__detail as ReturnType<
    typeof useApplicationDetail
  >;
  return { wrapper, router, detail };
}

/** stopTimers tears down polling so no test leaks an interval. */
function stopTimers() {
  try {
    useApplicationsStore().stopAllPolling();
  } catch {
    // The store was never created; nothing polls.
  }
}

describe("useApplicationDetail Logs tab derived defaults", () => {
  it("follows active/latest as late lists arrive without storing a pick", async () => {
    probe.apps = [app("app-1", "server-1")];
    probe.deployments = { "app-1": [] };
    probe.servers = [];
    const { wrapper, detail } = await mountDetail("app-1");
    try {
      expect(detail.displayedLogDeploymentId.value).toBe("");
      expect(detail.logDeploymentId.value).toBe("");
      probe.deployments = { "app-1": [dep("deploy-1", "failed"), dep("deploy-2", "building")] };
      probe.servers = [srv("server-1")];
      await useApplicationsStore().fetchDeployments("app-1");
      await useServersStore().fetchServers();
      await nextTick();
      expect(detail.logDeploymentId.value).toBe("");
      expect(detail.displayedLogDeploymentId.value).toBe("deploy-2");
      expect(detail.logServerId.value).toBe("");
      expect(detail.displayedLogServerId.value).toBe("server-1");
    } finally {
      stopTimers();
      wrapper.unmount();
    }
  });

  it("preserves an explicit pick across refreshes and resolves its target", async () => {
    probe.apps = [app("app-1", "server-1")];
    probe.deployments = { "app-1": [dep("deploy-old", "failed"), dep("deploy-new", "failed")] };
    probe.servers = [srv("server-1")];
    const { wrapper, detail } = await mountDetail("app-1");
    try {
      detail.logDeploymentId.value = "deploy-old";
      probe.deployments = {
        "app-1": [dep("deploy-newer", "failed"), dep("deploy-new", "failed"), dep("deploy-old", "failed")],
      };
      await useApplicationsStore().fetchDeployments("app-1");
      await nextTick();
      expect(detail.displayedLogDeploymentId.value).toBe("deploy-old");
      expect(detail.logTarget.value?.id).toBe("deploy-old");
    } finally {
      stopTimers();
      wrapper.unmount();
    }
  });

  it("follows a newly arrived deployment while no pick is stored", async () => {
    probe.apps = [app("app-1", "server-1")];
    probe.deployments = { "app-1": [dep("deploy-1", "failed")] };
    probe.servers = [srv("server-1")];
    const { wrapper, detail } = await mountDetail("app-1");
    try {
      expect(detail.displayedLogDeploymentId.value).toBe("deploy-1");
      probe.deployments = { "app-1": [dep("deploy-2", "building"), dep("deploy-1", "failed")] };
      await useApplicationsStore().fetchDeployments("app-1");
      await nextTick();
      expect(detail.logDeploymentId.value).toBe("");
      expect(detail.displayedLogDeploymentId.value).toBe("deploy-2");
      expect(detail.logTarget.value?.id).toBe("deploy-2");
    } finally {
      stopTimers();
      wrapper.unmount();
    }
  });

  it("resets the explicit pick when the application changes", async () => {
    probe.apps = [app("app-1", "server-1"), app("app-2", "server-2")];
    probe.deployments = {
      "app-1": [dep("deploy-1", "failed")],
      "app-2": [{ ...dep("deploy-9", "failed"), application_id: "app-2" }],
    };
    probe.servers = [srv("server-1"), srv("server-2")];
    const { wrapper, router, detail } = await mountDetail("app-1");
    try {
      detail.logDeploymentId.value = "deploy-1";
      detail.logServerId.value = "server-1";
      await router.push({
        name: "application-detail",
        params: { projectId: "proj-1", environmentId: "env-1", id: "app-2" },
      });
      await flushPromises();
      await nextTick();
      expect(detail.logDeploymentId.value).toBe("");
      expect(detail.logServerId.value).toBe("");
      expect(detail.displayedLogDeploymentId.value).toBe("deploy-9");
      expect(detail.displayedLogServerId.value).toBe("server-2");
    } finally {
      stopTimers();
      wrapper.unmount();
    }
  });

  it("streams the application node when it is missing from the server list", async () => {
    probe.apps = [app("app-1", "ghost-node")];
    probe.deployments = { "app-1": [dep("deploy-1", "failed")] };
    probe.servers = [srv("server-1"), srv("server-2")];
    const { wrapper, detail } = await mountDetail("app-1");
    try {
      expect(detail.logServerId.value).toBe("");
      expect(detail.displayedLogServerId.value).toBe("server-1");
      expect(detail.effectiveLogServerId.value).toBe("ghost-node");
    } finally {
      stopTimers();
      wrapper.unmount();
    }
  });

  it("streams the application node while the server list has not loaded yet", async () => {
    probe.apps = [app("app-1", "server-1")];
    probe.deployments = { "app-1": [dep("deploy-1", "failed")] };
    probe.servers = [];
    const { wrapper, detail } = await mountDetail("app-1");
    try {
      expect(useServersStore().servers).toHaveLength(0);
      expect(detail.displayedLogServerId.value).toBe("");
      expect(detail.effectiveLogServerId.value).toBe("server-1");
    } finally {
      stopTimers();
      wrapper.unmount();
    }
  });

  it("keeps streaming the application node when the server list fetch fails", async () => {
    probe.apps = [app("app-1", "server-1")];
    probe.deployments = { "app-1": [dep("deploy-1", "failed")] };
    probe.servers = [];
    probe.serversError = new Error("nodes unreachable");
    const { wrapper, detail } = await mountDetail("app-1");
    try {
      expect(useServersStore().servers).toHaveLength(0);
      expect(detail.displayedLogServerId.value).toBe("");
      expect(detail.effectiveLogServerId.value).toBe("server-1");
    } finally {
      stopTimers();
      wrapper.unmount();
    }
  });

  it("follows a node move while no pick is stored", async () => {
    probe.apps = [app("app-1", "server-1")];
    probe.deployments = { "app-1": [dep("deploy-1", "failed")] };
    probe.servers = [srv("server-1"), srv("server-2")];
    const { wrapper, detail } = await mountDetail("app-1");
    try {
      expect(detail.displayedLogServerId.value).toBe("server-1");
      useApplicationsStore().applicationsById["app-1"] = app("app-1", "server-2");
      await nextTick();
      expect(detail.logServerId.value).toBe("");
      expect(detail.displayedLogServerId.value).toBe("server-2");
      expect(detail.effectiveLogServerId.value).toBe("server-2");
    } finally {
      stopTimers();
      wrapper.unmount();
    }
  });
});
