// JUS-247: the application detail Terminal tab. Every seam is a faithful fake
// (targets list, container create/close, host create/close and the renderer)
// that can fail, delay or reject, so the tests assert request counts, the exact
// target identity and session ids — not only rendered text.
/* global document:readonly, HTMLElement:readonly, setTimeout:readonly, window:readonly */
import { NSelect, NTabs } from "naive-ui";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import type { Pinia } from "pinia";
import { defineComponent, h, nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  i18n,
  registerDiscoveredCatalogs,
  resetLocaleState,
  syncComposerLocale,
} from "../src/shared/i18n";
import { checkCatalogParity } from "@/shared/i18n/catalog";
import enTerminal from "../src/features/terminal/locales/en";
import viTerminal from "../src/features/terminal/locales/vi";
import enApplications from "../src/features/applications/locales/en";
import viApplications from "../src/features/applications/locales/vi";

/** runtimeFakes records every renderer the store creates. */
const runtimeFakes = vi.hoisted(() => {
  class FakeRuntime {
    options: {
      onState: (_patch: Record<string, unknown>) => void;
      session: { id: string };
      banner?: unknown;
    };
    host: HTMLElement;
    attached: HTMLElement | null = null;
    disposed = false;
    connected = false;

    constructor(options: FakeRuntime["options"]) {
      this.options = options;
      this.host = document.createElement("div");
      this.host.className = "term-host";
      instances.push(this);
    }

    async connect(): Promise<void> {
      this.connected = true;
      this.options.onState({ status: "connected", shell: "/bin/sh" });
    }

    attach(container: HTMLElement): void {
      this.attached = container;
      container.appendChild(this.host);
    }

    focus(): void {}
    fit(): void {}
    setTheme(): void {}

    dispose(): void {
      this.disposed = true;
      this.host.remove();
    }

    emit(patch: Record<string, unknown>): void {
      this.options.onState(patch);
    }
  }
  const instances: FakeRuntime[] = [];
  return { FakeRuntime, instances };
});

/** apiFakes is the terminal HTTP seam: every call records its exact target. */
const apiFakes = vi.hoisted(() => ({
  listTargets: vi.fn(),
  create: vi.fn(),
  close: vi.fn(),
  hostCreate: vi.fn(),
  hostClose: vi.fn(),
}));

vi.mock("@/features/terminal/transport/terminalRuntime", () => ({
  TerminalRuntime: runtimeFakes.FakeRuntime,
}));

/** classify mirrors the real application classifier for the fake seam. */
function classify(error: unknown): { code: string; message: string } {
  const api = error as { status?: number; message?: string } | null;
  const status = api?.status ?? null;
  const message = api?.message ?? "";
  const text = message.toLowerCase();
  if (status === 403) return { code: "applicationForbidden", message };
  if (status === 404) {
    return {
      code: text.includes("container") ? "containerGone" : "applicationNotFound",
      message,
    };
  }
  if (status === 409) {
    if (text.includes("no running deployment"))
      return { code: "noDeployment", message };
    if (text.includes("deployment is in progress"))
      return { code: "deployInFlight", message };
    if (text.includes("not running"))
      return { code: "containerStopped", message };
    return { code: "generic", message };
  }
  if (status === 429) return { code: "quota", message };
  if (status === 502) return { code: "unavailable", message };
  if (status === 503) return { code: "terminalDisabled", message };
  return { code: status === null ? "network" : "generic", message };
}

vi.mock("@/features/terminal/api/terminal", () => ({
  applicationTerminalFailure: (error: unknown) => classify(error),
  createApplicationTerminalSession: apiFakes.create,
  closeApplicationTerminalSession: apiFakes.close,
  createTerminalSession: apiFakes.hostCreate,
  closeTerminalSession: apiFakes.hostClose,
  currentTerminalToken: () => "jwt-token",
  terminalFailure: () => ({ code: "generic", message: "start failed" }),
  terminalStreamFailureCode: () => null,
  terminalWebSocketUrl: (path: string) => `ws://test${path}`,
  terminalSubprotocols: (token: string) => [
    "gotham.terminal.v1",
    `gotham.token.${token}`,
  ],
  listApplicationTerminalTargets: apiFakes.listTargets,
}));

const APP_ID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const APP_B_ID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb";
const PROJECT_ID = "11111111-1111-4111-8111-111111111111";
const ENV_ID = "22222222-2222-4222-8222-222222222222";
const SERVER_ID = "33333333-3333-4333-8333-333333333333";

const team = (role: "owner" | "admin" | "read_only") => ({
  id: "team-1",
  name: "Acme",
  is_personal: false,
  role,
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
});

