// GitSourcesPage component states (GS-10 fix round 1): loading, empty,
// error/Retry, Connect GitHub submit, reconnect, the 409 dialog (kept open,
// no unhandled rejection), the wizard shortcut and the callback back-links.
// The page composable is mocked; the row mapping it feeds is covered in
// git-sources-page.test.ts.
import { NMessageProvider } from "naive-ui";
import { createPinia, setActivePinia } from "pinia";
import { defineComponent, h, ref } from "vue";
import type { Ref } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { flushPromises, mount } from "@vue/test-utils";
import type { VueWrapper } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/features/applications/composables/useGitSourcesPage", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/features/applications/composables/useGitSourcesPage")>();
  return { ...actual, useGitSourcesPage: vi.fn() };
});

import GitHubAppCallbackPage from "@/features/applications/pages/GitHubAppCallbackPage.vue";
import GitSourcesPage from "@/features/applications/pages/GitSourcesPage.vue";
import ProviderCallbackPage from "@/features/applications/pages/ProviderCallbackPage.vue";
import WizardSourceStep from "@/features/applications/components/WizardSourceStep.vue";
import {
  provideCreateWizard,
  useCreateAppWizard,
} from "@/features/applications/composables/useCreateAppWizard";
import { useGitSourcesPage } from "@/features/applications/composables/useGitSourcesPage";
import type { GitSourceRow } from "@/features/applications/composables/useGitSourcesPage";
import applicationsEn from "@/features/applications/locales/en";
import applicationsVi from "@/features/applications/locales/vi";
import { useTeamsStore } from "@/features/teams";
import { i18n, resetLocaleState, syncComposerLocale } from "@/shared/i18n";

const mockedPage = vi.mocked(useGitSourcesPage);

const gitHubRow: GitSourceRow = {
  kind: "github-app",
  id: "gh-1",
  title: "GitHub App",
  subtitle: "gotham-ci",
  connected: true,
  account: "acme",
  instance: "github.com",
  installations: 1,
  repos: 48,
  apps: ["shop", "blog"],
  createdAt: "2026-10-02T00:00:00Z",
  legacy: false,
};

const gitLabRow: GitSourceRow = {
  kind: "provider",
  id: "prov-1",
  title: "GitLab",
  subtitle: "OAuth · PKCE",
  connected: true,
  account: "git.example.com",
  instance: "read_api",
  installations: null,
  repos: 12,
  apps: ["docs"],
  createdAt: "2026-09-28T00:00:00Z",
  legacy: false,
};

interface PageFixture {
  rows: Ref<GitSourceRow[]>;
  loading: Ref<boolean>;
  error: Ref<string | null>;
  usageKnown: Ref<boolean>;
  usageLoading: Ref<boolean>;
  disconnectError: Ref<string | null>;
  disconnectNames: Ref<string[]>;
  refresh: ReturnType<typeof vi.fn>;
  connectGitHub: ReturnType<typeof vi.fn>;
  reconnect: ReturnType<typeof vi.fn>;
  disconnect: ReturnType<typeof vi.fn>;
}

/** fixture returns the mocked composable shape the page consumes. */
function fixture(over: Partial<PageFixture> = {}): PageFixture {
  return {
    rows: ref<GitSourceRow[]>([gitHubRow, gitLabRow]),
    loading: ref(false),
    error: ref(null),
    usageKnown: ref(true),
    usageLoading: ref(false),
    disconnectError: ref(null),
    disconnectNames: ref([]),
    refresh: vi.fn().mockResolvedValue(undefined),
    connectGitHub: vi.fn().mockResolvedValue(undefined),
    reconnect: vi.fn().mockResolvedValue(undefined),
    disconnect: vi.fn().mockResolvedValue({ applicationsUsing: 0 }),
    ...over,
  };
}

function shell(child: object) {
  return defineComponent({
    render() {
      return h(NMessageProvider, null, { default: () => h(child as never) });
    },
  });
}

function seedTeams(): void {
  const teams = useTeamsStore();
  teams.teams = [{ id: "team-1", name: "Acme", is_personal: false, role: "owner" }] as never;
  teams.activeTeamId = "team-1";
}

