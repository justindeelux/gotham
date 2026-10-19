// JUS-238: compose applications declare volumes in their compose source, so
// the single-container Storage tab is hidden and a stale selection falls back
// to Overview. Image/git applications keep the tab unchanged.

import { mount } from "@vue/test-utils";
import { NMessageProvider, NTabs } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { h, ref } from "vue";

import type { Application } from "@/features/applications/api/applications";
import ApplicationOverviewTab from "@/features/applications/components/ApplicationOverviewTab.vue";
import ApplicationDetailPage from "@/features/applications/pages/ApplicationDetailPage.vue";
import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "@/shared/i18n";

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
});

/** composeApp is one stored compose source; the document itself is inert. */
function composeApp(): Application {
  return {
    id: "app-compose",
    name: "compose-demo",
    environment_id: "env-1",
    environment_name: "production",
    project_id: "proj-1",
    project_name: "shop",
    provider: "",
    repo: "",
    clone_url: "",
    source_type: "compose",
    branch: "",
    build_pack: "",
    compose_content: "services:\n  web:\n    image: nginx\n",
    compose_file: "",
    compose_service: "web",
    image_ref: "",
    has_registry_credential: false,
    primary_domain: "",
    server_id: "server-1",
    server_name: "node-1",
    created_at: "2026-09-01T10:00:00Z",
    updated_at: "2026-09-01T10:00:00Z",
  } as Application;
}

/** imageApp is a non-compose source that keeps the Storage tab. */
function imageApp(): Application {
  return {
    ...composeApp(),
    id: "app-image",
    name: "storefront",
    source_type: "image",
    compose_content: "",
    compose_service: "",
    image_ref: "registry.example.com/team/app:1.2",
    has_registry_credential: true,
  } as Application;
}

let detailApplication: Application = imageApp();
let detailTab = "overview";

vi.mock("@/features/applications/composables/useApplicationDetail", () => ({
  useApplicationDetail: () => mockDetail(),
}));

/** mockDetail stands in for the detail composable: real refs, no-op handlers. */
function mockDetail() {
  const appsStore = {
    loading: false,
    acting: false,
    error: null,
    savingEnv: false,
    savingStorages: false,
  };
  const values: Record<string, unknown> = {
    application: ref(detailApplication),
    canWrite: ref(true),
    activeTab: ref(detailTab),
    appsStore,
    deployments: ref([]),
    previews: ref([]),
    pipelineSteps: ref([]),
    envDraft: ref([]),
    storagesDraft: ref([]),
    inheritedVars: ref([]),
    serverOptions: ref([]),
    deploymentOptions: ref([]),
    latest: ref(null),
    active: ref(null),
    runningDeployments: ref([]),
    appId: ref(detailApplication.id),
    shortId: ref(detailApplication.id.slice(0, 8)),
    displayName: ref(detailApplication.name),
    initials: ref("AP"),
    descColumns: ref(2),
    controlHint: ref(null),
    containerStopped: ref(false),
    containerIsRunning: ref(false),
    logTarget: ref(null),
    effectiveLogServerId: ref(""),
    logServerId: ref(""),
    logDeploymentId: ref(""),
    envError: ref(null),
    envLoading: ref(false),
    envLoadedFor: ref(""),
    storagesError: ref(null),
    storagesLoading: ref(false),
    storagesLoadedFor: ref(""),
    inheritedReady: ref(false),
    inheritedLoading: ref(false),
    moveError: ref(null),
    moveSaving: ref(false),
    previewsAvailable: ref(false),
    previewsError: ref(null),
    previewsLoaded: ref(false),
    previewsLoading: ref(false),
    rollbackOpen: ref(false),
    rollbackTarget: ref(null),
    rollingBack: ref(false),
  };
  return new Proxy(values, {
    get(target, prop) {
      if (prop in target) {
        return target[prop as string];
      }
      return () => undefined;
    },
  });
}

function mountDetailPage() {
  setActivePinia(createPinia());
  return mount(NMessageProvider, {
    slots: { default: () => h(ApplicationDetailPage) },
    global: {
      plugins: [i18n],
      stubs: {
        ProjectBreadcrumb: true,
        ApplicationHeader: true,
        ApplicationOverviewTab: true,
        ApplicationDeploymentsTab: true,
        ApplicationLogsTab: true,
        ApplicationEnvTab: true,
        ApplicationStorageTab: true,
        ApplicationPreviewsTab: true,
        DomainEditor: true,
        RollbackDialog: true,
        ResourceMoveCard: true,
      },
    },
  });
}

/** tabLabels returns the rendered tab headers. */
function tabLabels(wrapper: ReturnType<typeof mountDetailPage>): string[] {
  return wrapper.findAll(".n-tabs-tab").map((tab) => tab.text());
}

describe("application detail storage tab", () => {
  it("keeps the Storage tab for non-compose applications", () => {
    detailApplication = imageApp();
    detailTab = "overview";
    const wrapper = mountDetailPage();
    expect(tabLabels(wrapper)).toContain("Storage");
    expect(wrapper.findComponent(NTabs).props("value")).toBe("overview");
    wrapper.unmount();
  });

  it("hides the Storage tab for compose applications", () => {
    detailApplication = composeApp();
    detailTab = "overview";
    const wrapper = mountDetailPage();
    expect(tabLabels(wrapper)).not.toContain("Storage");
    expect(tabLabels(wrapper)).toContain("Overview");
    wrapper.unmount();
  });

  it("falls back to Overview when a compose app selects Storage", () => {
    detailApplication = composeApp();
    detailTab = "storage";
    const wrapper = mountDetailPage();
    expect(wrapper.findComponent(NTabs).props("value")).toBe("overview");
    wrapper.unmount();
  });

  it("keeps a selected non-storage tab for compose applications", () => {
    detailApplication = composeApp();
    detailTab = "env";
    const wrapper = mountDetailPage();
    expect(wrapper.findComponent(NTabs).props("value")).toBe("env");
    wrapper.unmount();
  });
});

describe("compose volume hint", () => {
  it("names the compose source and the managed root", () => {
    const wrapper = mount(ApplicationOverviewTab, {
      props: {
        application: composeApp(),
        latest: null,
        deployments: [],
        pipelineSteps: [],
        descColumns: 2,
        acting: false,
      },
      global: {
        plugins: [i18n],
        stubs: { ComposeEditor: true, DeploymentHistoryTable: true },
      },
    });
    const text = wrapper.text();
    expect(text).toContain("declared in the compose source");
    expect(text).toContain("/var/lib/gotham/volumes/<appID>/<name>");
    wrapper.unmount();
  });

  it("does not render the hint for non-compose applications", () => {
    const wrapper = mount(ApplicationOverviewTab, {
      props: {
        application: imageApp(),
        latest: null,
        deployments: [],
        pipelineSteps: [],
        descColumns: 2,
        acting: false,
      },
      global: {
        plugins: [i18n],
        stubs: { DeploymentHistoryTable: true },
      },
    });
    expect(wrapper.text()).not.toContain("declared in the compose source");
    wrapper.unmount();
  });
});
