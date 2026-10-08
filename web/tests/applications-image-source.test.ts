// GS-9 image source: reference validation mirrors internal/deploy/image.go,
// the wizard gates and payload carry the image shape, and the detail page
// shows the reference without branch/build-pack rows.
import { mount } from "@vue/test-utils";
import { NInput, NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, nextTick, ref } from "vue";

import type { Application } from "@/features/applications/api/applications";
import ApplicationImagePanel from "@/features/applications/components/ApplicationImagePanel.vue";
import ApplicationOverviewTab from "@/features/applications/components/ApplicationOverviewTab.vue";
import { useCreateAppWizard } from "@/features/applications/composables/useCreateAppWizard";
import ApplicationDetailPage from "@/features/applications/pages/ApplicationDetailPage.vue";
import {
  imageRefSchema,
  isImageRef,
  isLatestImageTag,
  sourceTypeImplemented,
} from "@/features/applications/schemas/applications";
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

describe("image reference validation", () => {
  const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef";
  const cases: Array<{ name: string; ref: string; valid: boolean }> = [
    { name: "library", ref: "nginx:1.25.3", valid: true },
    { name: "namespaced", ref: "library/nginx", valid: true },
    { name: "registry with port", ref: "registry.example.com:5000/team/app:1.2", valid: true },
    { name: "digest pinned", ref: `nginx@${digest}`, valid: true },
    { name: "tag and digest", ref: `registry.example.com/team/app:1.2@${digest}`, valid: true },
    { name: "latest stays valid (warning only)", ref: "nginx:latest", valid: true },
    { name: "implicit latest", ref: "nginx", valid: true },
    { name: "empty", ref: "", valid: false },
    { name: "whitespace", ref: "ng inx:1", valid: false },
    { name: "empty tag", ref: "nginx:", valid: false },
    { name: "uppercase path", ref: "Team/App:1", valid: false },
    { name: "bad digest", ref: "nginx@sha256:zzz", valid: false },
    { name: "localhost refused", ref: "localhost:5000/app:1", valid: false },
    { name: "localhost bare refused", ref: "localhost/app:1", valid: false },
    { name: "localhost trailing dot refused", ref: "localhost./x:1", valid: false },
    { name: "localhost subdomain refused", ref: "evil.localhost/x:1", valid: false },
    { name: "loopback refused", ref: "127.0.0.1:5000/app:1", valid: false },
    { name: "loopback shorthand refused", ref: "127.1/x:1", valid: false },
    { name: "loopback shorthand 3-part refused", ref: "127.0.1/x:1", valid: false },
    { name: "unspecified refused", ref: "0.0.0.0:5000/x:1", valid: false },
    { name: "unspecified v6 refused", ref: "[::]:5000/x:1", valid: false },
    { name: "loopback v6 refused", ref: "[::1]:5000/x:1", valid: false },
    { name: "out-of-range quad refused", ref: "999.1.1.1/x:1", valid: false },
    { name: "out-of-range quads refused", ref: "300.300.300.300/x:1", valid: false },
    { name: "loopback v6 expanded refused", ref: "[0:0:0:0:0:0:0:1]/x:1", valid: false },
    { name: "loopback v4-mapped refused", ref: "[::ffff:7f00:1]/x:1", valid: false },
    { name: "loopback v4-mapped dotted refused", ref: "[::ffff:127.0.0.1]/x:1", valid: false },
    { name: "unbracketed v6 refused", ref: "::1/x:1", valid: false },
    { name: "dash-leading host refused", ref: "-v.evil/x:1", valid: false },
    { name: "double-dash host refused", ref: "--privileged.x/y:1", valid: false },
    { name: "empty label refused", ref: "a..b/x:1", valid: false },
    { name: "underscore host refused", ref: "reg_x.example.com/x:1", valid: false },
    { name: "port zero refused", ref: "reg.example.com:0/x:1", valid: false },
    { name: "port too big refused", ref: "reg.example.com:99999/x:1", valid: false },
    { name: "port non-numeric refused", ref: "reg.example.com:http/x:1", valid: false },
    { name: "empty port refused", ref: "reg.example.com:/x:1", valid: false },
    { name: "multi-colon host refused", ref: "registry:abc:123/foo:1.0", valid: false },
    { name: "private ipv4 allowed", ref: "192.168.1.10:5000/app:1", valid: true },
    { name: "single-label host with port allowed", ref: "reg:5000/app:1", valid: true },
  ];
  for (const c of cases) {
    it(`${c.name}: ${c.ref || "(empty)"}`, () => {
      expect(isImageRef(c.ref)).toBe(c.valid);
      expect(imageRefSchema.safeParse(c.ref).success).toBe(c.valid);
    });
  }

  it("flags moving tags for the warning", () => {
    expect(isLatestImageTag("nginx:latest")).toBe(true);
    expect(isLatestImageTag("nginx")).toBe(true);
    expect(isLatestImageTag("nginx:1.25")).toBe(false);
    expect(isLatestImageTag("nginx@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")).toBe(false);
    // Pinned even with a latest tag: the digest freezes the release.
    expect(isLatestImageTag("nginx:latest@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")).toBe(false);
  });

  it("implements the image source type", () => {
    expect(sourceTypeImplemented("image")).toBe(true);
    expect(sourceTypeImplemented("dockerfile")).toBe(true);
    expect(sourceTypeImplemented("git_private")).toBe(false);
    expect(sourceTypeImplemented("compose")).toBe(false);
  });
});