const application = {
  id: APP_ID,
  name: "shop",
  environment_id: ENV_ID,
  environment_name: "production",
  project_id: PROJECT_ID,
  project_name: "storefront",
  provider: "github",
  repo: "acme/shop",
  clone_url: "https://github.com/acme/shop.git",
  source_type: "compose",
  github_app_id: "",
  branch: "main",
  build_pack: "auto",
  image_ref: "",
  has_registry_credential: false,
  primary_domain: "",
  server_id: SERVER_ID,
  server_name: "prod-01",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

const deployment = {
  id: "dep-1",
  application_id: APP_ID,
  kind: "deploy",
  state: "running",
  image_tag: "shop:1",
  registry_image: "registry/shop:1",
  digest: "sha256:1",
  error: "",
  attempt: 1,
  container_id: "container-1",
  rollback_from: "",
  created_at: "2026-10-01T00:00:00Z",
  updated_at: "2026-10-01T00:00:00Z",
};

/** target builds one discovery row with the API's deterministic label. */
function target(
  service: string,
  containerId: string,
  name: string,
  replica = 1,
  image = "nginx:1",
) {
  return {
    service,
    container_id: containerId,
    name,
    image,
    state: "running",
    replica,
    label: service === "" ? name : `${service} · ${name}`,
  };
}

/** targetsResponse wraps rows in the discovery envelope. */
function targetsResponse(
  containers: ReturnType<typeof target>[],
  sourceType = "compose",
) {
  return {
    application_id: APP_ID,
    application_name: "shop",
    server_id: SERVER_ID,
    source_type: sourceType,
    containers,
  };
}

const SINGLE = [target("", "cid-single", "gotham-shop-1")];
const COMPOSE = [
  target("db", "cid-db", "gotham-shop-db-1", 1, "postgres:16"),
  target("web", "cid-web-1", "gotham-shop-web-1", 1, "nginx:1"),
  target("web", "cid-web-2", "gotham-shop-web-2", 2, "nginx:1"),
];

/** applicationApiFakes replaces application reads with deterministic rows. */
const applicationApiFakes = vi.hoisted(() => ({
  getApplication: vi.fn(),
  listDeployments: vi.fn(),
  getEnv: vi.fn(),
  getStorages: vi.fn(),
  listPreviews: vi.fn(),
}));

vi.mock(
  "@/features/applications/api/applications",
  async (importOriginal) => {
    const actual =
      await importOriginal<
        typeof import("@/features/applications/api/applications")
      >();
    return {
      ...actual,
      getApplication: applicationApiFakes.getApplication,
      listDeployments: applicationApiFakes.listDeployments,
      getEnv: applicationApiFakes.getEnv,
      getStorages: applicationApiFakes.getStorages,
      listPreviews: applicationApiFakes.listPreviews,
    };
  },
);

vi.mock("@/features/applications/api/previews", async (importOriginal) => {
  const actual =
    await importOriginal<
      typeof import("@/features/applications/api/previews")
    >();
  return { ...actual, listPreviews: applicationApiFakes.listPreviews };
});

/** teamsApiFakes lets a test make the role read fail or hang. */
const teamsApiFakes = vi.hoisted(() => ({ listTeams: vi.fn() }));

vi.mock("@/features/teams/api/teams", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/teams/api/teams")>();
  return { ...actual, listTeams: teamsApiFakes.listTeams };
});

vi.mock("@/features/projects/api/variables", () => ({
  getEnvironmentVariables: vi.fn().mockResolvedValue([]),
  getProjectVariables: vi.fn().mockResolvedValue([]),
}));

vi.mock("@/features/servers/api/servers", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/servers/api/servers")>();
  return { ...actual, listServers: vi.fn().mockResolvedValue([]) };
});

// useMessage needs the app shell's NMessageProvider; tests only need the shape.
vi.mock("naive-ui", async (importOriginal) => {
  const actual = await importOriginal<typeof import("naive-ui")>();
  return {
    ...actual,
    useMessage: () => ({
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      info: vi.fn(),
    }),
  };
});

const { useTerminalStore } = await import("@/features/terminal/stores/terminal");
const { useTerminalWindowStore } = await import(
  "@/features/terminal/stores/terminalWindow"
);
const { useAuthStore } = await import("@/features/auth");
const { useTeamsStore } = await import("@/features/teams");
const { useApplicationsStore } = await import(
  "@/features/applications/stores/applications"
);
const { useTerminalAccessBoundary } = await import(
  "../src/app/layouts/useTerminalAccessBoundary"
);
const { default: ApplicationDetailPage } = await import(
  "../src/features/applications/pages/ApplicationDetailPage.vue"
);
const { default: TerminalPageDock } = await import(
  "../src/features/terminal/components/TerminalPageDock.vue"
);

/** stub is the lazy route component used outside the page under test. */
const stub = { template: "<div />" };

/** pageRouter is the minimal route table (names match the app router). */
function pageRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/dashboard", name: "dashboard", component: stub },
      { path: "/terminal", name: "terminal", component: stub },
      { path: "/login", name: "login", component: stub },
      { path: "/forbidden", name: "forbidden", component: stub },
      {
        path: "/projects/:projectId/environments/:environmentId/applications/:id",
        name: "application-detail",
        component: stub,
      },
    ],
  });
}

let seq = 0;

/** labelFor resolves the discovered label a session echoes. */
function labelFor(containerId: string): string {
  if (containerId === "cid-new") {
    return "gotham-shop-2";
  }
  if (containerId === "cid-blog") {
    return "gotham-blog-1";
  }
  const row = [...SINGLE, ...COMPOSE].find(
    (item) => item.container_id === containerId,
  );
  return row?.label ?? containerId;
}

function sessionFor(
  applicationId: string,
  input: { service: string; containerId: string; cols: number; rows: number },
) {
  seq += 1;
  return {
    id: `session-${seq}`,
    server_id: SERVER_ID,
    target: "container",
    cols: input.cols,
    rows: input.rows,
    expires_at: "2026-10-20T12:01:00Z",
    websocket_path: `/api/v1/applications/${applicationId}/terminal/sessions/session-${seq}/ws`,
    subprotocol: "gotham.terminal.v1",
    application_id: applicationId,
    // Real backend shape: a single-container app omits `service`.
    ...(input.service !== "" ? { service: input.service } : {}),
    container_id: input.containerId,
    container_name: labelFor(input.containerId).split(" · ").at(-1),
    target_label: labelFor(input.containerId),
  };
}