function mountPage(fx: PageFixture) {
  mockedPage.mockReturnValue(fx as never);
  return mount(shell(GitSourcesPage), {
    global: { plugins: [i18n], stubs: { teleport: true } },
  });
}

beforeEach(() => {
  setActivePinia(createPinia());
  resetLocaleState();
  i18n.global.mergeLocaleMessage("en", { applications: applicationsEn });
  i18n.global.mergeLocaleMessage("vi", { applications: applicationsVi });
  syncComposerLocale("en");
  vi.clearAllMocks();
  seedTeams();
});

/** dialogConfirm finds the disconnect dialog confirm button (row buttons
 * share its label, so scope to the open dialog). */
function dialogConfirm(wrapper: VueWrapper) {
  const cards = wrapper.findAll(".app-modal");
  for (const card of cards) {
    const found = card.findAll("button").find((b) => b.text() === "Disconnect");
    if (found !== undefined) {
      return found;
    }
  }
  return undefined;
}

describe("GitSourcesPage states", () => {
  it("renders the connections table with the summary line", async () => {
    const wrapper = mountPage(fixture());
    await flushPromises();
    expect(wrapper.find('[data-testid="git-sources-table"]').exists()).toBe(true);
    expect(wrapper.text()).toContain("2 connected");
    expect(wrapper.text()).toContain("gotham-ci");
    expect(wrapper.text()).toContain("git.example.com");
  });

  it("renders loading, empty and error states with a working Retry", async () => {
    const fx = fixture({ rows: ref([]), loading: ref(true) });
    const wrapper = mountPage(fx);
    await flushPromises();
    expect(wrapper.find(".n-spin").exists()).toBe(true);

    fx.loading.value = false;
    await flushPromises();
    expect(wrapper.text()).toContain("No Git sources connected");
    expect(wrapper.text()).toContain("Connect the source first");

    fx.error.value = "Git sources could not be loaded.";
    await flushPromises();
    expect(wrapper.find('[data-testid="git-sources-error"]').exists()).toBe(true);
    await wrapper.find('[data-testid="git-sources-error"] button').trigger("click");
    expect(fx.refresh).toHaveBeenCalled();
  });

  it("shows unknown usage instead of a false none", async () => {
    const wrapper = mountPage(fixture({ usageKnown: ref(false), usageLoading: ref(false) }));
    await flushPromises();
    expect(wrapper.text()).toContain("Unknown");
    expect(wrapper.text()).not.toContain("No application in this team");
  });

  it("submits Connect GitHub with the typed name", async () => {
    const fx = fixture();
    const wrapper = mountPage(fx);
    await flushPromises();
    await wrapper.find(".page-actions button").trigger("click");
    await flushPromises();
    await wrapper.find("input#github-app-name-input").setValue("gotham-ci");
    const card = wrapper.findAll(".dialog-card").find((c) => c.text().includes("Connect GitHub"));
    await card?.findAll("button").find((b) => b.text() === "Connect GitHub")?.trigger("click");
    await flushPromises();
    expect(fx.connectGitHub).toHaveBeenCalledWith("gotham-ci");
  });

  it("reconnects a row and surfaces the server reason on failure", async () => {
    const fx = fixture({
      reconnect: vi.fn().mockRejectedValue({ status: 500, message: "providers: github unavailable" }),
    });
    const wrapper = mountPage(fx);
    await flushPromises();
    const reconnect = wrapper
      .find('[data-testid="git-sources-table"] tbody tr')
      .findAll("button")
      .find((b) => b.text() === "Reconnect");
    await reconnect?.trigger("click");
    await flushPromises();
    expect(fx.reconnect).toHaveBeenCalledWith(expect.objectContaining({ id: "gh-1" }));
    expect(wrapper.text()).toContain("github unavailable");
  });

  it("keeps the 409 dialog open with the names and no unhandled rejection", async () => {
    const rejections: unknown[] = [];
    const onRejection = (reason: unknown): void => {
      rejections.push(reason);
    };
    process.on("unhandledRejection", onRejection);
    try {
      const fx = fixture({
        disconnect: vi.fn().mockImplementation(async () => {
          fx.disconnectNames.value = ["shop", "blog"];
          fx.disconnectError.value = "request failed applications still use this connection: shop, blog";
          throw { status: 409, message: "applications still use this connection: shop, blog" };
        }),
      });
      const wrapper = mountPage(fx);
      await flushPromises();
      const rowButtons = wrapper
        .find('[data-testid="git-sources-table"] tbody tr')
        .findAll("button");
      await rowButtons.find((b) => b.text() === "Disconnect")?.trigger("click");
      await flushPromises();
      expect(wrapper.text()).toContain("shop");
      await dialogConfirm(wrapper)?.trigger("click");
      await flushPromises();
      await new Promise((resolve) => setTimeout(resolve, 50));
      // The dialog stays open with the server-named applications…
      expect(wrapper.text()).toContain("Applications still use this connection: shop, blog");
      // …and the rejection is handled, never unhandled.
      expect(rejections).toEqual([]);
      expect(fx.disconnect).toHaveBeenCalled();
    } finally {
      process.off("unhandledRejection", onRejection);
    }
  });

  it("requires acknowledgement when usage is unknown", async () => {
    const fx = fixture({
      usageKnown: ref(false),
      disconnect: vi.fn().mockResolvedValue({ applicationsUsing: 0 }),
    });
    const wrapper = mountPage(fx);
    await flushPromises();
    const rowButtons = wrapper
      .find('[data-testid="git-sources-table"] tbody tr')
      .findAll("button");
    await rowButtons.find((b) => b.text() === "Disconnect")?.trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("Could not check which applications use");
    expect(dialogConfirm(wrapper)?.attributes("disabled")).not.toBeUndefined();
    await wrapper.find(".n-checkbox").trigger("click");
    await flushPromises();
    expect(dialogConfirm(wrapper)?.attributes("disabled")).toBeUndefined();
    await dialogConfirm(wrapper)?.trigger("click");
    await flushPromises();
    expect(fx.disconnect).toHaveBeenCalled();
  });
});