describe("image wizard flow", () => {
  function harness() {
    setActivePinia(createPinia());
    let wiz: ReturnType<typeof useCreateAppWizard> | null = null;
    const Harness = defineComponent({
      setup() {
        wiz = useCreateAppWizard(ref(false), (() => undefined) as never);
        return () => h("div");
      },
    });
    const wrapper = mount({ render: () => h(NMessageProvider, null, { default: () => h(Harness) }) });
    return { wrapper, wiz: () => wiz! };
  }

  const reset = {
    sourceType: "git_public",
    providerId: "",
    publicCloneUrl: "",
    repoFullName: "",
    cloneUrl: "",
    branch: "main",
    imageRef: "",
    registryUsername: "",
    registryPassword: "",
    name: "",
    buildPack: "",
    serverId: "",
    port: 3000,
    hostPort: null,
    baseDomain: "",
    env: [],
    storage: [],
  };

  it("gates the source step on the reference, ignoring branch", () => {
    const { wrapper, wiz } = harness();
    const w = wiz();
    const cases: Array<{ name: string; patch: Record<string, unknown>; valid: boolean }> = [
      { name: "valid", patch: { sourceType: "image", imageRef: "registry.example.com/team/app:1.2", name: "storefront" }, valid: true },
      { name: "digest pinned", patch: { sourceType: "image", imageRef: "nginx:1.25@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", name: "storefront" }, valid: true },
      { name: "bad ref", patch: { sourceType: "image", imageRef: "not a ref", name: "storefront" }, valid: false },
      { name: "loopback", patch: { sourceType: "image", imageRef: "127.0.0.1:5000/app:1", name: "storefront" }, valid: false },
      { name: "missing ref", patch: { sourceType: "image", name: "storefront" }, valid: false },
      { name: "missing name", patch: { sourceType: "image", imageRef: "nginx:1.25" }, valid: false },
      { name: "half credential user only", patch: { sourceType: "image", imageRef: "nginx:1.25", name: "storefront", registryUsername: "robot" }, valid: false },
      { name: "half credential password only", patch: { sourceType: "image", imageRef: "nginx:1.25", name: "storefront", registryPassword: "s3cret" }, valid: false },
      { name: "full credential", patch: { sourceType: "image", imageRef: "nginx:1.25", name: "storefront", registryUsername: "robot", registryPassword: "s3cret" }, valid: true },
    ];
    for (const c of cases) {
      Object.assign(w.form, { ...reset }, c.patch);
      expect({ name: c.name, actual: w.sourceValid.value }).toEqual({ name: c.name, actual: c.valid });
    }
    wrapper.unmount();
  });

  it("builds the image payload without git fields", () => {
    const { wrapper, wiz } = harness();
    const w = wiz();
    Object.assign(w.form, {
      ...reset,
      sourceType: "image",
      imageRef: "registry.example.com/team/app:1.2",
      registryUsername: "robot",
      registryPassword: "s3cret",
      name: "storefront",
      port: 8080,
    });
    expect(w.buildPayload()).toMatchObject({
      provider: "",
      repo: "",
      clone_url: "",
      source_type: "image",
      branch: "",
      build_pack: "",
      image_ref: "registry.example.com/team/app:1.2",
      registry_username: "robot",
      registry_password: "s3cret",
      port: 8080,
    });
    expect(w.reviewSource.value).toBe("registry.example.com/team/app:1.2");
    wrapper.unmount();
  });

  it("clears image fields on source type switch", async () => {
    const { wrapper, wiz } = harness();
    const w = wiz();
    Object.assign(w.form, { sourceType: "image" });
    await nextTick();
    Object.assign(w.form, {
      imageRef: "nginx:1.25",
      registryUsername: "robot",
      registryPassword: "s3cret",
    });
    w.form.sourceType = "git_public";
    await nextTick();
    expect({ imageRef: w.form.imageRef, user: w.form.registryUsername, pass: w.form.registryPassword }).toEqual({
      imageRef: "",
      user: "",
      pass: "",
    });
    wrapper.unmount();
  });
});