beforeEach(() => {
  registerDiscoveredCatalogs();
  resetLocaleState();
  syncComposerLocale("en");
  document.body.innerHTML = "";
  window.localStorage.clear();
  setActivePinia(createPinia());
  useTerminalStore().reset();
  runtimeFakes.instances.length = 0;
  seq = 0;
  apiFakes.listTargets.mockReset();
  apiFakes.create.mockReset();
  apiFakes.close.mockReset();
  apiFakes.hostCreate.mockReset();
  apiFakes.hostClose.mockReset();
  apiFakes.close.mockResolvedValue(undefined);
  apiFakes.hostClose.mockResolvedValue(undefined);
  apiFakes.listTargets.mockImplementation(async () =>
    targetsResponse(SINGLE, "image"),
  );
  apiFakes.create.mockImplementation(
    async (
      applicationId: string,
      input: { service: string; containerId: string; cols: number; rows: number },
    ) => sessionFor(applicationId, input),
  );
  applicationApiFakes.getApplication.mockReset();
  applicationApiFakes.getApplication.mockResolvedValue(application);
  applicationApiFakes.listDeployments.mockReset();
  applicationApiFakes.listDeployments.mockResolvedValue([deployment]);
  applicationApiFakes.getEnv.mockReset();
  applicationApiFakes.getEnv.mockResolvedValue([]);
  applicationApiFakes.getStorages.mockReset();
  applicationApiFakes.getStorages.mockResolvedValue([]);
  applicationApiFakes.listPreviews.mockReset();
  applicationApiFakes.listPreviews.mockResolvedValue([]);
  teamsApiFakes.listTeams.mockReset();
  teamsApiFakes.listTeams.mockResolvedValue([]);
});