describe("wizard shortcut and callback back-links", () => {
  function mountWithRouter(component: object, path: string) {
    const stub = { template: "<div />" };
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: "/git-sources", name: "git-sources", component: stub },
        { path: "/wizard", name: "wizard", component: component as never },
        { path: "/providers/callback", name: "provider-callback", component: component as never },
        {
          path: "/applications/github-app/callback",
          name: "github-app-callback",
          component: component as never,
        },
      ],
    });
    return { router, path };
  }

  it("links to Git sources when the provider flow has no connections", async () => {
    const { router } = mountWithRouter(WizardSourceStep, "/wizard");
    await router.push("/wizard");
    await router.isReady();
    const Harness = defineComponent({
      setup() {
        const wiz = useCreateAppWizard(
          ref(false),
          (() => undefined) as never,
          { projectId: "", environmentId: "" },
        );
        provideCreateWizard(wiz);
        wiz.form.sourceType = "github_app";
        return () => h(WizardSourceStep);
      },
    });
    const wrapper = mount(shell(Harness), {
      global: { plugins: [router, i18n], stubs: { teleport: true } },
    });
    await flushPromises();
    const link = wrapper.find('a[href="/git-sources"]');
    expect(link.exists()).toBe(true);
    expect(link.text()).toContain("Connect GitHub / GitLab");
  });

  it("sends both callbacks back to Git sources", async () => {
    for (const component of [ProviderCallbackPage, GitHubAppCallbackPage]) {
      const { router } = mountWithRouter(component, "/x");
      await router.push(
        component === ProviderCallbackPage
          ? "/providers/callback?status=error&reason=failed"
          : "/applications/github-app/callback",
      );
      await router.isReady();
      const wrapper = mount(shell(component), {
        global: { plugins: [router, i18n], stubs: { teleport: true } },
      });
      await flushPromises();
      await wrapper.find("button").trigger("click");
      await flushPromises();
      expect(router.currentRoute.value.name).toBe("git-sources");
    }
  });
});