function imageApp(): Application {
  return {
    id: "app-1",
    name: "storefront",
    environment_id: "env-1",
    environment_name: "production",
    project_id: "proj-1",
    project_name: "shop",
    provider: "",
    repo: "",
    clone_url: "",
    source_type: "image",
    branch: "",
    build_pack: "",
    image_ref: "registry.example.com/team/app:1.2",
    has_registry_credential: true,
    base_domain: "",
    base_domain_disabled: false,
    port: 3000,
    host_port: 0,
    server_id: "server-1",
    server_name: "node-1",
    created_at: "2026-09-01T10:00:00Z",
    updated_at: "2026-09-01T10:00:00Z",
  } as Application;
}

describe("image detail page", () => {

  it("shows the reference instead of branch/build-pack rows", () => {
    const wrapper = mount(ApplicationOverviewTab, {
      props: {
        application: imageApp(),
        latest: null,
        deployments: [],
        pipelineSteps: [],
        descColumns: 2,
        acting: false,
      },
      global: { plugins: [i18n] },
    });
    const text = wrapper.text();
    expect(text).toContain("registry.example.com/team/app:1.2");
    expect(text).not.toContain("Branch");
    expect(text).not.toContain("Build pack");
    wrapper.unmount();
  });

  it("rejects a bad reference without calling the API", async () => {
    setActivePinia(createPinia());
    const wrapper = mount(NMessageProvider, {
      slots: { default: () => h(ApplicationImagePanel, { application: imageApp() }) },
      global: { plugins: [i18n] },
    });
    const input = wrapper.findComponent(NInput).find("input");
    await input.setValue("not a ref");
    const buttons = wrapper.findAll("button");
    await buttons[0]!.trigger("click");
    await nextTick();
    expect(wrapper.text()).toContain("valid image reference");
    wrapper.unmount();
  });
});

/** Controllable canWrite for the DetailPage gate test below. */
let detailCanWrite = true;

vi.mock("@/features/applications/composables/useApplicationDetail", () => ({
  useApplicationDetail: () => mockImageDetail(),
}));

/** mockImageDetail stands in for the detail composable: real refs for every
 * value the template reads, no-ops for handlers. */
function mockImageDetail() {
  const appsStore = {
    loading: false,
    acting: false,
    error: null,
    savingEnv: false,
    savingStorages: false,
  };
  const values: Record<string, unknown> = {
    application: ref(imageApp()),
    canWrite: ref(detailCanWrite),
    activeTab: ref("overview"),
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
    appId: ref("app-1"),
    shortId: ref("app-1"),
    displayName: ref("storefront"),
    initials: ref("ST"),
    descColumns: ref(2),
    controlHint: ref(""),
    containerStopped: ref(false),
    containerIsRunning: ref(false),
    logTarget: ref(""),
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

describe("image panel viewer gate", () => {
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

  it("shows the image panel to writers", () => {
    detailCanWrite = true;
    const wrapper = mountDetailPage();
    expect(wrapper.findComponent(ApplicationImagePanel).exists()).toBe(true);
    wrapper.unmount();
  });

  it("hides the image panel from viewers", () => {
    detailCanWrite = false;
    const wrapper = mountDetailPage();
    expect(wrapper.findComponent(ApplicationImagePanel).exists()).toBe(false);
    wrapper.unmount();
  });
});