afterEach(() => {
  useTerminalStore().reset();
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

/** login signs in a non-operator account (a team owner needs no operator bit). */
function login(pinia: Pinia, isAdmin = false): void {
  setActivePinia(pinia);
  const auth = useAuthStore();
  auth.accessToken = "test-access";
  auth.refreshToken = "test-refresh";
  auth.user = {
    id: "u-1",
    email: "ada@gotham.dev",
    display_name: "Ada",
    created_at: "2026-01-01T00:00:00Z",
    is_platform_admin: isAdmin,
  };
}

/** mountDetail mounts the application detail page with heavy tabs stubbed. */
async function mountDetail(
  path = `/projects/${PROJECT_ID}/environments/${ENV_ID}/applications/${APP_ID}?tab=terminal`,
  role: "owner" | "admin" | "read_only" | null = "owner",
  existingPinia?: Pinia,
  teamsFailure: "error" | "hang" | null = null,
) {
  const pinia = existingPinia ?? createPinia();
  setActivePinia(pinia);
  login(pinia);
  const teams = useTeamsStore();
  teams.activeTeamId = "team-1";
  teams.teams = role === null || teamsFailure !== null ? [] : [team(role)];
  if (teamsFailure === "error") {
    teamsApiFakes.listTeams.mockRejectedValueOnce({
      status: 500,
      message: "teams unavailable",
    });
    await teams.fetchTeams().catch(() => undefined);
  } else if (teamsFailure === "hang") {
    teamsApiFakes.listTeams.mockImplementationOnce(
      () => new Promise(() => undefined),
    );
    void teams.fetchTeams();
  }
  const router = pageRouter();
  await router.push(path);
  await router.isReady();
  const wrapper = mount(ApplicationDetailPage, {
    attachTo: document.body,
    global: {
      plugins: [pinia, router, i18n],
      stubs: {
        ApplicationHeader: true,
        ApplicationOverviewTab: true,
        ApplicationImagePanel: true,
        ApplicationDeploymentsTab: true,
        ApplicationLogsTab: true,
        ApplicationEnvTab: true,
        ApplicationStorageTab: true,
        ApplicationPreviewsTab: true,
        DomainEditor: true,
        ProjectBreadcrumb: true,
        ResourceMoveCard: true,
        ApplicationDeleteDialog: true,
        RollbackDialog: true,
      },
    },
  });
  await flushPromises();
  await nextTick();
  return { wrapper, router, pinia, terminal: useTerminalStore(), teams };
}

/** tabButton finds one detail tab by its visible label. */
function tabButton(wrapper: VueWrapper, label: string) {
  return wrapper.findAll(".n-tabs-tab").find((node) => node.text() === label)!;
}

/** selectValue emits an explicit service selection. */
async function selectValue(wrapper: VueWrapper, key: string) {
  wrapper.findComponent(NSelect).vm.$emit("update:value", key);
  await flushPromises();
  await nextTick();
}

describe("application terminal catalogs (JUS-247)", () => {
  it("keeps EN/VI parity and the bounded container errors", () => {
    expect(checkCatalogParity(enTerminal, viTerminal)).toEqual([]);
    expect(checkCatalogParity(enApplications, viApplications)).toEqual([]);
    for (const code of [
      "applicationForbidden",
      "applicationNotFound",
      "noDeployment",
      "deployInFlight",
      "containerStopped",
      "containerGone",
      "containerNoShell",
      "terminalDisabled",
    ]) {
      expect((enTerminal.errors as Record<string, string>)[code]).toBeTruthy();
      expect((viTerminal.errors as Record<string, string>)[code]).toBeTruthy();
    }
    for (const key of [
      "tab",
      "loading",
      "serviceLabel",
      "chooseService",
      "selectTitle",
      "selectBody",
      "emptyTitle",
      "emptyBody",
      "refresh",
      "retry",
      "notAllowedTitle",
      "notAllowedBody",
    ]) {
      expect(
        (enApplications.terminal as Record<string, string>)[key],
      ).toBeTruthy();
      expect(
        (viApplications.terminal as Record<string, string>)[key],
      ).toBeTruthy();
    }
  });
});

describe("application terminal tab ordering and laziness (JUS-247)", () => {
  it("places Terminal immediately after Environment and creates nothing before activation", async () => {
    const { wrapper } = await mountDetail(
      `/projects/${PROJECT_ID}/environments/${ENV_ID}/applications/${APP_ID}`,
    );
    const labels = wrapper.findAll(".n-tabs-tab").map((node) => node.text());
    const env = labels.indexOf("Environment");
    expect(env).toBeGreaterThanOrEqual(0);
    expect(labels.indexOf("Terminal")).toBe(env + 1);

    // Merely loading the page (and the other tabs) never discovers or creates.
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(0);
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    expect(apiFakes.hostCreate).toHaveBeenCalledTimes(0);

    await tabButton(wrapper, "Terminal").trigger("click");
    await flushPromises();
    await nextTick();
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);
    expect(apiFakes.listTargets.mock.calls[0]?.[0]).toBe(APP_ID);
    expect(apiFakes.hostCreate).toHaveBeenCalledTimes(0);
    wrapper.unmount();
  });

  it("auto-opens the only container of an image application with its exact target", async () => {
    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const [applicationId, input] = apiFakes.create.mock.calls[0]!;
    expect(applicationId).toBe(APP_ID);
    expect(input).toMatchObject({
      service: "",
      containerId: "cid-single",
      cols: 120,
      rows: 32,
    });
    // A container tab never dials the host create route.
    expect(apiFakes.hostCreate).toHaveBeenCalledTimes(0);

    const tab = terminal.pageTabs[0]!;
    expect(tab.target).toBe("container");
    expect(tab.applicationId).toBe(APP_ID);
    expect(tab.service).toBe("");
    expect(tab.containerId).toBe("cid-single");
    expect(tab.targetLabel).toBe("gotham-shop-1");
    expect(tab.title).toBe("shop · gotham-shop-1");
    expect(tab.sourcePath).toContain("tab=terminal");
    expect(tab.sessionId).toBe("session-1");
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    expect(wrapper.text()).toContain("gotham-shop-1");
    wrapper.unmount();
  });

  it("auto-opens the only container of a Dockerfile application", async () => {
    applicationApiFakes.getApplication.mockResolvedValue({
      ...application,
      source_type: "dockerfile",
    });
    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(apiFakes.create.mock.calls[0]?.[1]).toMatchObject({
      service: "",
      containerId: "cid-single",
    });
    expect(terminal.pageTabs[0]!.target).toBe("container");
    wrapper.unmount();
  });

  it("refreshes the same list across rapid tab flapping without duplicates", async () => {
    let release!: (_value: unknown) => void;
    const gate = new Promise((resolve) => {
      release = resolve;
    });
    apiFakes.listTargets.mockImplementationOnce(async () => {
      await gate;
      return targetsResponse(SINGLE, "image");
    });

    const { wrapper, terminal } = await mountDetail(
      `/projects/${PROJECT_ID}/environments/${ENV_ID}/applications/${APP_ID}?tab=terminal`,
    );
    for (let index = 0; index < 4; index += 1) {
      await tabButton(wrapper, "Overview").trigger("click");
      await flushPromises();
      await tabButton(wrapper, "Terminal").trigger("click");
      await flushPromises();
    }
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);

    release(undefined);
    await flushPromises();
    await nextTick();
    await flushPromises();
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(terminal.pageTabs).toHaveLength(1);
    wrapper.unmount();
  });
});

describe("multi-service discovery and explicit selection (JUS-247)", () => {
  beforeEach(() => {
    apiFakes.listTargets.mockResolvedValue(targetsResponse(COMPOSE));
  });

  it("creates no session until a service/replica is chosen, then uses the exact id", async () => {
    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    expect(wrapper.find(".app-term-select-card").exists()).toBe(true);

    const options = wrapper.findComponent(NSelect).props("options") as Array<{
      label: string;
      value: string;
    }>;
    // Deterministic API order, replicas visibly distinct.
    expect(options.map((option) => option.label)).toEqual([
      "db · gotham-shop-db-1",
      "web · gotham-shop-web-1",
      "web · gotham-shop-web-2",
    ]);

    await selectValue(wrapper, "web\u0000cid-web-2");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(apiFakes.create.mock.calls[0]?.[1]).toMatchObject({
      service: "web",
      containerId: "cid-web-2",
    });
    const tab = terminal.pageTabs[0]!;
    expect(tab.service).toBe("web");
    expect(tab.containerId).toBe("cid-web-2");
    expect(tab.targetLabel).toBe("web · gotham-shop-web-2");
    wrapper.unmount();
  });

  it("refreshing a live list never spawns a duplicate session", async () => {
    const { wrapper, terminal } = await mountDetail();
    await selectValue(wrapper, "web\u0000cid-web-1");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const first = terminal.pageTabs[0]!;

    await wrapper.find(".app-term-refresh").trigger("click");
    await flushPromises();
    await nextTick();

    expect(apiFakes.listTargets).toHaveBeenCalledTimes(2);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(terminal.pageTabs).toHaveLength(1);
    expect(terminal.pageTabs[0]!.id).toBe(first.id);
    expect(terminal.pageTabs[0]!.sessionId).toBe(first.sessionId);
    wrapper.unmount();
  });

  it("switching service cancels safely and confirms exactly one release", async () => {
    const { wrapper, terminal } = await mountDetail();
    await selectValue(wrapper, "web\u0000cid-web-1");
    const first = terminal.pageTabs[0]!;

    // Cancel keeps the current shell and restores the visible selection.
    await selectValue(wrapper, "db\u0000cid-db");
    expect(terminal.closeRequest?.ids).toEqual([first.id]);
    terminal.cancelClose();
    await flushPromises();
    await nextTick();
    expect(terminal.findTab(first.id)).not.toBeNull();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(apiFakes.close).toHaveBeenCalledTimes(0);
    expect(wrapper.findComponent(NSelect).props("value")).toBe(
      "web\u0000cid-web-1",
    );

    // Confirm releases exactly the old container session on the app route.
    await selectValue(wrapper, "db\u0000cid-db");
    terminal.confirmClose();
    await flushPromises();
    await nextTick();
    expect(apiFakes.close).toHaveBeenCalledTimes(1);
    expect(apiFakes.close.mock.calls[0]).toEqual([APP_ID, first.sessionId]);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(apiFakes.create.mock.calls[1]?.[1]).toMatchObject({
      service: "db",
      containerId: "cid-db",
    });
    expect(terminal.pageTabs).toHaveLength(1);
    expect(terminal.pageTabs[0]!.containerId).toBe("cid-db");
    expect(apiFakes.hostClose).toHaveBeenCalledTimes(0);
    wrapper.unmount();
  });
});

describe("application terminal failure states (JUS-247)", () => {
  it("shows a bounded empty state for zero running containers", async () => {
    apiFakes.listTargets.mockResolvedValue(targetsResponse([]));
    const { wrapper } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    expect(wrapper.find(".app-term-empty").exists()).toBe(true);
    expect(wrapper.text()).toContain("No running containers");

    // Nothing re-discovers on its own.
    await new Promise((resolve) => setTimeout(resolve, 150));
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);
    await wrapper.find(".app-term-refresh").trigger("click");
    await flushPromises();
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  const discoveryFailures = [
    {
      status: 403,
      message: "application write access is required",
      copy: "You need write access to this application's team",
      code: "applicationForbidden",
    },
    {
      status: 404,
      message: "not found",
      copy: "This application is no longer available.",
      code: "applicationNotFound",
    },
    {
      status: 409,
      message: "the application has no running deployment",
      copy: "This application has no running deployment.",
      code: "noDeployment",
    },
    {
      status: 409,
      message: "a deployment is in progress; try again when it finishes",
      copy: "A deployment is in progress.",
      code: "deployInFlight",
    },
    {
      status: 409,
      message: "the selected container is not running; start the application first",
      copy: "The selected container is not running.",
      code: "containerStopped",
    },
    {
      status: 502,
      message: "the node agent is unavailable",
      copy: "The node agent is unavailable.",
      code: "unavailable",
    },
  ];

  for (const failure of discoveryFailures) {
    it(`shows ${failure.code} only after an explicit retry, with no raw body`, async () => {
      apiFakes.listTargets.mockRejectedValue({
        status: failure.status,
        message: `${failure.message} /srv/gotham/secret`,
      });
      const { wrapper } = await mountDetail();
      expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);
      expect(apiFakes.create).toHaveBeenCalledTimes(0);
      expect(wrapper.find(".app-term-alert").exists()).toBe(true);
      expect(wrapper.text()).toContain(failure.copy);
      expect(wrapper.html()).not.toContain("/srv/gotham/secret");

      await new Promise((resolve) => setTimeout(resolve, 150));
      expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);

      await wrapper.find(".app-term-retry").trigger("click");
      await flushPromises();
      expect(apiFakes.listTargets).toHaveBeenCalledTimes(2);
      expect(apiFakes.create).toHaveBeenCalledTimes(0);
      wrapper.unmount();
    });
  }

  it("classifies a replaced container on create as containerGone and never falls back to a host shell", async () => {
    apiFakes.create.mockRejectedValue({
      status: 404,
      message:
        "the selected container is no longer available; refresh the targets /srv/gotham/secret",
    });
    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(apiFakes.hostCreate).toHaveBeenCalledTimes(0);
    const tab = terminal.pageTabs[0]!;
    expect(tab.errorCode).toBe("containerGone");
    expect(wrapper.text()).toContain(
      "The selected container is no longer available.",
    );
    expect(wrapper.html()).not.toContain("/srv/gotham/secret");
    expect(wrapper.html()).not.toContain("Request failed with status code");
    wrapper.unmount();
  });

  it("retries a replaced container by rediscovering, never re-POSTing the dead id", async () => {
    apiFakes.listTargets.mockReset();
    apiFakes.listTargets
      .mockResolvedValueOnce(
        targetsResponse([target("", "cid-single", "gotham-shop-1")], "image"),
      )
      .mockResolvedValueOnce(
        targetsResponse([target("", "cid-new", "gotham-shop-2")], "image"),
      );
    apiFakes.create.mockRejectedValueOnce({
      status: 404,
      message: "the selected container is no longer available; refresh the targets",
    });

    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.create.mock.calls[0]?.[1]).toMatchObject({
      containerId: "cid-single",
    });
    expect(terminal.pageTabs[0]!.errorCode).toBe("containerGone");

    const retry = wrapper
      .findAll("button")
      .find((node) => node.text().includes("Start new session"));
    await retry!.trigger("click");
    await flushPromises();
    await nextTick();
    await flushPromises();

    // One rediscovery, then exactly one POST with the NEW container id.
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(2);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(apiFakes.create.mock.calls[1]?.[1]).toMatchObject({
      service: "",
      containerId: "cid-new",
    });
    const tab = terminal.pageTabs[0]!;
    expect(tab.containerId).toBe("cid-new");
    expect(tab.targetLabel).toBe("gotham-shop-2");
    expect(apiFakes.hostCreate).toHaveBeenCalledTimes(0);
    wrapper.unmount();
  });

  it("recovers a live shell whose container vanished on redeploy", async () => {
    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const first = terminal.pageTabs[0]!;
    expect(first.sessionId).toBe("session-1");

    apiFakes.listTargets.mockResolvedValueOnce(
      targetsResponse([target("", "cid-new", "gotham-shop-2")], "image"),
    );
    runtimeFakes.instances[0]!.emit({
      status: "error",
      errorCode: "containerGone",
    });
    await flushPromises();
    expect(terminal.pageTabs[0]!.errorCode).toBe("containerGone");

    const retry = wrapper
      .findAll("button")
      .find((node) => node.text().includes("Start new session"));
    await retry!.trigger("click");
    await flushPromises();
    await nextTick();
    await flushPromises();

    expect(apiFakes.close).toHaveBeenCalledTimes(1);
    expect(apiFakes.close.mock.calls[0]).toEqual([APP_ID, "session-1"]);
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(2);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(apiFakes.create.mock.calls[1]?.[1]).toMatchObject({
      containerId: "cid-new",
    });
    expect(terminal.pageTabs).toHaveLength(1);
    expect(terminal.pageTabs[0]!.sessionId).toBe("session-2");
    wrapper.unmount();
  });

  it("close leaves an explicit restart card and never auto-creates on re-entry", async () => {
    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const tabId = terminal.pageTabs[0]!.id;

    terminal.closeTabs([tabId]);
    await flushPromises();
    await nextTick();
    await nextTick();
    expect(wrapper.find(".term-stage-closed").exists()).toBe(true);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    await tabButton(wrapper, "Overview").trigger("click");
    await flushPromises();
    await tabButton(wrapper, "Terminal").trigger("click");
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(wrapper.find(".term-stage-closed").exists()).toBe(true);

    const start = wrapper
      .find(".term-stage-closed")
      .findAll("button")
      .find((node) => node.text().includes("Start new session"));
    await start!.trigger("click");
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(terminal.pageTabs).toHaveLength(1);
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    wrapper.unmount();
  });

  it("keeps the restart card across a route remount after a close", async () => {
    const first = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    first.terminal.closeTabs([first.terminal.pageTabs[0]!.id]);
    await flushPromises();
    await nextTick();
    const { pinia } = first;
    first.wrapper.unmount();

    const second = await mountDetail(undefined, "owner", pinia);
    await flushPromises();
    await nextTick();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(second.wrapper.find(".term-stage-closed").exists()).toBe(true);

    const start = second.wrapper
      .find(".term-stage-closed")
      .findAll("button")
      .find((node) => node.text().includes("Start new session"));
    await start!.trigger("click");
    await flushPromises();
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(second.terminal.pageTabs).toHaveLength(1);
    second.wrapper.unmount();
  });

  it("opens a fresh page session beside a retained window shell", async () => {
    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const first = terminal.pageTabs[0]!;
    await wrapper.find(".term-open-in-window").trigger("click");
    await flushPromises();
    expect(terminal.findTab(first.id)?.mode).toBe("window");
    expect(wrapper.find(".term-detached").exists()).toBe(true);
    // JUS-255: the moved card exposes its actions once the window is minimized.
    useTerminalWindowStore().setMinimized(true);
    await nextTick();

    apiFakes.listTargets.mockResolvedValueOnce(
      targetsResponse([target("", "cid-new", "gotham-shop-2")], "image"),
    );
    const start = wrapper
      .find(".term-detached")
      .findAll("button")
      .find((node) => node.text().includes("Start new session"));
    await start!.trigger("click");
    await flushPromises();
    await nextTick();
    await flushPromises();

    // One rediscovery + one fresh page POST; the detached window tab is kept.
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(2);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(apiFakes.create.mock.calls[1]?.[1]).toMatchObject({
      containerId: "cid-new",
    });
    expect(apiFakes.close).toHaveBeenCalledTimes(0);
    const tabs = terminal.tabs;
    expect(tabs).toHaveLength(2);
    expect(tabs.find((tab) => tab.id === first.id)?.mode).toBe("window");
    expect(tabs.find((tab) => tab.id === first.id)?.sessionId).toBe("session-1");
    const page = tabs.find((tab) => tab.mode === "page")!;
    expect(page.containerId).toBe("cid-new");
    expect(page.sessionId).toBe("session-2");
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    wrapper.unmount();
  });

  it("retains the window shell when the selector changes while detached", async () => {
    apiFakes.listTargets.mockResolvedValue(targetsResponse(COMPOSE));
    const { wrapper, terminal } = await mountDetail();
    await selectValue(wrapper, "web\u0000cid-web-1");
    const first = terminal.pageTabs[0]!;
    await wrapper.find(".term-open-in-window").trigger("click");
    await flushPromises();
    expect(terminal.findTab(first.id)?.mode).toBe("window");

    await selectValue(wrapper, "db\u0000cid-db");
    expect(terminal.closeRequest).toBeNull();
    expect(apiFakes.close).toHaveBeenCalledTimes(0);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(apiFakes.create.mock.calls[1]?.[1]).toMatchObject({
      service: "db",
      containerId: "cid-db",
    });
    const tabs = terminal.tabs;
    expect(tabs).toHaveLength(2);
    expect(tabs.find((tab) => tab.id === first.id)?.mode).toBe("window");
    expect(tabs.find((tab) => tab.mode === "page")?.containerId).toBe("cid-db");
    wrapper.unmount();
  });

  it("adopts a live container tab on remount without a second shell", async () => {
    const first = await mountDetail();
    const tab = first.terminal.pageTabs[0]!;
    const { pinia } = first;
    first.wrapper.unmount();

    const second = await mountDetail(undefined, "owner", pinia);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(second.terminal.pageTabs).toHaveLength(1);
    expect(second.terminal.pageTabs[0]!.id).toBe(tab.id);
    expect(second.wrapper.find(".term-surface").exists()).toBe(true);
    second.wrapper.unmount();
  });

  it("forgets the restart marker on logout so a later sign-in opens cleanly", async () => {
    const first = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    first.terminal.closeTabs([first.terminal.pageTabs[0]!.id]);
    await flushPromises();
    await nextTick();
    expect(first.wrapper.find(".term-stage-closed").exists()).toBe(true);

    // Logout resets the store (and therefore the restart marker).
    first.terminal.reset();
    const { pinia } = first;
    first.wrapper.unmount();

    const second = await mountDetail(undefined, "owner", pinia);
    await flushPromises();
    await nextTick();
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(second.wrapper.find(".term-stage-closed").exists()).toBe(false);
    expect(second.wrapper.find(".term-surface").exists()).toBe(true);
    second.wrapper.unmount();
  });

  it("switching application opens that application's own container session", async () => {
    applicationApiFakes.getApplication.mockImplementation(async (id: string) =>
      id === APP_B_ID
        ? { ...application, id: APP_B_ID, name: "blog" }
        : application,
    );
    apiFakes.listTargets.mockImplementation(async (id: string) =>
      id === APP_B_ID
        ? {
            ...targetsResponse(SINGLE, "image"),
            application_id: APP_B_ID,
            application_name: "blog",
            containers: [target("", "cid-blog", "gotham-blog-1")],
          }
        : targetsResponse(SINGLE, "image"),
    );

    const { wrapper, router, terminal } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    await router.push(
      `/projects/${PROJECT_ID}/environments/${ENV_ID}/applications/${APP_B_ID}?tab=terminal`,
    );
    await flushPromises();
    await nextTick();
    await flushPromises();
    await nextTick();

    expect(apiFakes.listTargets).toHaveBeenCalledTimes(2);
    expect(apiFakes.create).toHaveBeenCalledTimes(2);
    expect(apiFakes.create.mock.calls[1]?.[0]).toBe(APP_B_ID);
    expect(apiFakes.create.mock.calls[1]?.[1]).toMatchObject({
      containerId: "cid-blog",
    });
    const tabs = terminal.pageTabs;
    expect(tabs).toHaveLength(2);
    expect(tabs.map((tab) => tab.applicationId).sort()).toEqual([
      APP_ID,
      APP_B_ID,
    ]);
    expect(tabs.find((tab) => tab.applicationId === APP_B_ID)?.targetLabel).toBe(
      "gotham-blog-1",
    );
    wrapper.unmount();
  });

  it("detaches to the window with the same session and keeps the target label", async () => {
    const { wrapper, terminal } = await mountDetail();
    const tab = terminal.pageTabs[0]!;
    await wrapper.find(".term-open-in-window").trigger("click");
    await flushPromises();

    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(terminal.findTab(tab.id)?.mode).toBe("window");
    expect(terminal.findTab(tab.id)?.sessionId).toBe(tab.sessionId);
    expect(terminal.findTab(tab.id)?.targetLabel).toBe("gotham-shop-1");
    expect(terminal.findTab(tab.id)?.title).toBe("shop · gotham-shop-1");
    expect(wrapper.find(".term-detached").exists()).toBe(true);
    wrapper.unmount();
  });

  it("keeps the target label on the paused dock card", async () => {
    const { wrapper, router, pinia, terminal } = await mountDetail();
    const tab = terminal.pageTabs[0]!;
    wrapper.unmount();
    await router.push("/dashboard");
    const dock = mount(TerminalPageDock, {
      attachTo: document.body,
      global: { plugins: [pinia, router, i18n] },
    });
    await flushPromises();
    expect(dock.find(".term-dock").exists()).toBe(true);
    expect(dock.text()).toContain("shop · gotham-shop-1");
    expect(dock.text()).toContain("gotham-shop-1");
    expect(dock.text()).toContain("Paused");
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    // Resume goes back to the owning tab without a second shell.
    await dock.find(".term-dock-resume").trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query.tab).toBe("terminal");
    expect(terminal.pageTabs[0]!.id).toBe(tab.id);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    dock.unmount();
  });
});

describe("application terminal permissions (JUS-247)", () => {
  it("waits for a known role before any request (read_only cold load)", async () => {
    const { wrapper, teams } = await mountDetail(undefined, null);
    // Role unknown: the tab is present but neutral, and nothing is requested.
    expect(wrapper.text()).toContain("Checking terminal access");
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(0);
    expect(apiFakes.create).toHaveBeenCalledTimes(0);

    teams.teams = [team("read_only")];
    await flushPromises();
    await nextTick();
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(0);
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    expect(apiFakes.hostCreate).toHaveBeenCalledTimes(0);
    expect(wrapper.text()).toContain("Terminal not available");
    wrapper.unmount();
  });

  it("discovers only after a write role resolves on a cold load", async () => {
    const { wrapper, teams } = await mountDetail(undefined, null);
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(0);
    expect(apiFakes.create).toHaveBeenCalledTimes(0);

    teams.teams = [team("owner")];
    await flushPromises();
    await nextTick();
    await flushPromises();
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    wrapper.unmount();
  });

  it("shows a bounded access error with retry when the teams read fails", async () => {
    const { wrapper, teams } = await mountDetail(
      undefined,
      null,
      undefined,
      "error",
    );
    expect(wrapper.text()).toContain("Terminal access could not be checked");
    expect(wrapper.find(".app-term-access-retry").exists()).toBe(true);
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(0);
    expect(apiFakes.create).toHaveBeenCalledTimes(0);

    teamsApiFakes.listTeams.mockResolvedValueOnce([team("owner")]);
    await wrapper.find(".app-term-access-retry").trigger("click");
    await flushPromises();
    await nextTick();
    await flushPromises();
    expect(teams.activeTeam?.role).toBe("owner");
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(1);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });

  it("bounds a hanging teams read and never requests a terminal meanwhile", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const { wrapper } = await mountDetail(
        undefined,
        null,
        undefined,
        "hang",
      );
      expect(wrapper.text()).toContain("Checking terminal access");
      expect(apiFakes.listTargets).toHaveBeenCalledTimes(0);
      expect(apiFakes.create).toHaveBeenCalledTimes(0);

      await vi.advanceTimersByTimeAsync(8_000);
      await flushPromises();
      await nextTick();
      expect(wrapper.text()).toContain("Terminal access could not be checked");
      expect(apiFakes.listTargets).toHaveBeenCalledTimes(0);
      expect(apiFakes.create).toHaveBeenCalledTimes(0);
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });

  it("hides the tab and issues zero requests for a read_only team role", async () => {
    const { wrapper, terminal } = await mountDetail(undefined, "read_only");
    const labels = wrapper.findAll(".n-tabs-tab").map((node) => node.text());
    expect(labels).not.toContain("Terminal");
    expect(wrapper.text()).toContain("Terminal not available");
    expect(wrapper.text()).toContain("write access");
    expect(apiFakes.listTargets).toHaveBeenCalledTimes(0);
    expect(apiFakes.create).toHaveBeenCalledTimes(0);
    expect(apiFakes.hostCreate).toHaveBeenCalledTimes(0);
    expect(terminal.pageTabs).toHaveLength(0);
    wrapper.unmount();
  });

  it("leaves the pane on demotion without closing the shell itself", async () => {
    const { wrapper, terminal, teams } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);

    teams.teams = [team("read_only")];
    await flushPromises();
    await nextTick();

    // The tab host only leaves; the app shell owns disposal (next test), so a
    // transient canWrite gap can never kill a live PTY from here.
    expect(apiFakes.close).toHaveBeenCalledTimes(0);
    expect(terminal.pageTabs).toHaveLength(1);
    expect(wrapper.findComponent(NTabs).props("value")).toBe("overview");
    wrapper.unmount();
  });

  it("the app-shell boundary disposes container tabs once on demotion", async () => {
    const { terminal, teams } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const tab = terminal.pageTabs[0]!;
    const Boundary = defineComponent({
      setup() {
        useTerminalAccessBoundary();
        return () => h("div");
      },
    });
    const boundary = mount(Boundary, {
      global: { plugins: [useAuthStore().$pinia] },
    });
    await flushPromises();

    teams.teams = [team("read_only")];
    await flushPromises();
    await nextTick();

    expect(apiFakes.close).toHaveBeenCalledTimes(1);
    expect(apiFakes.close.mock.calls[0]).toEqual([APP_ID, tab.sessionId]);
    expect(terminal.pageTabs).toHaveLength(0);
    expect(apiFakes.hostClose).toHaveBeenCalledTimes(0);
    boundary.unmount();
  });

  it("never closes a live shell during a transient application refetch gap", async () => {
    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    const tab = terminal.pageTabs[0]!;
    const appsStore = useApplicationsStore();

    // The page's application row briefly disappears (refetch/cache churn).
    appsStore.applicationsById[APP_ID] = null as never;
    await flushPromises();
    await nextTick();
    expect(terminal.findTab(tab.id)).not.toBeNull();
    expect(apiFakes.close).toHaveBeenCalledTimes(0);

    // Restoring the row re-adopts the same session, no second POST.
    appsStore.applicationsById[APP_ID] = application as never;
    await flushPromises();
    await nextTick();
    expect(terminal.findTab(tab.id)?.sessionId).toBe(tab.sessionId);
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    expect(wrapper.find(".term-surface").exists()).toBe(true);
    wrapper.unmount();
  });

  it("a logout clears the store without issuing a DELETE", async () => {
    const { wrapper, terminal } = await mountDetail();
    expect(apiFakes.create).toHaveBeenCalledTimes(1);
    useAuthStore().clearSession();
    await flushPromises();
    expect(terminal.tabs).toHaveLength(0);
    expect(apiFakes.close).toHaveBeenCalledTimes(0);
    wrapper.unmount();
  });
});
